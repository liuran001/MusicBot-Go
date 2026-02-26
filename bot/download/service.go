package download

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/util"
)

type ProgressFunc = util.ProgressFunc

type DownloadService struct {
	client              *http.Client
	timeout             time.Duration
	proxy               string
	checkMD5            bool
	maxRetries          int
	multipartEnabled    bool
	multipartOpts       MultipartDownloadOptions
	multipartDownloader *MultipartDownloader
	inflightMu          sync.Mutex
	inflight            map[string]*inflightDownload
}

type inflightDownload struct {
	done      chan struct{}
	temp      string
	written   int64
	size      int64
	format    string
	sourceURL string
	err       error
	refs      int
	closed    bool
}

type DownloadServiceOptions struct {
	Timeout              time.Duration
	Proxy                string
	CheckMD5             bool
	MaxRetries           int
	EnableMultipart      bool
	MultipartConcurrency int
	MultipartMinSize     int64
}

const (
	defaultDownloadMaxRetries = 3
	maxDownloadRetryDelay     = 30 * time.Second
)

var (
	retryJitterMu  sync.Mutex
	retryJitterRng = rand.New(rand.NewSource(time.Now().UnixNano()))
)

func NewDownloadService(opts DownloadServiceOptions) *DownloadService {
	proxyAddr := strings.TrimSpace(opts.Proxy)
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
	if proxyAddr != "" {
		if proxyURL, err := normalizeProxyURL(proxyAddr); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	client := &http.Client{
		Transport: transport,
	}

	s := &DownloadService{
		client:           client,
		timeout:          opts.Timeout,
		proxy:            proxyAddr,
		checkMD5:         opts.CheckMD5,
		maxRetries:       opts.MaxRetries,
		multipartEnabled: opts.EnableMultipart,
		inflight:         make(map[string]*inflightDownload),
	}
	if s.maxRetries <= 0 {
		s.maxRetries = defaultDownloadMaxRetries
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

func (s *DownloadService) FillMetadata(info *platform.DownloadInfo, resp *http.Response) {
	if info == nil || resp == nil {
		return
	}

	contentType := resp.Header.Get("Content-Type")
	contentDisposition := resp.Header.Get("Content-Disposition")

	isAudioContent := strings.HasPrefix(contentType, "audio/")
	hasFilename := strings.Contains(contentDisposition, "filename")

	if (isAudioContent || hasFilename) && resp.ContentLength > 0 {
		info.Size = resp.ContentLength
	}

	if info.Format == "" && contentType != "" {
		if strings.Contains(contentType, "mpeg") || strings.Contains(contentType, "mp3") {
			info.Format = "mp3"
		} else if strings.Contains(contentType, "flac") {
			info.Format = "flac"
		} else if strings.Contains(contentType, "aac") || strings.Contains(contentType, "mp4") {
			info.Format = "m4a"
		}
	}
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

	key := strings.TrimSpace(rewriteNeteaseHost(info.URL))
	if key == "" {
		key = strings.TrimSpace(info.URL)
	}
	call, leader := s.acquireInflight(key)
	defer s.releaseInflight(key, call)

	if leader {
		tmpFile, err := os.CreateTemp("", "musicbot-download-*")
		if err != nil {
			call.err = err
			s.inflightMu.Lock()
			call.closed = true
			s.inflightMu.Unlock()
			close(call.done)
			return 0, err
		}
		_ = tmpFile.Close()
		call.temp = tmpFile.Name()

		infoCopy := *info
		written, err := s.downloadToPath(ctx, &infoCopy, call.temp, progress)
		call.written = written
		call.err = err
		call.size = infoCopy.Size
		call.format = infoCopy.Format
		call.sourceURL = strings.TrimSpace(infoCopy.URL)
		s.inflightMu.Lock()
		call.closed = true
		s.inflightMu.Unlock()
		close(call.done)
	} else {
		select {
		case <-call.done:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}

	if call.err != nil {
		return 0, call.err
	}
	if info != nil {
		if call.size > 0 {
			info.Size = call.size
		}
		if strings.TrimSpace(info.Format) == "" && strings.TrimSpace(call.format) != "" {
			info.Format = call.format
		}
		if strings.TrimSpace(call.sourceURL) != "" {
			info.URL = call.sourceURL
		}
	}

	copyProgress := progress
	if leader {
		copyProgress = nil
	}
	if call.temp == "" {
		return 0, errors.New("download temp file missing")
	}
	total := call.size
	if total <= 0 {
		total = call.written
	}

	return copyToPath(call.temp, destPath, total, copyProgress)
}

func (s *DownloadService) downloadToPath(ctx context.Context, info *platform.DownloadInfo, destPath string, progress ProgressFunc) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if info == nil || info.URL == "" {
		return 0, errors.New("download info missing")
	}
	if destPath == "" {
		return 0, errors.New("dest path missing")
	}

	urls := candidateDownloadURLs(info)
	var lastErr error
	for idx, raw := range urls {
		info.URL = raw
		baseURL := rewriteNeteaseHost(raw)

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
			lastErr = err
			_ = os.Remove(destPath)
		}

		for attempt := 0; attempt < s.maxRetries; attempt++ {
			written, err := s.downloadOnce(ctx, baseURL, info, destPath, progress)
			if err == nil {
				if info.Size > 0 && written < info.Size {
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
			if attempt < s.maxRetries-1 {
				wait := retryDelayWithJitter(attempt)
				select {
				case <-ctx.Done():
					return 0, ctx.Err()
				case <-time.After(wait):
				}
			}
		}

		if idx < len(urls)-1 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			default:
			}
		}
	}
	return 0, lastErr
}

func candidateDownloadURLs(info *platform.DownloadInfo) []string {
	if info == nil {
		return nil
	}
	seen := make(map[string]struct{}, 1+len(info.CandidateURLs))
	urls := make([]string, 0, 1+len(info.CandidateURLs))
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if _, ok := seen[raw]; ok {
			return
		}
		seen[raw] = struct{}{}
		urls = append(urls, raw)
	}
	add(info.URL)
	for _, item := range info.CandidateURLs {
		add(item)
	}
	return urls
}

func (s *DownloadService) acquireInflight(key string) (*inflightDownload, bool) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	if call, ok := s.inflight[key]; ok {
		call.refs++
		return call, false
	}
	call := &inflightDownload{done: make(chan struct{}), refs: 1}
	s.inflight[key] = call
	return call, true
}

func (s *DownloadService) releaseInflight(key string, call *inflightDownload) {
	if call == nil {
		return
	}
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	if call.refs > 0 {
		call.refs--
	}
	if call.refs == 0 && call.closed {
		delete(s.inflight, key)
		if strings.TrimSpace(call.temp) != "" {
			_ = os.Remove(call.temp)
		}
	}
}

func copyToPath(srcPath, destPath string, total int64, progress ProgressFunc) (int64, error) {
	if srcPath == "" {
		return 0, errors.New("source path missing")
	}
	if progress == nil {
		_ = os.Remove(destPath)
		if err := os.Link(srcPath, destPath); err == nil {
			if total > 0 {
				return total, nil
			}
			if stat, statErr := os.Stat(srcPath); statErr == nil {
				return stat.Size(), nil
			}
			return 0, nil
		}
	}
	in, err := os.Open(srcPath)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return 0, err
	}
	written, copyErr := util.CopyWithProgress(out, in, total, progress)
	closeErr := out.Close()
	if copyErr != nil {
		return written, copyErr
	}
	if closeErr != nil {
		return written, closeErr
	}
	return written, nil
}

func retryDelayWithJitter(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	base := time.Duration(1<<attempt) * time.Second
	if base > maxDownloadRetryDelay {
		base = maxDownloadRetryDelay
	}

	retryJitterMu.Lock()
	jitter := 0.75 + retryJitterRng.Float64()*0.5
	retryJitterMu.Unlock()

	wait := time.Duration(float64(base) * jitter)
	if wait <= 0 {
		return time.Second
	}
	return wait
}

func (s *DownloadService) tryMultipartDownload(ctx context.Context, baseURL string, info *platform.DownloadInfo, destPath string, progress ProgressFunc) (int64, error) {
	written, err := s.multipartDownloader.Download(ctx, baseURL, info, destPath, progress)
	if err != nil {
		_ = os.Remove(destPath)
		return 0, fmt.Errorf("multipart download failed (will retry with single-thread): %w", err)
	}
	if info.Size > 0 && written < info.Size {
		_ = os.Remove(destPath)
		return 0, fmt.Errorf("incomplete multipart download: got %d bytes, expected %d", written, info.Size)
	}
	return written, nil
}

func (s *DownloadService) downloadOnce(ctx context.Context, rawURL string, info *platform.DownloadInfo, destPath string, progress ProgressFunc) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	for k, v := range info.Headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	s.FillMetadata(info, resp)

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

	totalSize := info.Size
	if totalSize <= 0 && resp.ContentLength > 0 {
		totalSize = resp.ContentLength
	}
	written, err := util.CopyWithProgress(file, resp.Body, totalSize, throttledProgress)
	closeErr := file.Close()
	if err != nil {
		return written, err
	}
	if closeErr != nil {
		return written, closeErr
	}
	return written, nil
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

func normalizeProxyURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty proxy")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	proxyURL, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if proxyURL.Scheme == "" || proxyURL.Host == "" {
		return nil, errors.New("invalid proxy")
	}
	return proxyURL, nil
}

func minDuration(a, b time.Duration) time.Duration {
	if a == 0 || a > b {
		return b
	}
	return a
}
