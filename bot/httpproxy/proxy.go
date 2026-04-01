package httpproxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTimeout            = 20 * time.Second
	defaultBaiduConnectTarget = "153.3.236.22:443"
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
		return &http.Client{Transport: transport, Timeout: timeout}, nil
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
	if len(keys) <= 1 {
		return keys
	}
	for i := 0; i < len(keys)-1; i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
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
