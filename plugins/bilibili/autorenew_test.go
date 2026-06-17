package bilibili

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestSetAutoRenewConcurrentWithStatus 在 -race 下并发调用 SetAutoRenew 与
// AutoRenewStatus，复现并验证修复前 autoRenew 字段无锁读写导致的 data race。
// 修复前：SetAutoRenew 无锁写 enabled/interval，AutoRenewStatus 无锁读 → race。
func TestSetAutoRenewConcurrentWithStatus(t *testing.T) {
	client := getTestClient()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 多个 reader 持续读状态。
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = client.AutoRenewStatus()
				}
			}
		}()
	}

	// writer 反复开关自动续期。cookie 为空，daemon 不会真正发网络请求。
	for i := 0; i < 500; i++ {
		enabled := i%2 == 0
		if _, err := client.SetAutoRenew(enabled, time.Hour); err != nil {
			t.Fatalf("SetAutoRenew failed: %v", err)
		}
	}

	close(stop)
	wg.Wait()

	// 确保收尾时停止任何潜在守护协程。
	client.StopAutoRefreshDaemon()
}

// TestStartStopAutoRefreshDaemon 验证守护协程可启动后被取消停止，且可重启。
// 使用非空 cookie 让 daemon 真正启动；CheckAndRefreshCookie 会因网络/cookie 失败，
// 但本测试只关心 goroutine 生命周期（启动→停止），不依赖网络结果。
func TestStartStopAutoRefreshDaemon(t *testing.T) {
	client := getTestClient()
	client.cookie = "SESSDATA=dummy"
	client.autoRenew.enabled = true
	client.autoRenew.interval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client.StartAutoRefreshDaemon(ctx)

	client.cookieMutex.RLock()
	started := client.autoRenew.started
	hasCancel := client.autoRenew.cancel != nil
	client.cookieMutex.RUnlock()
	if !started || !hasCancel {
		t.Fatalf("daemon should be started with cancel set, got started=%v hasCancel=%v", started, hasCancel)
	}

	client.StopAutoRefreshDaemon()

	client.cookieMutex.RLock()
	startedAfter := client.autoRenew.started
	cancelAfter := client.autoRenew.cancel
	client.cookieMutex.RUnlock()
	if startedAfter || cancelAfter != nil {
		t.Fatalf("daemon should be stopped, got started=%v cancel=%v", startedAfter, cancelAfter)
	}

	// 可重启。
	client.StartAutoRefreshDaemon(ctx)
	client.cookieMutex.RLock()
	restarted := client.autoRenew.started
	client.cookieMutex.RUnlock()
	if !restarted {
		t.Fatal("daemon should be restartable after stop")
	}
	client.StopAutoRefreshDaemon()
}

// TestCloseStopsDaemon 验证 Close 能停止守护协程（用于应用关闭/reload 回收）。
func TestCloseStopsDaemon(t *testing.T) {
	client := getTestClient()
	client.cookie = "SESSDATA=dummy"
	client.autoRenew.enabled = true
	client.autoRenew.interval = time.Hour

	client.StartAutoRefreshDaemon(context.Background())
	if err := client.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	client.cookieMutex.RLock()
	started := client.autoRenew.started
	client.cookieMutex.RUnlock()
	if started {
		t.Fatal("Close should stop the daemon")
	}
}
