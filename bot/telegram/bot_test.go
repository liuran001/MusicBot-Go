package telegram

import (
	"testing"
)

func TestAllowedUpdates(t *testing.T) {
	want := []string{
		"message",
		"callback_query",
		"inline_query",
		"chosen_inline_result",
	}

	got := AllowedUpdates()
	if len(got) != len(want) {
		t.Fatalf("AllowedUpdates() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("AllowedUpdates()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	got[0] = "edited_message"
	again := AllowedUpdates()
	if again[0] != want[0] {
		t.Fatalf("AllowedUpdates() should return a copy, got first value %q, want %q", again[0], want[0])
	}
}

func TestLongPollingParams(t *testing.T) {
	params := LongPollingParams()
	if params == nil {
		t.Fatal("LongPollingParams() = nil")
	}
	if params.Timeout != longPollingTimeoutSeconds {
		t.Fatalf("LongPollingParams().Timeout = %d, want %d", params.Timeout, longPollingTimeoutSeconds)
	}

	want := AllowedUpdates()
	if len(params.AllowedUpdates) != len(want) {
		t.Fatalf("LongPollingParams().AllowedUpdates len = %d, want %d", len(params.AllowedUpdates), len(want))
	}
	for i := range want {
		if params.AllowedUpdates[i] != want[i] {
			t.Fatalf("LongPollingParams().AllowedUpdates[%d] = %q, want %q", i, params.AllowedUpdates[i], want[i])
		}
	}

	params.AllowedUpdates[0] = "edited_message"
	if AllowedUpdates()[0] != want[0] {
		t.Fatalf("LongPollingParams() should not share backing array with AllowedUpdates")
	}
}

func TestWebhookParams(t *testing.T) {
	params := WebhookParams("https://example.com/hook", "secret-token")
	if params == nil {
		t.Fatal("WebhookParams() = nil")
	}
	if params.URL != "https://example.com/hook" {
		t.Fatalf("WebhookParams().URL = %q", params.URL)
	}
	if params.SecretToken != "secret-token" {
		t.Fatalf("WebhookParams().SecretToken = %q", params.SecretToken)
	}

	want := AllowedUpdates()
	if len(params.AllowedUpdates) != len(want) {
		t.Fatalf("WebhookParams().AllowedUpdates len = %d, want %d", len(params.AllowedUpdates), len(want))
	}
	for i := range want {
		if params.AllowedUpdates[i] != want[i] {
			t.Fatalf("WebhookParams().AllowedUpdates[%d] = %q, want %q", i, params.AllowedUpdates[i], want[i])
		}
	}

	params.AllowedUpdates[0] = "edited_message"
	if AllowedUpdates()[0] != want[0] {
		t.Fatalf("WebhookParams() should not share backing array with AllowedUpdates")
	}
}
