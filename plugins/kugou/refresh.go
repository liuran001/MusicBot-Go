package kugou

import (
	"context"
	"sync"
	"time"
)

var kugouAutoRefreshStart sync.Once

func (m *ConceptSessionManager) StartAutoRefreshDaemon(ctx context.Context) {
	if m == nil || !m.Enabled() {
		return
	}
	kugouAutoRefreshStart.Do(func() {
		go func() {
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
				state := m.Snapshot()
				interval := state.AutoRefreshPeriod
				if interval <= 0 {
					interval = 6 * time.Hour
				}
				if state.AutoRefresh {
					_, _ = m.ManualRenew(ctx)
				}
				timer.Reset(interval)
			}
		}()
	})
}
