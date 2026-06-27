package i18n

import (
	"context"
	"testing"
)

func TestInitAndResolve(t *testing.T) {
	if _, err := Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	cases := []struct {
		name     string
		override string
		client   string
		want     string
	}{
		{"override wins", "ja", "en", "ja"},
		{"client used when no override", "", "zh-hans", "zh"},
		{"ietf primary subtag", "", "pt-BR", "en"}, // pt not shipped -> fallback
		{"empty -> default", "", "", "en"},
		{"unsupported override falls through to client", "ko", "zh", "zh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Resolve(c.override, c.client); got != c.want {
				t.Fatalf("Resolve(%q,%q)=%q want %q", c.override, c.client, got, c.want)
			}
		})
	}
}

func TestLocalizeAndFallback(t *testing.T) {
	if _, err := Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	zh := For("zh")
	if got := zh.T("no_results"); got != "没有找到结果，换个关键词试试" {
		t.Fatalf("zh no_results = %q", got)
	}
	en := For("en")
	if got := en.T("no_results"); got == "" || got == "no_results" {
		t.Fatalf("en no_results not localized: %q", got)
	}

	// Template interpolation.
	got := zh.T("upload_queue_ahead", map[string]any{"Count": 3})
	if got != "当前正在发送队列中，前面还有 3 个任务" {
		t.Fatalf("interpolation failed: %q", got)
	}

	// Missing key echoes the id (visible failure, not blank).
	if got := zh.T("does_not_exist"); got != "does_not_exist" {
		t.Fatalf("missing key = %q want echo", got)
	}
}

func TestContextRoundTrip(t *testing.T) {
	if _, err := Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ctx := WithLocalizer(context.Background(), For("ja"))
	if got := From(ctx).Lang(); got != "ja" {
		t.Fatalf("From(ctx).Lang() = %q want ja", got)
	}
	// No localizer in ctx -> default language, never nil.
	if From(context.Background()) == nil {
		t.Fatal("From(empty) returned nil")
	}
}

func TestMarkdownEscape(t *testing.T) {
	in := "MusicBot-Go v1.0 (beta)."
	got := EscapeMarkdownV2(in)
	want := "MusicBot\\-Go v1\\.0 \\(beta\\)\\."
	if got != want {
		t.Fatalf("escape = %q want %q", got, want)
	}
}
