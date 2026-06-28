package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempConfig writes content to a temp .ini file and returns its path.
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.ini")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// TestBotProfileLoad verifies [bot_profile.<lang>] sections load into the
// typed accessors and that unknown keys are ignored.
func TestBotProfileLoad(t *testing.T) {
	path := writeTempConfig(t, `BOT_TOKEN = t

[bot_profile.zh]
name = 我的机器人
description = 中文简介
short_description = 短简介
bogus = ignored

[bot_profile.en]
name = My Bot
`)
	conf, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got := conf.GetBotProfileField("zh", "name"); got != "我的机器人" {
		t.Errorf("zh name = %q, want 我的机器人", got)
	}
	if got := conf.GetBotProfileField("zh", "description"); got != "中文简介" {
		t.Errorf("zh description = %q", got)
	}
	// Unknown key must not load.
	if got := conf.GetBotProfileField("zh", "bogus"); got != "" {
		t.Errorf("bogus key should be dropped, got %q", got)
	}
	if got := conf.GetBotProfileField("en", "name"); got != "My Bot" {
		t.Errorf("en name = %q, want My Bot", got)
	}
	// A language with no section returns empty, not a panic.
	if got := conf.GetBotProfileField("ja", "name"); got != "" {
		t.Errorf("ja name = %q, want empty", got)
	}
}

// TestBotProfileSetPersists verifies SetBotProfileField writes through to the
// file and survives a reload.
func TestBotProfileSetPersists(t *testing.T) {
	path := writeTempConfig(t, "BOT_TOKEN = t\n")
	conf, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := conf.SetBotProfileField("zh", "name", "新名字"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := conf.SetBotProfileField("zh", "description", "新简介"); err != nil {
		t.Fatalf("set desc: %v", err)
	}

	// In-memory copy reflects the change immediately.
	if got := conf.GetBotProfileField("zh", "name"); got != "新名字" {
		t.Errorf("in-memory name = %q", got)
	}

	// Reload from disk: the section was actually persisted.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.GetBotProfileField("zh", "name"); got != "新名字" {
		t.Errorf("persisted name = %q, want 新名字", got)
	}
	if got := reloaded.GetBotProfileField("zh", "description"); got != "新简介" {
		t.Errorf("persisted description = %q", got)
	}

	// The original BOT_TOKEN key must be untouched.
	if reloaded.GetString("BOT_TOKEN") != "t" {
		t.Errorf("BOT_TOKEN clobbered: %q", reloaded.GetString("BOT_TOKEN"))
	}
}

// TestBotProfileSetRejectsUnknownKey ensures only known fields are accepted.
func TestBotProfileSetRejectsUnknownKey(t *testing.T) {
	path := writeTempConfig(t, "BOT_TOKEN = t\n")
	conf, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := conf.SetBotProfileField("zh", "nickname", "x"); err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

// TestBotProfileSetEmptyRemovesKey verifies that setting an empty value clears
// the override, falling back to the embedded default.
func TestBotProfileSetEmptyRemovesKey(t *testing.T) {
	path := writeTempConfig(t, `BOT_TOKEN = t

[bot_profile.zh]
name = 旧名字
description = 保留
`)
	conf, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := conf.SetBotProfileField("zh", "name", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := conf.GetBotProfileField("zh", "name"); got != "" {
		t.Errorf("name not cleared: %q", got)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.GetBotProfileField("zh", "name"); got != "" {
		t.Errorf("name should be gone after reload, got %q", got)
	}
	// The sibling key must survive.
	if got := reloaded.GetBotProfileField("zh", "description"); got != "保留" {
		t.Errorf("description should survive, got %q", got)
	}
}

// TestResetBotProfile verifies the whole section is removed.
func TestResetBotProfile(t *testing.T) {
	path := writeTempConfig(t, `BOT_TOKEN = t

[bot_profile.zh]
name = 名字
description = 简介

[plugins.netease]
api_url = https://x
`)
	conf, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if err := conf.ResetBotProfile("zh"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, ok := conf.GetBotProfile("zh"); ok {
		t.Error("zh profile should be gone in memory")
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.GetBotProfile("zh"); ok {
		t.Error("zh profile should be gone after reload")
	}
	// An unrelated plugin section must survive the reset.
	if got := reloaded.GetPluginString("netease", "api_url"); got != "https://x" {
		t.Errorf("netease section clobbered: %q", got)
	}
	// Resetting a non-existent language is a no-op, not an error.
	if err := reloaded.ResetBotProfile("ja"); err != nil {
		t.Errorf("reset of absent lang should be nil, got %v", err)
	}
}

// TestBotProfileMultilineThenSingle reproduces the startup-failure bug: a
// multi-line value (wrapped in backticks across several physical lines) that is
// later overwritten with a single-line value used to leave the old
// continuation lines orphaned in the file. On reload those orphaned lines have
// no "=" and the INI parser rejected them with "key-value delimiter not found".
func TestBotProfileMultilineThenSingle(t *testing.T) {
	path := writeTempConfig(t, "BOT_TOKEN = t\n")
	conf, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	multi := "複数プラットフォームからの音楽ダウンロードに対応\n" +
		"フィードバック： @BDovo\n" +
		"チャンネル： @bakaPD\n" +
		"オープンソース： https://obdo.cc/MusicBot-Go"
	if err := conf.SetBotProfileField("ja", "short_description", multi); err != nil {
		t.Fatalf("set multi: %v", err)
	}
	// The round-trip through disk must preserve the multi-line value exactly.
	if r, err := Load(path); err != nil {
		t.Fatalf("reload after multi: %v", err)
	} else if got := r.GetBotProfileField("ja", "short_description"); got != multi {
		t.Fatalf("multi value mismatch: %q", got)
	}

	// Overwrite with a single-line value.
	if err := conf.SetBotProfileField("ja", "short_description", "単一行の説明"); err != nil {
		t.Fatalf("set single: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if strings.Contains(string(raw), "@BDovo") {
		t.Fatalf("orphaned continuation lines left behind:\n%s", raw)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload after single (the bug): %v", err)
	}
	if got := reloaded.GetBotProfileField("ja", "short_description"); got != "単一行の説明" {
		t.Fatalf("value mismatch: %q", got)
	}
}

// TestBotProfileDeleteMultiline verifies clearing a multi-line value removes the
// whole block, including continuation lines, and leaves sibling keys intact.
func TestBotProfileDeleteMultiline(t *testing.T) {
	path := writeTempConfig(t, "BOT_TOKEN = t\n")
	conf, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	multi := "line one\nline two\nline three"
	if err := conf.SetBotProfileField("ja", "short_description", multi); err != nil {
		t.Fatalf("set multi: %v", err)
	}
	if err := conf.SetBotProfileField("ja", "name", "BotName"); err != nil {
		t.Fatalf("set name: %v", err)
	}

	// Clearing the multi-line value must not orphan its continuation lines.
	if err := conf.SetBotProfileField("ja", "short_description", ""); err != nil {
		t.Fatalf("clear multi: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if strings.Contains(string(raw), "line two") {
		t.Fatalf("orphaned continuation lines after delete:\n%s", raw)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload after delete: %v", err)
	}
	if got := reloaded.GetBotProfileField("ja", "short_description"); got != "" {
		t.Fatalf("short_description should be gone, got %q", got)
	}
	// The sibling key must survive.
	if got := reloaded.GetBotProfileField("ja", "name"); got != "BotName" {
		t.Fatalf("sibling name clobbered: %q", got)
	}
}

// TestBotProfileSetRejectsTooLong verifies the per-field character limit is
// enforced before persisting, counting runes (not bytes) so multi-byte CJK
// text gets its true character count.
func TestBotProfileSetRejectsTooLong(t *testing.T) {
	path := writeTempConfig(t, "BOT_TOKEN = t\n")
	conf, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// name limit is 64 chars; 65 CJK runes (195 bytes) must be rejected.
	tooLong := strings.Repeat("名", 65)
	if err := conf.SetBotProfileField("zh", "name", tooLong); err == nil {
		t.Fatal("expected name over 64 chars to be rejected")
	}
	// Exactly 64 runes is allowed.
	ok64 := strings.Repeat("名", 64)
	if err := conf.SetBotProfileField("zh", "name", ok64); err != nil {
		t.Fatalf("64-char name should be allowed, got: %v", err)
	}
	if got := conf.GetBotProfileField("zh", "name"); got != ok64 {
		t.Fatalf("stored name mismatch")
	}

	// short_description limit is 120.
	if err := conf.SetBotProfileField("zh", "short_description", strings.Repeat("x", 121)); err == nil {
		t.Fatal("expected short_description over 120 to be rejected")
	}
	// description limit is 512.
	if err := conf.SetBotProfileField("zh", "description", strings.Repeat("x", 513)); err == nil {
		t.Fatal("expected description over 512 to be rejected")
	}
}
