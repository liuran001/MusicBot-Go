package handler

import (
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// stubManagerWithPlatforms builds a stub manager populated with a handful of
// platforms (with aliases) for help-text rendering tests.
func stubManagerWithPlatforms() *stubPlatformManager {
	m := newStubManager()
	metas := []platform.Meta{
		{Name: "netease", DisplayName: "NetEase Cloud Music", Aliases: []string{"163", "netease", "wyy", "ncm"}},
		{Name: "qqmusic", DisplayName: "QQ Music", Aliases: []string{"qq", "qqmusic", "qqyy"}},
		{Name: "applemusic", DisplayName: "Apple Music", Aliases: []string{"am", "apple", "applemusic"}},
		{Name: "spotify", DisplayName: "Spotify", Aliases: []string{"spotify", "spot"}},
		{Name: "youtubemusic", DisplayName: "YouTube Music", Aliases: []string{"ytm", "youtube", "yt"}},
	}
	for _, meta := range metas {
		m.metas[meta.Name] = meta
		m.platforms[meta.Name] = nil
	}
	return m
}

// TestBuildHelpTextFoldsPlatforms verifies the platform/alias list is rendered
// as a single expandable MarkdownV2 blockquote (collapsed by default) rather
// than a long flat bullet list, and that each platform's aliases live behind
// the fold.
func TestBuildHelpTextFoldsPlatforms(t *testing.T) {
	text := buildHelpText(zhCtx(), stubManagerWithPlatforms(), false, nil, true, true)

	// The expandable blockquote opens with `**>` and closes with `||`.
	if !strings.Contains(text, "**>") {
		t.Fatalf("expected an expandable blockquote (**>), got:\n%s", text)
	}
	if !strings.Contains(text, "||") {
		t.Fatalf("expected the blockquote to be closed with ||, got:\n%s", text)
	}

	// Every platform's aliases must appear behind the fold as quoted code.
	for _, alias := range []string{"`163`", "`qq`", "`am`", "`spotify`", "`ytm`"} {
		if !strings.Contains(text, alias) {
			t.Fatalf("expected alias %s in folded block, got:\n%s", alias, text)
		}
	}

	// The collapsed summary line shows the Platform label followed by a preview
	// of display names with an ellipsis (more behind the fold).
	if !strings.Contains(text, "平台：") {
		t.Fatalf("expected platform summary label, got:\n%s", text)
	}
	if !strings.Contains(text, "…") {
		t.Fatalf("expected an ellipsis in the truncated summary, got:\n%s", text)
	}

	// The old layout had a separate "支持平台" (supported platforms) line; it must
	// be gone now that everything is folded into one block.
	if strings.Contains(text, "支持平台") {
		t.Fatalf("expected the redundant supported-platforms line to be removed, got:\n%s", text)
	}
}

// TestBuildHelpTextNoDoubledEmoji guards against the section headers carrying a
// duplicated emoji (the localized strings already include one, so the code must
// not prepend another).
func TestBuildHelpTextNoDoubledEmoji(t *testing.T) {
	text := buildHelpText(zhCtx(), stubManagerWithPlatforms(), true,
		nil, true, true)
	for _, doubled := range []string{"🚀 🚀", "🎚 🎚", "💡 💡", "🛠 🛠"} {
		if strings.Contains(text, doubled) {
			t.Fatalf("found doubled emoji %q in help text:\n%s", doubled, text)
		}
	}
}

// TestBuildPlatformBlockFallback verifies that with no registered platforms the
// section degrades to a static inline hint instead of an empty fold.
func TestBuildPlatformBlockFallback(t *testing.T) {
	block := buildPlatformBlock(zhCtx(), newStubManager())
	if strings.Contains(block, "**>") {
		t.Fatalf("expected no blockquote when no platforms registered, got: %q", block)
	}
	if !strings.Contains(block, "`163`") || !strings.Contains(block, "`qq`") {
		t.Fatalf("expected static 163/qq hint fallback, got: %q", block)
	}
}
