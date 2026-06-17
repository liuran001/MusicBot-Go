package kugou

import (
	"context"
	"time"
)

// StartAutoRefreshDaemon 启动 per-manager 的概念版会话自动续期守护协程。
//
// 旧实现用包级 sync.Once，导致两个问题：(1) /reload 重建 manager 后旧守护协程
// 因 ctx=context.Background() 永不退出而泄漏，新 manager 又因 Once 已触发而永不启动；
// (2) 守护协程与续期请求都用 background ctx，进程关闭时无法取消。
//
// 现改为：守护 ctx 从传入 ctx 派生并可取消（cancel 存入 m.daemonCancel），每次续期
// 用带超时的子 ctx，关闭/重载时通过 Close 回收。
func (m *ConceptSessionManager) StartAutoRefreshDaemon(ctx context.Context) {
	if m == nil || !m.Enabled() {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m.mu.Lock()
	if m.daemonStarted {
		m.mu.Unlock()
		return
	}
	m.daemonStarted = true
	daemonCtx, cancel := context.WithCancel(ctx)
	m.daemonCancel = cancel
	m.mu.Unlock()

	go func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-daemonCtx.Done():
				return
			case <-timer.C:
			}
			state := m.Snapshot()
			interval := state.AutoRefreshPeriod
			if interval <= 0 {
				interval = 6 * time.Hour
			}
			if state.AutoRefresh {
				m.runAutoRefreshOnce(daemonCtx)
			}
			timer.Reset(interval)
		}
	}()
}

// runAutoRefreshOnce 以带超时的子 ctx 执行一次续期，避免底层 HTTP 卡死导致守护协程长期挂起。
func (m *ConceptSessionManager) runAutoRefreshOnce(ctx context.Context) {
	renewCtx, cancel := context.WithTimeout(ctx, conceptAutoRefreshTimeout)
	defer cancel()
	_, _ = m.ManualRenew(renewCtx)
}

// StopAutoRefreshDaemon 取消守护协程并重置启动标志，可重复调用，幂等。
func (m *ConceptSessionManager) StopAutoRefreshDaemon() {
	if m == nil {
		return
	}
	m.mu.Lock()
	cancel := m.daemonCancel
	m.daemonCancel = nil
	m.daemonStarted = false
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Close 实现 io.Closer，供应用关闭或 /reload 丢弃旧实例时停止后台续期协程。
func (m *ConceptSessionManager) Close() error {
	m.StopAutoRefreshDaemon()
	return nil
}
