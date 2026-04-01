package netease

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/httpproxy"
)

func TestWithRetryStopsOnSuccess(t *testing.T) {
	client := New("", true, nil)
	client.maxRetries = 2
	client.minBackoff = time.Millisecond
	client.maxBackoff = time.Millisecond

	attempts := 0
	err := client.withRetry(context.Background(), func() error {
		attempts++
		if attempts < 2 {
			return errors.New("fail")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestWithRetryRespectsContext(t *testing.T) {
	client := New("", true, nil)
	client.maxRetries = 5
	client.minBackoff = time.Millisecond
	client.maxBackoff = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.withRetry(ctx, func() error {
		return errors.New("fail")
	})
	if err == nil {
		t.Fatalf("expected context error")
	}
}

func TestSetAPIProxyDisabledUsesDirectClient(t *testing.T) {
	client := New("", true, nil)

	if err := client.SetAPIProxy(httpproxy.Config{}); err != nil {
		t.Fatalf("SetAPIProxy() error = %v", err)
	}

	data := client.requestData()
	if data.Client == nil {
		t.Fatal("expected requestData client to be initialized")
	}
	if data.Client.Transport != nil {
		t.Fatalf("expected direct client transport to be nil, got %#v", data.Client.Transport)
	}
}

func TestRequestDataKeepsConfiguredClient(t *testing.T) {
	client := New("", true, nil)
	expected := &http.Client{Timeout: 3 * time.Second}
	client.baseData.Client = expected

	data := client.requestData()
	if data.Client != expected {
		t.Fatal("expected requestData to reuse configured HTTP client")
	}
}
