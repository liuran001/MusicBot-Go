package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
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

var errRangeNotSupported = errors.New("range request not supported by server")

// bufPool reuses 32KB buffers for download I/O to reduce allocations.
var bufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32*1024)
		return &buf
	},
}

// partDownload represents a single chunk download task
type partDownload struct {
	index   int
	start   int64
	end     int64
	err     error
	written int64
}

// progressTracker aggregates progress from multiple parts
type progressTracker struct {
	mu           sync.Mutex
	parts        map[int]int64
	total        int64
	totalWritten int64
	callback     ProgressFunc
	lastCall     time.Time
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

	prev := pt.parts[partIndex]
	pt.parts[partIndex] = written
	pt.totalWritten += written - prev

	now := time.Now()
	if now.Sub(pt.lastCall) < 500*time.Millisecond {
		return
	}
	pt.lastCall = now

	pt.callback(pt.totalWritten, pt.total)
}

func (pt *progressTracker) final() {
	if pt.callback == nil {
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.callback(pt.totalWritten, pt.total)
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
	supportsRange := strings.EqualFold(acceptRanges, "bytes") && contentLength > 0

	return supportsRange, contentLength, nil
}

func (md *MultipartDownloader) Download(ctx context.Context, rawURL string, info *platform.DownloadInfo, destPath string, progress ProgressFunc) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Sources that demand bounded Range chunks (e.g. googlevideo, which 403s on
	// HEAD, plain GET, open-ended ranges, and any single Range larger than a
	// per-IP cap) advertise MaxChunkSize. For these we MUST skip the HEAD probe
	// and never fall back to an unbounded single GET — both 403. Always fetch in
	// bounded Range chunks no larger than the advertised cap.
	if info != nil && info.MaxChunkSize > 0 {
		totalSize := info.Size
		if totalSize <= 0 {
			// Without a known size we can't compute bounded ranges. Probing the
			// upper bound would mean an open-ended/unbounded request, which these
			// servers reject — so there is nothing safe to fall back to.
			return 0, fmt.Errorf("chunked download requires known size, got %d", totalSize)
		}
		return md.downloadChunked(ctx, rawURL, info, destPath, totalSize, info.MaxChunkSize, progress)
	}

	supportsRange, contentLength, err := md.SupportsRange(ctx, rawURL, info)
	if err != nil {
		return md.downloadSingle(ctx, rawURL, info, destPath, info.Size, progress)
	}
	// The declared size is an exact contract unless the platform marked it
	// advisory, in which case only a shorter body is a truncated transfer and the
	// served length wins from here on.
	if contentLength > 0 && violatesDeclaredSize(info, contentLength) {
		return 0, declaredSizeError(info, contentLength, "download content length")
	}

	totalSize := contentLength
	if totalSize <= 0 && info.Size > 0 {
		totalSize = info.Size
	}

	if !supportsRange {
		return md.downloadSingle(ctx, rawURL, info, destPath, totalSize, progress)
	}
	if totalSize <= 0 {
		return md.downloadSingle(ctx, rawURL, info, destPath, totalSize, progress)
	}
	if totalSize < md.minSize {
		return md.downloadSingle(ctx, rawURL, info, destPath, totalSize, progress)
	}
	written, err := md.downloadMultipart(ctx, rawURL, info, destPath, totalSize, progress)
	if err != nil && errors.Is(err, errRangeNotSupported) {
		return md.downloadSingle(ctx, rawURL, info, destPath, totalSize, progress)
	}
	return written, err
}

func (md *MultipartDownloader) downloadSingle(ctx context.Context, rawURL string, info *platform.DownloadInfo, destPath string, expectedTotal int64, progress ProgressFunc) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	for k, v := range info.Headers {
		if strings.EqualFold(k, "Range") {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := md.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// This request deliberately omits Range. A 206 response therefore cannot
	// represent a complete download; accepting it (especially with unknown
	// length/chunked transfer) would cache a truncated file as successful.
	if resp.StatusCode == http.StatusPartialContent {
		return 0, fmt.Errorf("%w: unexpected partial content for un-ranged request", errDownloadIntegrity)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Fall back to the served length when the platform declared no size. Some
	// sources cannot supply one -- bilibili reports Size 0 and its CDN answers
	// HEAD with 404, so SupportsRange yields nothing either -- which left the
	// transfer with no length contract at all: a body that ended early was
	// accepted as a complete download.
	if expectedTotal <= 0 && resp.ContentLength > 0 {
		expectedTotal = resp.ContentLength
	}

	file, err := os.Create(destPath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	bufp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufp)
	buf := *bufp
	var written int64
	for {
		nr, readErr := resp.Body.Read(buf)
		if nr > 0 {
			nw, writeErr := file.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
				if progress != nil {
					progress(written, expectedTotal)
				}
			}
			if writeErr != nil {
				return written, writeErr
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return written, wrapDownloadIntegrityReadError(readErr, written, expectedTotal, "download body", info.SizeIsAdvisory)
		}
	}

	if expectedTotal <= 0 {
		expectedTotal = written
	}
	if progress != nil {
		progress(written, expectedTotal)
	}

	return written, nil
}

// downloadMultipart performs concurrent chunk downloads
// downloadChunked fetches the whole file in bounded Range chunks of at most
// maxChunk bytes, for sources (e.g. googlevideo) that reject HEAD, plain GET,
// open-ended ranges, and any single Range larger than a per-IP cap. There is no
// fallback path: every request is a bounded Range within the cap.
func (md *MultipartDownloader) downloadChunked(ctx context.Context, rawURL string, info *platform.DownloadInfo, destPath string, totalSize, maxChunk int64, progress ProgressFunc) (int64, error) {
	if maxChunk <= 0 {
		maxChunk = 1024 * 1024
	}
	partSize := totalSize / int64(md.concurrency)
	if partSize > maxChunk || partSize <= 0 {
		partSize = maxChunk
	}
	return md.downloadMultipartWithPartSize(ctx, rawURL, info, destPath, totalSize, partSize, progress)
}

func (md *MultipartDownloader) downloadMultipart(ctx context.Context, rawURL string, info *platform.DownloadInfo, destPath string, totalSize int64, progress ProgressFunc) (int64, error) {
	// Calculate part size
	partSize := md.partSize
	if partSize <= 0 {
		partSize = totalSize / int64(md.concurrency)
		if partSize < 1024*1024 {
			partSize = 1024 * 1024 // Minimum 1MB per part
		}
	}
	return md.downloadMultipartWithPartSize(ctx, rawURL, info, destPath, totalSize, partSize, progress)
}

func (md *MultipartDownloader) downloadMultipartWithPartSize(ctx context.Context, rawURL string, info *platform.DownloadInfo, destPath string, totalSize, partSize int64, progress ProgressFunc) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if partSize <= 0 {
		partSize = 1024 * 1024
	}

	// Calculate number of parts
	numParts := int(totalSize / partSize)
	if totalSize%partSize != 0 {
		numParts++
	}

	// Preallocate the destination and let every worker pwrite into its own
	// slice of it. Staging each part as its own file meant writing the whole
	// track twice -- once as parts, once again while merging -- and holding two
	// full copies on disk at the peak, which is a hard limit on a small VPS.
	outFile, err := os.Create(destPath)
	if err != nil {
		return 0, fmt.Errorf("failed to create destination: %w", err)
	}
	if err := outFile.Truncate(totalSize); err != nil {
		outFile.Close()
		os.Remove(destPath)
		return 0, fmt.Errorf("failed to preallocate destination: %w", err)
	}
	// A failed multipart download must not leave a sparse carcass behind: the
	// caller either retries or falls back to a single unranged GET, and both
	// expect to start from nothing.
	committed := false
	defer func() {
		if !committed {
			outFile.Close()
			os.Remove(destPath)
		}
	}()

	// Setup progress tracking
	tracker := newProgressTracker(totalSize, progress)

	// Download parts concurrently
	parts := make([]*partDownload, numParts)
	partCh := make(chan int, numParts)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

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
				err := md.downloadPart(ctx, rawURL, info, part, outFile, tracker)
				if err != nil {
					part.err = err
					errOnce.Do(func() {
						firstErr = fmt.Errorf("part %d failed: %w", partIndex, err)
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
		}
		partCh <- i
	}
	close(partCh)

	wg.Wait()

	if err := preferredMultipartError(parts, firstErr); err != nil {
		return 0, err
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	tracker.final()

	var written int64
	for _, part := range parts {
		written += part.written
	}
	if written != totalSize {
		return 0, fmt.Errorf("%w: assembled size mismatch: got %d, expected %d", errDownloadIntegrity, written, totalSize)
	}
	if err := outFile.Close(); err != nil {
		return 0, fmt.Errorf("failed to close destination: %w", err)
	}
	committed = true

	return written, nil
}

func preferredMultipartError(parts []*partDownload, firstErr error) error {
	for _, part := range parts {
		if part != nil && errors.Is(part.err, errDownloadIntegrity) {
			return fmt.Errorf("part %d failed: %w", part.index, part.err)
		}
	}
	return firstErr
}

// downloadPart downloads a single part of the file
func (md *MultipartDownloader) downloadPart(ctx context.Context, rawURL string, info *platform.DownloadInfo, part *partDownload, out *os.File, tracker *progressTracker) (retErr error) {
	ctx = withDownloadOwnedHeader(ctx, "Range")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}

	// Set Range header
	rangeHeader := fmt.Sprintf("bytes=%d-%d", part.start, part.end)
	req.Header.Set("Range", rangeHeader)

	// Copy other headers
	for k, v := range info.Headers {
		if !strings.EqualFold(k, "Range") {
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
	if resp.StatusCode == http.StatusOK {
		return errRangeNotSupported
	}
	contentRange := resp.Header.Get("Content-Range")
	expectedContentRange := fmt.Sprintf("bytes %d-%d/%d", part.start, part.end, tracker.total)
	if contentRange != expectedContentRange {
		return fmt.Errorf("%w: range content mismatch: got %q, expected %q", errDownloadIntegrity, contentRange, expectedContentRange)
	}
	expectedSize := part.end - part.start + 1
	if resp.ContentLength >= 0 && resp.ContentLength != expectedSize {
		return fmt.Errorf("%w: range body size mismatch: got %d bytes, expected %d", errDownloadIntegrity, resp.ContentLength, expectedSize)
	}

	// Download part with progress tracking
	bufp := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufp)
	buf := *bufp
	var written int64

	for {
		if written >= expectedSize {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		remaining := expectedSize - written
		readBuf := buf
		if remaining < int64(len(buf)) {
			readBuf = readBuf[:remaining]
		}
		nr, err := resp.Body.Read(readBuf)
		if nr > 0 {
			// WriteAt issues a pwrite, which carries its own offset and does
			// not touch the shared file descriptor's position -- so concurrent
			// parts can write into one file without locking.
			nw, ew := out.WriteAt(buf[0:nr], part.start+written)
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
			// Range boundaries are computed by us, so a part is always an exact
			// contract regardless of how the platform declares the total size.
			return wrapDownloadIntegrityReadError(err, written, expectedSize, "range body", false)
		}
	}
	extra, err := io.ReadAll(io.LimitReader(resp.Body, 1))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("%w: failed to verify range body boundary: %w", errDownloadIntegrity, err)
	}
	if len(extra) != 0 {
		return fmt.Errorf("%w: range body exceeds expected %d bytes", errDownloadIntegrity, expectedSize)
	}

	// Verify part size
	if written != expectedSize {
		return fmt.Errorf("%w: part size mismatch: got %d, expected %d", errDownloadIntegrity, written, expectedSize)
	}

	part.written = written
	return nil
}
