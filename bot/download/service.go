package download

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/util"
)

type ProgressFunc = util.ProgressFunc

type DownloadService struct {
	client              *http.Client
	timeout             time.Duration
	reverseProxy        string
	checkMD5            bool
	multipartEnabled    bool
	multipartOpts       MultipartDownloadOptions
	multipartDownloader *MultipartDownloader
}

type DownloadServiceOptions struct {
	Timeout              time.Duration
	ReverseProxy         string
	CheckMD5             bool
	EnableMultipart      bool
	MultipartConcurrency int
	MultipartMinSize     int64
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

	client := &http.Client{
		Transport: transport,
	}

	s := &DownloadService{
		client:           client,
		timeout:          opts.Timeout,
		reverseProxy:     strings.TrimSpace(opts.ReverseProxy),
		checkMD5:         opts.CheckMD5,
		multipartEnabled: opts.EnableMultipart,
	}

	if opts.EnableMultipart {
		s.multipartOpts = MultipartDownloadOptions{
			Concurrency: opts.MultipartConcurrency,
			MinSize:     opts.MultipartMinSize,
		}
		s.multipartDownloader = NewMultipartDownloader(client, opts.Timeout, s.multipartOpts)
	}

	return s
}

func (s *DownloadService) Download(ctx context.Context, info *platform.DownloadInfo, destPath string, progress ProgressFunc) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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

	if s.multipartEnabled && s.multipartDownloader != nil {
		written, err := s.tryMultipartDownload(ctx, baseURL, info, destPath, progress)
		if err == nil {
			if s.checkMD5 && info.MD5 != "" {
				if ok, err := util.VerifyMD5(destPath, info.MD5); err != nil || !ok {
					_ = os.Remove(destPath)
					if err != nil {
						return 0, err
					}
					return 0, errors.New("md5 verification failed")
				}
			}
			return written, nil
		}
	}

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
				if ok, err := util.VerifyMD5(destPath, info.MD5); err != nil || !ok {
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
			wait := time.Duration(1<<attempt) * time.Second
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	return 0, lastErr
}

func (s *DownloadService) tryMultipartDownload(ctx context.Context, baseURL string, info *platform.DownloadInfo, destPath string, progress ProgressFunc) (int64, error) {
	written, err := s.multipartDownloader.Download(ctx, baseURL, info, destPath, progress)
	if err != nil {
		_ = os.Remove(destPath)
		return 0, fmt.Errorf("multipart download failed (will retry with single-thread): %w", err)
	}
	if info.Size > 0 && written != info.Size {
		_ = os.Remove(destPath)
		return 0, fmt.Errorf("incomplete multipart download: got %d bytes, expected %d", written, info.Size)
	}
	return written, nil
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

	throttledProgress := progress
	if progress != nil {
		lastUpdate := time.Time{}
		interval := 500 * time.Millisecond
		throttledProgress = func(written, total int64) {
			now := time.Now()
			if !lastUpdate.IsZero() && now.Sub(lastUpdate) < interval {
				if total <= 0 || written < total {
					return
				}
			}
			lastUpdate = now
			progress(written, total)
		}
	}

	written, err := util.CopyWithProgress(file, resp.Body, info.Size, throttledProgress)
	closeErr := file.Close()
	if err != nil {
		return written, err
	}
	if closeErr != nil {
		return written, closeErr
	}
	return written, nil
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

func minDuration(a, b time.Duration) time.Duration {
	if a == 0 || a > b {
		return b
	}
	return a
}
