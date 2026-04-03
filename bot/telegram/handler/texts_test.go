package handler

import (
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
)

func TestBuildHelpTextIncludesAccountCommandsForAdmin(t *testing.T) {
	adminCommands := []admincmd.Command{
		{Name: "checkck", Description: "检查插件 Cookie 有效性"},
		{Name: "login", Description: "统一账号登录（qr/cookie/renew/auto/help）"},
	}

	text := buildHelpText(nil, true, adminCommands, false, true)

	if !strings.Contains(text, "管理员命令") {
		t.Fatalf("expected admin command section, got: %s", text)
	}
	if strings.Contains(text, "账号命令") {
		t.Fatalf("expected no separate account section, got: %s", text)
	}
	for _, cmd := range []string{"/login"} {
		if !strings.Contains(text, cmd) {
			t.Fatalf("expected help text contains %s, got: %s", cmd, text)
		}
	}
	if strings.Count(text, "/login") != 1 {
		t.Fatalf("expected /login appears once, got: %s", text)
	}
}

func TestBuildHelpTextDoesNotShowAccountCommandsForNonAdmin(t *testing.T) {
	adminCommands := []admincmd.Command{
		{Name: "login", Description: "统一账号登录（qr/cookie/renew/auto/help）"},
	}

	text := buildHelpText(nil, false, adminCommands, false, true)

	if strings.Contains(text, "账号命令") {
		t.Fatalf("expected non-admin help hides legacy account section, got: %s", text)
	}
	if strings.Contains(text, "/login") {
		t.Fatalf("expected non-admin help hides account commands, got: %s", text)
	}
}
