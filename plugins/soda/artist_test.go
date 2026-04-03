package soda

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClientGetArtist(t *testing.T) {
	client := &Client{
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "https://music.douyin.com/qishui/share/artist?artist_id=423456789" {
					t.Fatalf("unexpected request url: %s", req.URL.String())
				}
				body := `<html><script>window._ROUTER_DATA = {"loaderData":{"artist_page":{"artistInfo":{"id":"423456789","name":"Artist A","track_count":7,"avatar":{"urls":["https://p3.qishui.com/img/"],"uri":"avatar123"}},"trackList":[{"id":"1"},{"id":"2"}]}}};</script></html>`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		},
	}

	artist, trackCount, err := client.GetArtist(context.Background(), "423456789")
	if err != nil {
		t.Fatalf("GetArtist() error = %v", err)
	}
	if artist == nil {
		t.Fatalf("GetArtist() returned nil artist")
	}
	if artist.ID != "423456789" || artist.Name != "Artist A" {
		t.Fatalf("GetArtist() artist = %+v", artist)
	}
	if artist.URL != "https://music.douyin.com/qishui/share/artist?artist_id=423456789" {
		t.Fatalf("GetArtist() url = %q", artist.URL)
	}
	if artist.AvatarURL == "" {
		t.Fatalf("GetArtist() avatar missing")
	}
	if trackCount != 7 {
		t.Fatalf("GetArtist() trackCount = %d", trackCount)
	}
}

func TestSodaPlatformGetArtist(t *testing.T) {
	client := &Client{
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := `<html><script>window._ROUTER_DATA = {"loaderData":{"artist_page":{"artistInfo":{"id":"423456789","name":"Artist A"}}}};</script></html>`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		},
	}

	plat := NewPlatform(client)
	artist, err := plat.GetArtist(context.Background(), "423456789")
	if err != nil {
		t.Fatalf("platform GetArtist() error = %v", err)
	}
	if artist == nil || artist.Name != "Artist A" {
		t.Fatalf("platform GetArtist() = %+v", artist)
	}
}

func TestSodaPlatformGetArtistUnavailable(t *testing.T) {
	plat := NewPlatform(nil)
	_, err := plat.GetArtist(context.Background(), "423456789")
	if err == nil || err.Error() == "" {
		t.Fatalf("expected error")
	}
	if err != nil && !strings.Contains(err.Error(), platform.ErrUnavailable.Error()) {
		t.Fatalf("expected unavailable error, got %v", err)
	}
}
