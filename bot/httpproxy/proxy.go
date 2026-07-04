package httpproxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTimeout            = 20 * time.Second
	defaultBaiduConnectTarget = "153.3.236.22:443"
	defaultBaiduRetryAttempts = 3
	defaultBaiduRetryBackoff  = 200 * time.Millisecond
)

type Config struct {
	Enabled bool
	Type    string
	Host    string
	Port    int
	Auth    string
	Headers map[string]string
}

func (cfg Config) Normalized() Config {
	rawHost := strings.TrimSpace(cfg.Host)
	host, port := normalizeEndpoint(rawHost, cfg.Port)
	headers := map[string]string{}
	for key, value := range cfg.Headers {
		key = canonicalMIMEHeaderKey(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		headers[key] = value
	}
	return Config{
		Enabled: cfg.Enabled,
		Type:    normalizeType(cfg.Type),
		Host:    host,
		Port:    port,
		Auth:    strings.TrimSpace(cfg.Auth),
		Headers: headers,
	}
}

func (cfg Config) EnabledAndReady() bool {
	normalized := cfg.Normalized()
	return normalized.Enabled && strings.TrimSpace(normalized.Host) != "" && normalized.Port > 0
}

func (cfg Config) Address() string {
	normalized := cfg.Normalized()
	if strings.TrimSpace(normalized.Host) == "" || normalized.Port <= 0 {
		return ""
	}
	return net.JoinHostPort(normalized.Host, strconv.Itoa(normalized.Port))
}

func (cfg Config) CloneHeaders() map[string]string {
	normalized := cfg.Normalized()
	if len(normalized.Headers) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(normalized.Headers))
	for key, value := range normalized.Headers {
		cloned[key] = value
	}
	return cloned
}

func ParseHeaders(raw string) map[string]string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\n`, "\n"))
	if raw == "" {
		return map[string]string{}
	}
	parsedJSON := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &parsedJSON); err == nil {
		result := make(map[string]string, len(parsedJSON))
		for key, value := range parsedJSON {
			key = canonicalMIMEHeaderKey(strings.TrimSpace(key))
			value = strings.TrimSpace(value)
			if key == "" || value == "" {
				continue
			}
			result[key] = value
		}
		return result
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '|'
	})
	result := make(map[string]string, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pair := strings.SplitN(part, ":", 2)
		if len(pair) != 2 {
			continue
		}
		key := canonicalMIMEHeaderKey(strings.TrimSpace(pair[0]))
		value := strings.TrimSpace(pair[1])
		if key == "" || value == "" {
			continue
		}
		result[key] = value
	}
	return result
}

func NewHTTPClient(cfg Config, timeout time.Duration) (*http.Client, error) {
	normalized := cfg.Normalized()
	if !normalized.EnabledAndReady() {
		return nil, nil
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	switch normalized.Type {
	case "baidu":
		transport := NewBaseTransport(timeout)
		transport.DialContext = newBaiduConnectDialContext(normalized, timeout)
		retryTransport := &baiduRetryTransport{
			base:           transport,
			maxAttempts:    defaultBaiduRetryAttempts,
			attemptTimeout: timeout / defaultBaiduRetryAttempts,
			backoff:        defaultBaiduRetryBackoff,
		}
		return &http.Client{Transport: retryTransport, Timeout: timeout}, nil
	case "http", "https":
		proxyURL, err := buildStandardProxyURL(normalized)
		if err != nil {
			return nil, err
		}
		transport := NewBaseTransport(timeout)
		transport.Proxy = http.ProxyURL(proxyURL)
		if headers := buildStandardProxyConnectHeader(normalized); len(headers) > 0 {
			transport.ProxyConnectHeader = headers
		}
		return &http.Client{Transport: transport, Timeout: timeout}, nil
	default:
		return nil, fmt.Errorf("unsupported api proxy type: %s", normalized.Type)
	}
}

type baiduRetryTransport struct {
	base           http.RoundTripper
	maxAttempts    int
	attemptTimeout time.Duration
	backoff        time.Duration
}

func (t *baiduRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	attempts := t.maxAttempts
	if attempts <= 1 || !isRetryableRequest(req) {
		return base.RoundTrip(req)
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptReq, cancel, err := t.requestForAttempt(req, attempt)
		if err != nil {
			return nil, err
		}
		resp, roundTripErr := base.RoundTrip(attemptReq)
		attemptTimedOut := attemptReq.Context().Err() == context.DeadlineExceeded

		if roundTripErr == nil && !isRetryableStatus(resp.StatusCode) {
			resp.Body = cancelOnClose(resp.Body, cancel)
			return resp, nil
		}
		if req.Context().Err() != nil {
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			cancel()
			return nil, req.Context().Err()
		}
		if roundTripErr != nil {
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			cancel()
			lastErr = roundTripErr
			if !attemptTimedOut && !isRetryableTransportError(roundTripErr) {
				return nil, roundTripErr
			}
		} else {
			if attempt == attempts-1 {
				resp.Body = cancelOnClose(resp.Body, cancel)
				return resp, nil
			}
			lastErr = fmt.Errorf("baidu proxy upstream returned http %d", resp.StatusCode)
			drainAndClose(resp.Body)
			cancel()
		}
		if attempt == attempts-1 {
			break
		}
		if err := waitForRetry(req.Context(), t.retryDelay(attempt)); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (t *baiduRetryTransport) requestForAttempt(req *http.Request, attempt int) (*http.Request, context.CancelFunc, error) {
	attemptReq := req.Clone(req.Context())
	if attempt > 0 && req.Body != nil {
		if req.GetBody == nil {
			return nil, func() {}, fmt.Errorf("cannot retry request body")
		}
		body, err := req.GetBody()
		if err != nil {
			return nil, func() {}, err
		}
		attemptReq.Body = body
	}
	if t.attemptTimeout <= 0 {
		return attemptReq, func() {}, nil
	}
	timeout := t.attemptTimeout
	if deadline, ok := req.Context().Deadline(); ok {
		remaining := time.Until(deadline)
		attemptsLeft := t.maxAttempts - attempt
		if attemptsLeft > 0 {
			shared := remaining / time.Duration(attemptsLeft)
			if shared < timeout {
				timeout = shared
			}
		}
	}
	if timeout <= 0 {
		return attemptReq, func() {}, nil
	}
	attemptCtx, cancel := context.WithTimeout(req.Context(), timeout)
	return attemptReq.Clone(attemptCtx), cancel, nil
}

func (t *baiduRetryTransport) retryDelay(attempt int) time.Duration {
	if t.backoff <= 0 {
		return 0
	}
	return t.backoff * time.Duration(attempt+1)
}

func isRetryableRequest(req *http.Request) bool {
	if req == nil {
		return false
	}
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
	default:
		return false
	}
}

func isRetryableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func isRetryableTransportError(err error) bool {
	return err != nil && !errors.Is(err, context.Canceled)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, 4<<10)
	_ = body.Close()
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func cancelOnClose(body io.ReadCloser, cancel context.CancelFunc) io.ReadCloser {
	if body == nil {
		cancel()
		return nil
	}
	return &cancelOnCloseBody{ReadCloser: body, cancel: cancel}
}

func (body *cancelOnCloseBody) Close() error {
	err := body.ReadCloser.Close()
	body.cancel()
	return err
}

func NewBaseTransport(timeout time.Duration) *http.Transport {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   minDuration(timeout, 10*time.Second),
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   minDuration(timeout, 10*time.Second),
		ResponseHeaderTimeout: minDuration(timeout, 10*time.Second),
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func normalizeType(proxyType string) string {
	proxyType = strings.ToLower(strings.TrimSpace(proxyType))
	if proxyType == "" {
		return "http"
	}
	return proxyType
}

func normalizeEndpoint(host string, port int) (string, int) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", port
	}
	if strings.Contains(host, "://") {
		if parsed, err := url.Parse(host); err == nil {
			if parsedHost := strings.TrimSpace(parsed.Hostname()); parsedHost != "" {
				host = parsedHost
			}
			if parsedPort, err := strconv.Atoi(parsed.Port()); err == nil && parsedPort > 0 {
				port = parsedPort
			}
		}
	}
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
		if value, err := strconv.Atoi(parsedPort); err == nil && value > 0 {
			port = value
		}
	}
	return strings.Trim(host, "[]"), port
}

func buildStandardProxyURL(cfg Config) (*url.URL, error) {
	proxyURL := &url.URL{Scheme: cfg.Type, Host: cfg.Address()}
	if strings.TrimSpace(cfg.Auth) != "" {
		if username, password, ok := strings.Cut(cfg.Auth, ":"); ok {
			proxyURL.User = url.UserPassword(username, password)
		} else {
			proxyURL.User = url.User(cfg.Auth)
		}
	}
	return proxyURL, nil
}

func buildStandardProxyConnectHeader(cfg Config) http.Header {
	headers := http.Header{}
	for key, value := range cfg.CloneHeaders() {
		headers.Set(key, value)
	}
	if strings.TrimSpace(cfg.Auth) != "" && headers.Get("Proxy-Authorization") == "" {
		encoded := base64.StdEncoding.EncodeToString([]byte(cfg.Auth))
		headers.Set("Proxy-Authorization", "Basic "+encoded)
	}
	return headers
}

func newBaiduConnectDialContext(cfg Config, timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	proxyAddress := cfg.Address()
	connectHeaders := buildBaiduConnectHeaders(cfg)
	dialer := &net.Dialer{
		Timeout:   minDuration(timeout, 10*time.Second),
		KeepAlive: 30 * time.Second,
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, proxyAddress)
		if err != nil {
			return nil, err
		}
		if err := writeConnectRequest(ctx, conn, addr, connectHeaders); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	}
}

func buildBaiduConnectHeaders(cfg Config) map[string]string {
	headers := cfg.CloneHeaders()
	if strings.TrimSpace(headers["Host"]) == "" {
		headers["Host"] = defaultBaiduConnectTarget
	}
	if strings.TrimSpace(cfg.Auth) != "" && strings.TrimSpace(headers["X-T5-Auth"]) == "" {
		headers["X-T5-Auth"] = strings.TrimSpace(cfg.Auth)
	}
	if strings.TrimSpace(headers["User-Agent"]) == "" {
		headers["User-Agent"] = "Mozilla/5.0"
	}
	return headers
}

func writeConnectRequest(ctx context.Context, conn net.Conn, targetAddr string, headers map[string]string) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer conn.SetDeadline(time.Time{})
	}
	targetAddr = strings.TrimSpace(targetAddr)
	if targetAddr == "" {
		return fmt.Errorf("empty connect target")
	}
	var builder strings.Builder
	builder.WriteString("CONNECT ")
	builder.WriteString(targetAddr)
	builder.WriteString(" HTTP/1.1\r\n")
	for _, key := range sortedHeaderKeys(headers) {
		value := strings.TrimSpace(headers[key])
		if key == "" || value == "" {
			continue
		}
		builder.WriteString(key)
		builder.WriteString(": ")
		builder.WriteString(value)
		builder.WriteString("\r\n")
	}
	builder.WriteString("\r\n")
	if _, err := conn.Write([]byte(builder.String())); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("baidu connect proxy http %d", resp.StatusCode)
	}
	return nil
}

func sortedHeaderKeys(headers map[string]string) []string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func canonicalMIMEHeaderKey(value string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(value)), "-")
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "-")
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 || a < b {
		return a
	}
	return b
}
