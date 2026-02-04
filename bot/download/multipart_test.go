package download

import (
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

	_, err := downloader.Download(ctx, server.URL, info, tempFile, nil)
	if err == nil {
		t.Fatal("Expected error for no Range support, got nil")
	}

	if !contains(err.Error(), "does not support Range") {
		t.Errorf("Expected 'does not support Range' error, got: %v", err)
	}

	t.Log("No Range support correctly detected!")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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

	_, err := downloader.Download(ctx, server.URL, info, tempFile, nil)
	if err == nil {
		t.Fatal("Expected error for small file, got nil")
	}

	if !contains(err.Error(), "file too small") {
		t.Errorf("Expected 'file too small' error, got: %v", err)
	}

	t.Log("Small file correctly skipped for multipart!")
}
