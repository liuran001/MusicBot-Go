package kugou

import (
	"context"
	"testing"
	"time"
)

// TestConceptDaemonLifecycle 验证概念版自动续期守护协程可启动、停止、重启，
// 修复了旧实现包级 sync.Once 导致 reload 后旧协程泄漏、新 manager 永不启动的问题。
// 使用 AutoRefresh=false 让 daemon 启动但每次 tick 跳过实际续期，避免触及网络。
func TestConceptDaemonLifecycle(t *testing.T) {
	mgr := NewConceptSessionManager(nil, nil, conceptSession{
		Enabled:           true,
		AutoRefresh:       false,
		AutoRefreshPeriod: time.Hour,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr.StartAutoRefreshDaemon(ctx)

	mgr.mu.RLock()
	started := mgr.daemonStarted
	hasCancel := mgr.daemonCancel != nil
	mgr.mu.RUnlock()
	if !started || !hasCancel {
		t.Fatalf("daemon should be started with cancel set, got started=%v hasCancel=%v", started, hasCancel)
	}

	// 重复 Start 应幂等，不再启动第二个协程。
	mgr.StartAutoRefreshDaemon(ctx)

	mgr.StopAutoRefreshDaemon()
	mgr.mu.RLock()
	startedAfter := mgr.daemonStarted
	cancelAfter := mgr.daemonCancel
	mgr.mu.RUnlock()
	if startedAfter || cancelAfter != nil {
		t.Fatalf("daemon should be stopped, got started=%v cancel=%v", startedAfter, cancelAfter)
	}

	// 模拟 reload：可在同一/新 manager 上重启。
	mgr.StartAutoRefreshDaemon(ctx)
	mgr.mu.RLock()
	restarted := mgr.daemonStarted
	mgr.mu.RUnlock()
	if !restarted {
		t.Fatal("daemon should be restartable after stop")
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	mgr.mu.RLock()
	closedStarted := mgr.daemonStarted
	mgr.mu.RUnlock()
	if closedStarted {
		t.Fatal("Close should stop the daemon")
	}
}

// TestConceptDaemonDisabledNoStart 验证 Enabled=false 时守护协程不启动。
func TestConceptDaemonDisabledNoStart(t *testing.T) {
	mgr := NewConceptSessionManager(nil, nil, conceptSession{Enabled: false})
	mgr.StartAutoRefreshDaemon(context.Background())
	mgr.mu.RLock()
	started := mgr.daemonStarted
	mgr.mu.RUnlock()
	if started {
		t.Fatal("daemon should not start when concept session is disabled")
	}
}
