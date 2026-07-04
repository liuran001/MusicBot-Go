package httpproxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestBaiduRetryTransportRetriesTransientGET(t *testing.T) {
	var attempts int
	transport := &baiduRetryTransport{
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return nil, context.DeadlineExceeded
			}
			return response(http.StatusOK), nil
		}),
		maxAttempts:    3,
		attemptTimeout: time.Second,
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestBaiduRetryTransportKeepsSuccessfulBodyContextAlive(t *testing.T) {
	transport := &baiduRetryTransport{
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       &contextResponseBody{ctx: req.Context()},
			}, nil
		}),
		maxAttempts:    3,
		attemptTimeout: time.Second,
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
}

func TestBaiduRetryTransportRetriesGatewayStatus(t *testing.T) {
	var attempts int
	transport := &baiduRetryTransport{
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return response(http.StatusBadGateway), nil
			}
			return response(http.StatusOK), nil
		}),
		maxAttempts: 3,
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestBaiduRetryTransportDoesNotRetryPOST(t *testing.T) {
	var attempts int
	transport := &baiduRetryTransport{
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return nil, context.DeadlineExceeded
		}),
		maxAttempts: 3,
	}
	req, err := http.NewRequest(http.MethodPost, "https://example.com/test", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = transport.RoundTrip(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RoundTrip() error = %v, want context deadline exceeded", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestBaiduRetryTransportStopsWhenParentContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	attempts := 0
	transport := &baiduRetryTransport{
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			attempts++
			mu.Unlock()
			cancel()
			return nil, context.Canceled
		}),
		maxAttempts: 3,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/test", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = transport.RoundTrip(req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RoundTrip() error = %v, want context canceled", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestBaiduRetryTransportSplitsParentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var attemptDeadline time.Time
	transport := &baiduRetryTransport{
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attemptDeadline, _ = req.Context().Deadline()
			return response(http.StatusOK), nil
		}),
		maxAttempts:    3,
		attemptTimeout: 20 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/test", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()
	remaining := time.Until(attemptDeadline)
	if remaining <= 0 || remaining > 1500*time.Millisecond {
		t.Fatalf("first attempt deadline remaining = %v, want about 1s", remaining)
	}
}

func response(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ok")),
	}
}

type contextResponseBody struct {
	ctx  context.Context
	read bool
}

func (body *contextResponseBody) Read(p []byte) (int, error) {
	if err := body.ctx.Err(); err != nil {
		return 0, err
	}
	if body.read {
		return 0, io.EOF
	}
	body.read = true
	return copy(p, "ok"), nil
}

func (body *contextResponseBody) Close() error {
	return nil
}
