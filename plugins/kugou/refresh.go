package kugou

import (
	"context"
	"time"
)

func (m *ConceptSessionManager) StartAutoRefreshDaemon(ctx context.Context) {
	if m == nil || !m.Enabled() {
		return
	}
	state := m.Snapshot()
	if !state.AutoRefresh {
		return
	}
	interval := state.AutoRefreshPeriod
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = m.ManualRenew(ctx)
			}
		}
	}()
}
