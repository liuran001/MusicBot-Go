package platform

import (
	"sync/atomic"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform/registry"
)

// closablePlatform 嵌入 mockPlatform 并实现 io.Closer，记录 Close 调用次数，
// 用于验证 Manager 在 Reset/Close 时回收实现了 io.Closer 的平台（如持有后台
// Cookie 续期守护协程的 bilibili/kugou），防止 daemon 泄漏。
type closablePlatform struct {
	*mockPlatform
	closed int32
}

func (c *closablePlatform) Close() error {
	atomic.AddInt32(&c.closed, 1)
	return nil
}

func TestManagerResetClosesPlatforms(t *testing.T) {
	mgr := NewManagerWithRegistry(registry.New())
	closable := &closablePlatform{mockPlatform: &mockPlatform{name: "closable"}}
	plain := &mockPlatform{name: "plain"} // 不实现 io.Closer，应被安全忽略

	mgr.Register(closable)
	mgr.Register(plain)

	mgr.Reset()

	if got := atomic.LoadInt32(&closable.closed); got != 1 {
		t.Fatalf("Reset should close io.Closer platforms exactly once, got %d", got)
	}
	if len(mgr.List()) != 0 {
		t.Fatalf("Reset should clear providers, got %d", len(mgr.List()))
	}
}

func TestManagerCloseClosesPlatforms(t *testing.T) {
	mgr := NewManagerWithRegistry(registry.New())
	closable := &closablePlatform{mockPlatform: &mockPlatform{name: "closable"}}
	mgr.Register(closable)

	if err := mgr.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if got := atomic.LoadInt32(&closable.closed); got != 1 {
		t.Fatalf("Close should close io.Closer platforms exactly once, got %d", got)
	}
}
