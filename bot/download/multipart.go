package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// MultipartDownloadOptions configures multipart download behavior
type MultipartDownloadOptions struct {
	// Number of concurrent parts (default: 4)
	Concurrency int
	// Minimum file size for multipart download in bytes (default: 5MB)
	MinSize int64
	// Size of each part in bytes (default: auto-calculated)
	PartSize int64
}

// MultipartDownloader handles concurrent chunk downloads
type MultipartDownloader struct {
	client      *http.Client
	timeout     time.Duration
	concurrency int
	minSize     int64
	partSize    int64
}

// partDownload represents a single chunk download task
type partDownload struct {
	index   int
	start   int64
	end     int64
	path    string
	err     error
	written int64
}

// progressTracker aggregates progress from multiple parts
type progressTracker struct {
	mu       sync.Mutex
	parts    map[int]int64
	total    int64
	callback ProgressFunc
	lastCall time.Time
}

func newProgressTracker(total int64, callback ProgressFunc) *progressTracker {
	return &progressTracker{
		parts:    make(map[int]int64),
		total:    total,
		callback: callback,
		lastCall: time.Now(),
	}
}

func (pt *progressTracker) update(partIndex int, written int64) {
	if pt.callback == nil {
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.parts[partIndex] = written

	now := time.Now()
	if now.Sub(pt.lastCall) < 500*time.Millisecond {
		return
	}
	pt.lastCall = now

	var totalWritten int64
	for _, w := range pt.parts {
		totalWritten += w
	}

	pt.callback(totalWritten, pt.total)
}

func (pt *progressTracker) final() {
	if pt.callback == nil {
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()

	var totalWritten int64
	for _, w := range pt.parts {
		totalWritten += w
	}

	pt.callback(totalWritten, pt.total)
}

func NewMultipartDownloader(client *http.Client, timeout time.Duration, opts MultipartDownloadOptions) *MultipartDownloader {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.MinSize <= 0 {
		opts.MinSize = 5 * 1024 * 1024 // 5MB
	}

	return &MultipartDownloader{
		client:      client,
		timeout:     timeout,
		concurrency: opts.Concurrency,
		minSize:     opts.MinSize,
		partSize:    opts.PartSize,
	}
}

// SupportsRange checks if the server supports Range requests
func (md *MultipartDownloader) SupportsRange(ctx context.Context, rawURL string, info *platform.DownloadInfo) (bool, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return false, 0, err
	}

	for k, v := range info.Headers {
		req.Header.Set(k, v)
	}

	resp, err := md.client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, 0, fmt.Errorf("HEAD request failed with status %d", resp.StatusCode)
	}

	acceptRanges := resp.Header.Get("Accept-Ranges")
	contentLength := resp.ContentLength

	// Server must explicitly support ranges and provide content length
	supportsRange := acceptRanges == "bytes" && contentLength > 0

	return supportsRange, contentLength, nil
}

func (md *MultipartDownloader) Download(ctx context.Context, rawURL string, info *platform.DownloadInfo, destPath string, progress ProgressFunc) (int64, error) {
	supportsRange, contentLength, err := md.SupportsRange(ctx, rawURL, info)
	if err != nil {
		return 0, fmt.Errorf("range check failed: %w", err)
	}

	totalSize := contentLength
	if totalSize <= 0 && info.Size > 0 {
		totalSize = info.Size
	}

	if !supportsRange {
		return 0, fmt.Errorf("server does not support Range requests, falling back to single-thread")
	}
	if totalSize <= 0 {
		return 0, fmt.Errorf("unknown file size, falling back to single-thread")
	}
	if totalSize < md.minSize {
		return 0, fmt.Errorf("file too small (%d bytes < %d bytes), using single-thread", totalSize, md.minSize)
	}

	return md.downloadMultipart(ctx, rawURL, info, destPath, totalSize, progress)
}

// downloadMultipart performs concurrent chunk downloads
func (md *MultipartDownloader) downloadMultipart(ctx context.Context, rawURL string, info *platform.DownloadInfo, destPath string, totalSize int64, progress ProgressFunc) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Calculate part size
	partSize := md.partSize
	if partSize <= 0 {
		partSize = totalSize / int64(md.concurrency)
		if partSize < 1024*1024 {
			partSize = 1024 * 1024 // Minimum 1MB per part
		}
	}

	// Calculate number of parts
	numParts := int(totalSize / partSize)
	if totalSize%partSize != 0 {
		numParts++
	}

	// Create temporary directory for parts
	tempDir := destPath + ".parts"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return 0, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Setup progress tracking
	tracker := newProgressTracker(totalSize, progress)

	// Download parts concurrently
	parts := make([]*partDownload, numParts)
	partCh := make(chan int, numParts)
	errCh := make(chan error, numParts)
	var wg sync.WaitGroup
	var errOnce sync.Once

	// Launch worker goroutines
	for i := 0; i < md.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for partIndex := range partCh {
				if ctx.Err() != nil {
					return
				}
				part := parts[partIndex]
				err := md.downloadPart(ctx, rawURL, info, part, tracker)
				if err != nil {
					part.err = err
					errOnce.Do(func() {
						errCh <- fmt.Errorf("part %d failed: %w", partIndex, err)
						cancel()
					})
					return
				}
			}
		}()
	}

	// Initialize parts and queue them
	for i := 0; i < numParts; i++ {
		start := int64(i) * partSize
		end := start + partSize - 1
		if i == numParts-1 {
			end = totalSize - 1
		}

		parts[i] = &partDownload{
			index: i,
			start: start,
			end:   end,
			path:  fmt.Sprintf("%s/part.%d", tempDir, i),
		}
		partCh <- i
	}
	close(partCh)

	wg.Wait()
	close(errCh)

	if len(errCh) > 0 {
		return 0, <-errCh
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	tracker.final()

	written, err := md.mergeParts(parts, destPath)
	if err != nil {
		return 0, fmt.Errorf("failed to merge parts: %w", err)
	}

	return written, nil
}

// downloadPart downloads a single part of the file
func (md *MultipartDownloader) downloadPart(ctx context.Context, rawURL string, info *platform.DownloadInfo, part *partDownload, tracker *progressTracker) (retErr error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}

	// Set Range header
	rangeHeader := fmt.Sprintf("bytes=%d-%d", part.start, part.end)
	req.Header.Set("Range", rangeHeader)

	// Copy other headers
	for k, v := range info.Headers {
		if k != "Range" {
			req.Header.Set(k, v)
		}
	}

	resp, err := md.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Accept both 200 (full content) and 206 (partial content)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("unexpected status %d for range request", resp.StatusCode)
	}

	// Create part file
	file, err := os.Create(part.path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && retErr == nil {
			retErr = closeErr
		}
	}()

	// Download part with progress tracking
	buf := make([]byte, 32*1024)
	var written int64
	expectedSize := part.end - part.start + 1

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		nr, err := resp.Body.Read(buf)
		if nr > 0 {
			nw, ew := file.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
				tracker.update(part.index, written)
			}
			if ew != nil {
				return ew
			}
			if nr != nw {
				return io.ErrShortWrite
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	// Verify part size
	if written != expectedSize {
		return fmt.Errorf("part size mismatch: got %d, expected %d", written, expectedSize)
	}

	part.written = written
	return nil
}

// mergeParts combines all downloaded parts into the final file
func (md *MultipartDownloader) mergeParts(parts []*partDownload, destPath string) (totalWritten int64, retErr error) {
	outFile, err := os.Create(destPath)
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := outFile.Close(); closeErr != nil && retErr == nil {
			retErr = closeErr
		}
	}()

	for _, part := range parts {
		if part.err != nil {
			return totalWritten, part.err
		}

		partFile, err := os.Open(part.path)
		if err != nil {
			return totalWritten, err
		}

		written, err := io.Copy(outFile, partFile)
		closeErr := partFile.Close()
		if err != nil {
			return totalWritten, err
		}
		if closeErr != nil {
			return totalWritten, closeErr
		}

		totalWritten += written
	}

	return totalWritten, nil
}
