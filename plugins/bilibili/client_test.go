package bilibili

import (
	"context"
	"os"
	"testing"
	"time"

	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
)

func getTestClient() *Client {
	logger, _ := logpkg.New("debug", "text", false)
	return New(logger, "", "", false, 0, false, "")
}

func TestClient_GetAudioSongInfo(t *testing.T) {
	// Skip in CI, only for local testing
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Bilibili API call in CI")
	}

	client := getTestClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Testing with a known auid, e.g. au3302094
	sid := 3302094

	resp, err := client.GetAudioSongInfo(ctx, sid)
	if err != nil {
		t.Fatalf("Failed to get audio song info: %v", err)
	}

	if resp == nil {
		t.Fatalf("Expected response but got nil")
	}

	if resp.ID != sid {
		t.Errorf("Expected sid %d, got %d", sid, resp.ID)
	}

	if resp.Title == "" {
		t.Errorf("Expected non-empty title")
	}

	t.Logf("Got Song Info: ID=%d, Title=%s, Author=%s, Duration=%ds",
		resp.ID, resp.Title, resp.Author, resp.Duration)
}

func TestClient_GetAudioStreamUrl(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Bilibili API call in CI")
	}

	client := getTestClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sid := 3302094
	quality := 0 // standard 128k

	resp, err := client.GetAudioStreamUrl(ctx, sid, quality)
	if err != nil {
		t.Fatalf("Failed to get stream url: %v", err)
	}

	if resp == nil {
		t.Fatalf("Expected response but got nil")
	}

	if len(resp.Cdns) == 0 {
		t.Errorf("Expected valid CDN list but got empty")
	} else {
		t.Logf("Got Stream URL: %s", resp.Cdns[0])
	}
}

func TestClient_GetVideoInfo(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Bilibili API call in CI")
	}

	client := getTestClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bvid := "BV1GJ411x7h7"

	resp, err := client.GetVideoInfo(ctx, bvid)
	if err != nil {
		t.Fatalf("Failed to get video info: %v", err)
	}

	if resp == nil {
		t.Fatalf("Expected response but got nil")
	}

	if resp.Bvid != bvid {
		t.Errorf("Expected bvid %s, got %s", bvid, resp.Bvid)
	}

	t.Logf("Got Video Info: BVID=%s, CID=%d, Title=%s", resp.Bvid, resp.Cid, resp.Title)
}

func TestClient_GetVideoPlayUrl(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Bilibili API call in CI")
	}

	client := getTestClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bvid := "BV1GJ411x7h7"
	cid := 137649199

	audioStreams, err := client.GetVideoPlayUrl(ctx, bvid, cid)
	if err != nil {
		t.Fatalf("Failed to get video play url: %v", err)
	}

	if len(audioStreams) == 0 {
		t.Fatalf("Expected audio stream but got none")
	}

	highestAudio := audioStreams[0]
	for _, audio := range audioStreams {
		if audio.Bandwidth > highestAudio.Bandwidth {
			highestAudio = audio
		}
	}

	if highestAudio.BaseURL == "" {
		t.Errorf("Expected non-empty stream URL")
	}

	t.Logf("Got Video Play URL: Bandwidth=%d, URL=%s... (total streams: %d)", highestAudio.Bandwidth, highestAudio.BaseURL[:50], len(audioStreams))
}

func TestClient_ResolveB23ID(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Bilibili API call in CI")
	}

	client := getTestClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	shortID := "ysjTEMn" // from user's provided link: https://b23.tv/ysjTEMn

	resolvedID, err := client.ResolveB23ID(ctx, shortID)
	if err != nil {
		t.Fatalf("Failed to resolve b23 link: %v", err)
	}

	if resolvedID == "" {
		t.Fatalf("Expected resolved ID but got empty string")
	}

	t.Logf("Successfully resolved b23.tv shortlink %s to track ID: %s", shortID, resolvedID)
}
