package spotify

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/plugins/spotify/native"
)

type spotifyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f spotifyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSearchUsesPathfinderWithoutClientCredentials(t *testing.T) {
	client := NewClient("", "", "US", time.Second, nil)
	client.WithWebAuthProvider(func(ctx context.Context) (native.WebAuth, error) {
		return native.WebAuth{Bearer: "bearer", ClientToken: "client-token"}, nil
	})
	client.httpClient = &http.Client{Transport: spotifyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != spotifyPathfinderURL {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		if got := req.Header.Get("Authorization"); got != "Bearer bearer" {
			t.Fatalf("authorization = %q", got)
		}
		body := `{
			"data": {
				"searchV2": {
					"tracksV2": {
						"items": [{
							"item": {
								"data": {
									"id": "track-id",
									"name": "Track",
									"uri": "spotify:track:track-id",
									"artists": {"items": [{"id": "artist-id", "profile": {"name": "Artist"}}]},
									"albumOfTrack": {
										"id": "album-id",
										"name": "Album",
										"coverArt": {"sources": [{"url": "small", "width": 64, "height": 64}, {"url": "large", "width": 640, "height": 640}]}
									}
								}
							}
						}]
					}
				}
			}
		}`
		return spotifyTestResponse(req, http.StatusOK, body), nil
	})}

	got, err := client.Search(context.Background(), "test", 3)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Search() len = %d, want 1", len(got))
	}
	if got[0].ID != "track-id" || got[0].Title != "Track" {
		t.Fatalf("track = %+v", got[0])
	}
	if len(got[0].Artists) != 1 || got[0].Artists[0].Name != "Artist" {
		t.Fatalf("artists = %+v", got[0].Artists)
	}
	if got[0].CoverURL != "large" {
		t.Fatalf("cover = %q, want large", got[0].CoverURL)
	}
}

func spotifyTestResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
