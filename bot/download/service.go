package download

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type ProgressFunc func(written, total int64)

type DownloadService struct {
	client       *http.Client
	timeout      time.Duration
	reverseProxy string
	checkMD5     bool
}

type DownloadServiceOptions struct {
	Timeout      time.Duration
	ReverseProxy string
	CheckMD5     bool
}

func NewDownloadService(opts DownloadServiceOptions) *DownloadService {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   minDuration(opts.Timeout, 10*time.Second),
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   minDuration(opts.Timeout, 10*time.Second),
		ResponseHeaderTimeout: minDuration(opts.Timeout, 10*time.Second),
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &DownloadService{
		client: &http.Client{
			Transport: transport,
		},
		timeout:      opts.Timeout,
		reverseProxy: strings.TrimSpace(opts.ReverseProxy),
		checkMD5:     opts.CheckMD5,
	}
}

func (s *DownloadService) Download(ctx context.Context, info *platform.DownloadInfo, destPath string, progress ProgressFunc) (int64, error) {
	if info == nil || info.URL == "" {
		return 0, errors.New("download info missing")
	}
	if destPath == "" {
		return 0, errors.New("dest path missing")
	}

	if err := os.MkdirAll(filepath.Dir(destPath), os.ModePerm); err != nil {
		return 0, err
	}

	baseURL := rewriteNeteaseHost(info.URL)
	originalHost := hostFromURL(baseURL)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		useProxy := attempt > 0 && s.reverseProxy != ""
		written, err := s.downloadOnce(ctx, baseURL, originalHost, info, destPath, progress, useProxy)
		if err == nil {
			if info.Size > 0 && written != info.Size {
				_ = os.Remove(destPath)
				return 0, fmt.Errorf("incomplete download: got %d bytes, expected %d", written, info.Size)
			}
			if s.checkMD5 && info.MD5 != "" {
				if ok, err := verifyMD5(destPath, info.MD5); err != nil || !ok {
					_ = os.Remove(destPath)
					if err != nil {
						return 0, err
					}
					return 0, errors.New("md5 verification failed")
				}
			}
			return written, nil
		}
		lastErr = err
		_ = os.Remove(destPath)
		if attempt < 2 {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
	}
	return 0, lastErr
}

func (s *DownloadService) downloadOnce(ctx context.Context, rawURL, originalHost string, info *platform.DownloadInfo, destPath string, progress ProgressFunc, useProxy bool) (int64, error) {
	client := s.client
	requestURL := rawURL
	overrideAddr := ""
	if useProxy {
		overrideAddr = s.reverseProxy
	}
	if overrideAddr != "" {
		proxyURL, err := replaceHost(rawURL, overrideAddr)
		if err != nil {
			return 0, err
		}
		requestURL = proxyURL
		client = s.newClientForOverride(originalHost, overrideAddr, rawURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, err
	}
	for k, v := range info.Headers {
		req.Header.Set(k, v)
	}
	if originalHost != "" && overrideAddr != "" {
		req.Host = originalHost
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	file, err := os.Create(destPath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	return copyWithProgress(file, resp.Body, info.Size, progress)
}

func (s *DownloadService) newClientForOverride(serverName, overrideAddr, rawURL string) *http.Client {
	addr := overrideAddr
	if !strings.Contains(addr, ":") {
		parsed, err := url.Parse(rawURL)
		if err == nil {
			port := parsed.Port()
			if port == "" {
				if parsed.Scheme == "https" {
					port = "443"
				} else {
					port = "80"
				}
			}
			addr = net.JoinHostPort(addr, port)
		}
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   minDuration(s.timeout, 10*time.Second),
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   minDuration(s.timeout, 10*time.Second),
		ResponseHeaderTimeout: s.timeout,
		ExpectContinueTimeout: 1 * time.Second,
	}

	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: minDuration(s.timeout, 10*time.Second)}).DialContext(ctx, network, addr)
	}
	if serverName != "" {
		transport.TLSClientConfig = &tls.Config{ServerName: serverName}
	}

	return &http.Client{Transport: transport}
}

func rewriteNeteaseHost(rawURL string) string {
	replacer := strings.NewReplacer("m8.", "m7.", "m801.", "m701.", "m804.", "m701.", "m704.", "m701.")
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parsed.Host = replacer.Replace(parsed.Host)
	return parsed.String()
}

func hostFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func replaceHost(rawURL, newHost string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	parsed.Host = newHost
	return parsed.String(), nil
}

func copyWithProgress(dst io.Writer, src io.Reader, total int64, progress ProgressFunc) (int64, error) {
	buf := make([]byte, 128*1024)
	var written int64
	lastUpdate := time.Now()

	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return written, werr
			}
			written += int64(n)
			if progress != nil && time.Since(lastUpdate) >= 2*time.Second {
				progress(written, total)
				lastUpdate = time.Now()
			}
		}
		if err != nil {
			if err == io.EOF {
				return written, nil
			}
			return written, err
		}
	}
}

func verifyMD5(filePath, expected string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	h := md5.New()
	if _, err := io.Copy(h, file); err != nil {
		return false, err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	return strings.EqualFold(actual, expected), nil
}

func minDuration(a, b time.Duration) time.Duration {
	if a == 0 || a > b {
		return b
	}
	return a
}
