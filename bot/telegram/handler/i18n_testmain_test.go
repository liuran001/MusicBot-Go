package handler

import (
	"context"
	"os"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/i18n"
)

// TestMain loads the embedded i18n catalogs once for the whole handler test
// binary. Many handler helpers now resolve user-facing text via the localizer
// in ctx; without an initialized bundle they would echo message IDs and the
// Chinese/English assertions would fail.
func TestMain(m *testing.M) {
	_, _ = i18n.Init()
	os.Exit(m.Run())
}

// zhCtx returns a context carrying the Simplified Chinese localizer, used by
// tests that assert the original (pre-i18n) Chinese strings.
func zhCtx() context.Context {
	return i18n.WithLocalizer(context.Background(), i18n.For("zh"))
}
