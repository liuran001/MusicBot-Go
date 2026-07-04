package spotify

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/plugins/spotify/native"
)

func TestGetLyricsUsesWebAuth(t *testing.T) {
	client := NewClient("", "", "US", time.Second, nil)
	client.WithWebAuthProvider(func(ctx context.Context) (native.WebAuth, error) {
		return native.WebAuth{Bearer: "bearer", ClientToken: "client-token"}, nil
	})
	client.httpClient = &http.Client{Transport: spotifyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.String() != spotifyLyricsURL+"track-id" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		if got := req.Header.Get("Authorization"); got != "Bearer bearer" {
			t.Fatalf("authorization = %q", got)
		}
		if got := req.Header.Get("Client-Token"); got != "client-token" {
			t.Fatalf("client-token = %q", got)
		}
		return spotifyTestResponse(req, http.StatusOK, `{
			"lyrics": {
				"syncType": "LINE_SYNCED",
				"language": "en",
				"lines": [
					{"startTimeMs": "13510", "endTimeMs": "16120", "words": "First line"},
					{"startTimeMs": "16120", "endTimeMs": "19000", "words": "Second line"}
				]
			}
		}`), nil
	})}

	got, err := client.GetLyrics(context.Background(), "track-id")
	if err != nil {
		t.Fatalf("GetLyrics() error = %v", err)
	}
	if got.Plain != "First line\nSecond line" {
		t.Fatalf("plain = %q", got.Plain)
	}
	if len(got.Timestamped) != 2 {
		t.Fatalf("timestamped len = %d, want 2", len(got.Timestamped))
	}
	if got.Timestamped[0].Time != 13510*time.Millisecond || got.Timestamped[0].Text != "First line" {
		t.Fatalf("first timestamped line = %+v", got.Timestamped[0])
	}
}

func TestConvertSpotifyLyricsUnsynced(t *testing.T) {
	var response spotifyLyricsResponse
	response.Lyrics.SyncType = "UNSYNCED"
	response.Lyrics.Lines = []spotifyLyricsLine{
		{Words: "First line"},
		{Words: "Second line"},
	}

	got := convertSpotifyLyrics(response)
	if got == nil || got.Plain != "First line\nSecond line" {
		t.Fatalf("lyrics = %+v", got)
	}
	if len(got.Timestamped) != 0 {
		t.Fatalf("timestamped = %+v, want empty", got.Timestamped)
	}
}

func TestGetLyricsMapsUnavailableResponse(t *testing.T) {
	client := NewClient("", "", "US", time.Second, nil)
	client.WithWebAuthProvider(func(ctx context.Context) (native.WebAuth, error) {
		return native.WebAuth{Bearer: "bearer", ClientToken: "client-token"}, nil
	})
	client.httpClient = &http.Client{Transport: spotifyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return spotifyTestResponse(req, http.StatusForbidden, `{}`), nil
	})}

	_, err := client.GetLyrics(context.Background(), "track-id")
	if !errors.Is(err, platform.ErrUnavailable) {
		t.Fatalf("GetLyrics() error = %v, want ErrUnavailable", err)
	}
}

func TestSpotifyPlatformSupportsLyrics(t *testing.T) {
	spotifyPlatform := NewPlatform(NewClient("", "", "US", time.Second, nil))
	if !spotifyPlatform.SupportsLyrics() {
		t.Fatal("SupportsLyrics() = false, want true")
	}
	if !spotifyPlatform.Capabilities().Lyrics {
		t.Fatal("Capabilities().Lyrics = false, want true")
	}
}
