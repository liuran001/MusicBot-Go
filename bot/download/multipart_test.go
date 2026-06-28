package download

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestMultipartDownload_RangeSupport(t *testing.T) {
	testData := make([]byte, 10*1024*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
			w.WriteHeader(http.StatusOK)
			return
		}

		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
			w.WriteHeader(http.StatusOK)
			w.Write(testData)
			return
		}

		var start, end int
		fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)

		if start < 0 || end >= len(testData) || start > end {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(testData)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(testData[start : end+1])
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	downloader := NewMultipartDownloader(client, 30*time.Second, MultipartDownloadOptions{
		Concurrency: 4,
		MinSize:     1 * 1024 * 1024,
	})

	ctx := context.Background()
	info := &platform.DownloadInfo{
		URL:  server.URL,
		Size: int64(len(testData)),
	}

	tempFile := "test_multipart_download.bin"
	defer os.Remove(tempFile)

	progressCalled := false
	progress := func(written, total int64) {
		progressCalled = true
		t.Logf("Progress: %d/%d (%.2f%%)", written, total, float64(written)*100/float64(total))
	}

	written, err := downloader.Download(ctx, server.URL, info, tempFile, progress)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if written != int64(len(testData)) {
		t.Errorf("Expected %d bytes, got %d", len(testData), written)
	}

	if !progressCalled {
		t.Error("Progress callback was never called")
	}

	downloaded, err := os.ReadFile(tempFile)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if len(downloaded) != len(testData) {
		t.Errorf("Downloaded file size mismatch: expected %d, got %d", len(testData), len(downloaded))
	}

	for i := range testData {
		if downloaded[i] != testData[i] {
			t.Errorf("Data mismatch at byte %d: expected %d, got %d", i, testData[i], downloaded[i])
			break
		}
	}

	t.Log("Multipart download test passed!")
}

func TestMultipartDownload_NoRangeSupport(t *testing.T) {
	testData := []byte("Hello, World!")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
			w.WriteHeader(http.StatusOK)
			return
		}

		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
		w.WriteHeader(http.StatusOK)
		w.Write(testData)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	downloader := NewMultipartDownloader(client, 30*time.Second, MultipartDownloadOptions{
		Concurrency: 4,
		MinSize:     1,
	})

	ctx := context.Background()
	info := &platform.DownloadInfo{
		URL:  server.URL,
		Size: int64(len(testData)),
	}

	tempFile := "test_single_download.bin"
	defer os.Remove(tempFile)

	written, err := downloader.Download(ctx, server.URL, info, tempFile, nil)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if written != int64(len(testData)) {
		t.Fatalf("expected written=%d, got %d", len(testData), written)
	}
	downloaded, err := os.ReadFile(tempFile)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(downloaded, testData) {
		t.Fatal("downloaded data mismatch")
	}
}

func TestMultipartDownload_SmallFile(t *testing.T) {
	testData := []byte("Small file")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	downloader := NewMultipartDownloader(client, 30*time.Second, MultipartDownloadOptions{
		Concurrency: 4,
		MinSize:     1024,
	})

	ctx := context.Background()
	info := &platform.DownloadInfo{
		URL:  server.URL,
		Size: int64(len(testData)),
	}

	tempFile := "test_small_file.bin"
	defer os.Remove(tempFile)

	written, err := downloader.Download(ctx, server.URL, info, tempFile, nil)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if written != int64(len(testData)) {
		t.Fatalf("expected written=%d, got %d", len(testData), written)
	}
	downloaded, err := os.ReadFile(tempFile)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(downloaded, testData) {
		t.Fatal("downloaded data mismatch")
	}
}

func TestMultipartDownload_RangeProbeBut200ResponseFallback(t *testing.T) {
	testData := make([]byte, 2*1024*1024)
	for i := range testData {
		testData[i] = byte((i * 7) % 251)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testData)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	downloader := NewMultipartDownloader(client, 30*time.Second, MultipartDownloadOptions{
		Concurrency: 4,
		MinSize:     1,
	})

	tempFile := "test_range_200_fallback.bin"
	defer os.Remove(tempFile)

	written, err := downloader.Download(context.Background(), server.URL, &platform.DownloadInfo{URL: server.URL, Size: int64(len(testData))}, tempFile, nil)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if written != int64(len(testData)) {
		t.Fatalf("expected written=%d, got %d", len(testData), written)
	}
	downloaded, err := os.ReadFile(tempFile)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(downloaded, testData) {
		t.Fatal("downloaded data mismatch")
	}
}

// TestChunkedDownload_GoogleVideoStyle simulates googlevideo's hostile behavior
// for non-PoToken stream URLs as observed live: HEAD, plain GET, open-ended
// ("bytes=0-") ranges, and any bounded Range larger than a per-IP cap all return
// 403. Only bounded Range chunks within the cap return 206. A source advertising
// MaxChunkSize must download successfully under these rules, in chunks no larger
// than the cap, without ever issuing a HEAD or an unbounded GET.
func TestChunkedDownload_GoogleVideoStyle(t *testing.T) {
	const cap = 1024 * 1024
	testData := make([]byte, 4*1024*1024+777) // not a multiple of cap
	for i := range testData {
		testData[i] = byte((i*13 + 7) % 251)
	}

	var sawHead, sawUnbounded, sawOversized bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			sawHead = true
			w.WriteHeader(http.StatusForbidden)
			return
		}
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			sawUnbounded = true
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var start, end int
		if n, _ := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); n != 2 {
			// open-ended "bytes=0-" — rejected
			sawUnbounded = true
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if start < 0 || start > end || start >= len(testData) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if end >= len(testData) {
			end = len(testData) - 1
		}
		if end-start+1 > cap {
			sawOversized = true
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(testData)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(testData[start : end+1])
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	// Multipart "disabled" config (MinSize huge) must NOT matter: MaxChunkSize
	// forces the chunked path regardless.
	downloader := NewMultipartDownloader(client, 30*time.Second, MultipartDownloadOptions{
		Concurrency: 4,
		MinSize:     100 * 1024 * 1024,
	})

	info := &platform.DownloadInfo{
		URL:          server.URL,
		Size:         int64(len(testData)),
		MaxChunkSize: cap,
	}
	tempFile := "test_chunked_googlevideo.bin"
	defer os.Remove(tempFile)

	written, err := downloader.Download(context.Background(), server.URL, info, tempFile, nil)
	if err != nil {
		t.Fatalf("chunked download failed: %v", err)
	}
	if written != int64(len(testData)) {
		t.Fatalf("expected %d bytes, got %d", len(testData), written)
	}
	if sawHead {
		t.Error("downloader issued a HEAD request (googlevideo would 403 it)")
	}
	if sawUnbounded {
		t.Error("downloader issued an unbounded/open-ended GET (would 403)")
	}
	if sawOversized {
		t.Error("downloader issued an oversized Range chunk (would 403)")
	}
	downloaded, err := os.ReadFile(tempFile)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if !bytes.Equal(downloaded, testData) {
		t.Fatal("downloaded data mismatch")
	}
}

// TestChunkedDownload_MissingSizeFails verifies a chunked source with no known
// size errors out rather than silently falling back to an unbounded GET.
func TestChunkedDownload_MissingSizeFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	downloader := NewMultipartDownloader(&http.Client{Timeout: 5 * time.Second}, 5*time.Second, MultipartDownloadOptions{Concurrency: 2})
	info := &platform.DownloadInfo{URL: server.URL, Size: 0, MaxChunkSize: 1024 * 1024}
	tempFile := "test_chunked_nosize.bin"
	defer os.Remove(tempFile)

	if _, err := downloader.Download(context.Background(), server.URL, info, tempFile, nil); err == nil {
		t.Fatal("expected error for chunked download with unknown size, got nil")
	}
}
