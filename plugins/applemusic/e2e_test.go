package applemusic

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Throwaway end-to-end test. Token comes ONLY from the environment, never a file.
// Run with: APPLE_MUSIC_TEST_TOKEN=... go test -run TestE2EDecrypt -v
func TestE2EDecrypt(t *testing.T) {
	token := os.Getenv("APPLE_MUSIC_TEST_TOKEN")
	if token == "" {
		t.Skip("no APPLE_MUSIC_TEST_TOKEN set")
	}

	client := NewClient(token, "us", "en-US", 60*time.Second, nil)
	if dev := loadWVDevice("", "", nil); dev != nil {
		client.wvDevice = dev
	} else {
		t.Fatal("built-in widevine device failed to load")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 1. Search for a real track.
	tracks, err := client.Search(ctx, "Bad Guy Billie Eilish", 5)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(tracks) == 0 {
		t.Fatal("search returned no tracks")
	}
	track := tracks[0]
	t.Logf("found track: id=%s %q, storefront now=%s", track.ID, track.Title, client.storefront)

	// 2. Get download info — should pick native Widevine decryption (priority 1).
	info, err := client.GetDownloadInfo(ctx, track.ID, 0)
	if err != nil {
		t.Fatalf("GetDownloadInfo failed: %v", err)
	}
	t.Logf("download info: url=%s format=%s bitrate=%d quality=%v hasDownloader=%v",
		info.URL, info.Format, info.Bitrate, info.Quality, info.Downloader != nil)

	if info.Downloader == nil {
		t.Fatalf("no Downloader — fell back to a plain URL (likely preview), not native decrypt. url=%s", info.URL)
	}

	// 3. Actually run the decrypt pipeline and write to disk.
	dest := filepath.Join(t.TempDir(), "decrypted.m4a")
	n, err := info.Downloader(ctx, info, dest, func(written, total int64) {})
	if err != nil {
		t.Fatalf("decrypt+download failed: %v", err)
	}
	t.Logf("wrote %d bytes to %s", n, dest)

	// 4. Verify the output is a real, non-trivial decrypted MP4/M4A.
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(data) < 100_000 {
		t.Fatalf("output suspiciously small: %d bytes (decrypt may have produced garbage)", len(data))
	}
	if !containsBox(data[:4096], "ftyp") {
		t.Fatalf("no ftyp box found in first 4KB — not a valid MP4 (decrypt likely failed)")
	}
	if !containsBox(data, "mdat") {
		t.Fatalf("no mdat box — no media payload")
	}
	t.Logf("SUCCESS: decrypted %d bytes, valid MP4 (ftyp+mdat present)", len(data))
}

// Diagnostic: dump the real Apple Music Widevine HLS manifest so we can fix the parser.
func TestE2EDumpManifest(t *testing.T) {
	token := os.Getenv("APPLE_MUSIC_TEST_TOKEN")
	if token == "" {
		t.Skip("no APPLE_MUSIC_TEST_TOKEN set")
	}
	client := NewClient(token, "us", "en-US", 60*time.Second, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tracks, err := client.Search(ctx, "Bad Guy Billie Eilish", 5)
	if err != nil || len(tracks) == 0 {
		t.Fatalf("search failed: %v", err)
	}
	trackID := tracks[0].ID

	assets, err := client.callWebPlayback(ctx, trackID)
	if err != nil {
		t.Fatalf("webplayback: %v", err)
	}
	var hlsURL string
	for i := range assets {
		t.Logf("asset flavor=%q url=%s", assets[i].Flavor, assets[i].URL)
		if assets[i].Flavor == "28:ctrp256" {
			hlsURL = assets[i].URL
		}
	}
	if hlsURL == "" && len(assets) > 0 {
		hlsURL = assets[0].URL
	}

	body, err := client.downloadURL(ctx, hlsURL)
	if err != nil {
		t.Fatalf("fetch m3u8: %v", err)
	}
	t.Logf("===== RAW M3U8 (%d bytes) =====\n%s\n===== END =====", len(body), string(body))
}

func containsBox(data []byte, box string) bool {
	b := []byte(box)
	for i := 0; i+len(b) <= len(data); i++ {
		match := true
		for j := range b {
			if data[i+j] != b[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
