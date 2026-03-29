package handler

import (
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
)

func TestBuildHelpTextIncludesConceptCommandsForAdmin(t *testing.T) {
	adminCommands := []admincmd.Command{
		{Name: "checkck", Description: "检查插件 Cookie 有效性"},
		{Name: "kgqr", Description: "生成酷狗概念版扫码二维码"},
		{Name: "kgstatus", Description: "查看酷狗概念版会话状态"},
		{Name: "kgsign", Description: "尝试酷狗概念版签到/领 VIP"},
	}

	text := buildHelpText(nil, true, adminCommands, false, true)

	if !strings.Contains(text, "酷狗概念版命令") {
		t.Fatalf("expected concept command section, got: %s", text)
	}
	for _, cmd := range []string{"/kgqr", "/kgstatus", "/kgsign"} {
		if !strings.Contains(text, cmd) {
			t.Fatalf("expected help text contains %s, got: %s", cmd, text)
		}
	}
	if strings.Count(text, "/kgqr") != 1 {
		t.Fatalf("expected /kgqr appears once, got: %s", text)
	}
}

func TestBuildHelpTextDoesNotShowConceptCommandsForNonAdmin(t *testing.T) {
	adminCommands := []admincmd.Command{
		{Name: "kgqr", Description: "生成酷狗概念版扫码二维码"},
		{Name: "kgstatus", Description: "查看酷狗概念版会话状态"},
	}

	text := buildHelpText(nil, false, adminCommands, false, true)

	if strings.Contains(text, "酷狗概念版命令") {
		t.Fatalf("expected non-admin help hides concept commands, got: %s", text)
	}
	if strings.Contains(text, "/kgqr") || strings.Contains(text, "/kgstatus") {
		t.Fatalf("expected non-admin help hides concept commands, got: %s", text)
	}
}
