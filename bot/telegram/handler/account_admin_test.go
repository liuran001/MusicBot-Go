package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type loginSignTestPlatform struct {
	*stubPlatform
	message string
	called  bool
}

func (p *loginSignTestPlatform) Metadata() platform.Meta {
	return platform.Meta{
		Name:        "kugou",
		DisplayName: "酷狗音乐",
		Aliases:     []string{"kugou", "kg", "酷狗"},
	}
}

func (p *loginSignTestPlatform) SupportedLoginMethods() []string {
	return []string{"qr", "sign", "renew", "auto", "status"}
}

func (p *loginSignTestPlatform) SignIn(ctx context.Context) (string, error) {
	_ = ctx
	p.called = true
	return p.message, nil
}

func TestHandleAccountLoginDispatchesPlatformSign(t *testing.T) {
	manager := newStubManager()
	plat := &loginSignTestPlatform{stubPlatform: newStubPlatform("kugou"), message: "概念版签到成功"}
	manager.Register(plat)

	resp, err := handleAccountLogin(context.Background(), manager, "kugou sign")
	if err != nil {
		t.Fatalf("handleAccountLogin() error = %v", err)
	}
	if !plat.called {
		t.Fatal("expected SignIn to be called")
	}
	if resp == nil || !strings.Contains(resp.Text, "概念版签到成功") {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandleAccountLoginDispatchesGlobalSignAlias(t *testing.T) {
	manager := newStubManager()
	plat := &loginSignTestPlatform{stubPlatform: newStubPlatform("kugou"), message: "全局签到成功"}
	manager.Register(plat)

	resp, err := handleAccountLogin(context.Background(), manager, "sign kugou")
	if err != nil {
		t.Fatalf("handleAccountLogin() error = %v", err)
	}
	if !plat.called {
		t.Fatal("expected global SignIn to be called")
	}
	if resp == nil || !strings.Contains(resp.Text, "全局签到成功") {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestBuildPlatformLoginHelpIncludesSignExample(t *testing.T) {
	manager := newStubManager()
	plat := &loginSignTestPlatform{stubPlatform: newStubPlatform("kugou")}
	manager.Register(plat)

	text := buildPlatformLoginHelp(manager, plat)
	for _, want := range []string{"支持: qr, sign, renew, auto, status", "/login kugou sign", "/login sign kugou"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected help contains %q, got: %s", want, text)
		}
	}
}

func TestHandleAccountLoginGlobalSignUsageOnExtraArgs(t *testing.T) {
	manager := newStubManager()
	plat := &loginSignTestPlatform{stubPlatform: newStubPlatform("kugou")}
	manager.Register(plat)

	resp, err := handleAccountLogin(context.Background(), manager, "sign kugou extra")
	if err != nil {
		t.Fatalf("handleAccountLogin() error = %v", err)
	}
	if plat.called {
		t.Fatal("expected SignIn not to be called on invalid global sign args")
	}
	if resp == nil || !strings.Contains(resp.Text, "/login sign <platform>") {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

type loginCookieOnlyTestPlatform struct {
	*stubPlatform
}

func (p *loginCookieOnlyTestPlatform) Metadata() platform.Meta {
	return platform.Meta{Name: "soda", DisplayName: "汽水音乐", Aliases: []string{"soda", "qs"}}
}

func (p *loginCookieOnlyTestPlatform) SupportedLoginMethods() []string {
	return []string{"cookie", "status"}
}

func TestBuildPlatformLoginHelpMatchesSupportedMethods(t *testing.T) {
	manager := newStubManager()
	plat := &loginCookieOnlyTestPlatform{stubPlatform: newStubPlatform("soda")}
	manager.Register(plat)

	text := buildPlatformLoginHelp(manager, plat)
	if !strings.Contains(text, "/login soda cookie <cookie>") {
		t.Fatalf("expected cookie example, got: %s", text)
	}
	for _, unwanted := range []string{"/login soda renew", "/login soda auto on 21600", "/login soda sign", "/login soda qr"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("expected help not contains %q, got: %s", unwanted, text)
		}
	}
}
