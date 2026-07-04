package spotify

import (
	"context"
	"encoding/json"
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
	client := newPathfinderTestClient(t, findTracksOperation, `{
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
	}`)

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

func TestGetTrackUsesPathfinderWithoutClientCredentials(t *testing.T) {
	client := newPathfinderTestClient(t, getTrackOperation, `{
		"data": {
			"trackUnion": {
				"id": "track-id",
				"name": "Track",
				"uri": "spotify:track:track-id",
				"duration": {"totalMilliseconds": 123000},
				"firstArtist": {"items": [{"id": "artist-id", "profile": {"name": "Artist"}}]},
				"albumOfTrack": {
					"id": "album-id",
					"name": "Album",
					"date": {"year": 2024},
					"coverArt": {"sources": [{"url": "cover", "width": 640, "height": 640}]}
				}
			}
		}
	}`)

	got, err := client.GetTrack(context.Background(), "track-id")
	if err != nil {
		t.Fatalf("GetTrack() error = %v", err)
	}
	if got.ID != "track-id" || got.Title != "Track" {
		t.Fatalf("track = %+v", got)
	}
	if got.Album == nil || got.Album.ID != "album-id" || got.CoverURL != "cover" {
		t.Fatalf("album/cover = %+v cover=%q", got.Album, got.CoverURL)
	}
}

func TestGetAlbumUsesPathfinderWithoutClientCredentials(t *testing.T) {
	client := newPathfinderTestClient(t, queryAlbumOperation, spotifyAlbumPathfinderBody())

	got, err := client.GetAlbum(context.Background(), "album-id")
	if err != nil {
		t.Fatalf("GetAlbum() error = %v", err)
	}
	if got.ID != "album-id" || got.Title != "Album" {
		t.Fatalf("album = %+v", got)
	}
	if got.TrackCount != 1 || got.CoverURL != "album-cover" {
		t.Fatalf("track count/cover = %d/%q", got.TrackCount, got.CoverURL)
	}
	if len(got.Artists) != 1 || got.Artists[0].Name != "Album Artist" {
		t.Fatalf("artists = %+v", got.Artists)
	}
}

func TestGetAlbumAsPlaylistUsesPathfinderWithoutClientCredentials(t *testing.T) {
	client := newPathfinderTestClient(t, queryAlbumOperation, spotifyAlbumPathfinderBody())

	got, err := client.GetAlbumAsPlaylist(context.Background(), "album-id")
	if err != nil {
		t.Fatalf("GetAlbumAsPlaylist() error = %v", err)
	}
	if got.ID != "album:album-id" || got.Title != "Album" || got.Creator != "Album Artist" {
		t.Fatalf("playlist = %+v", got)
	}
	if len(got.Tracks) != 1 || got.Tracks[0].ID != "track-id" {
		t.Fatalf("tracks = %+v", got.Tracks)
	}
	if got.Tracks[0].Album == nil || got.Tracks[0].Album.ID != "album-id" {
		t.Fatalf("track album = %+v", got.Tracks[0].Album)
	}
}

func TestGetPlaylistUsesPathfinderWithoutClientCredentials(t *testing.T) {
	client := newPathfinderTestClient(t, queryPlaylistOperation, `{
		"data": {
			"playlistV2": {
				"__typename": "Playlist",
				"id": "playlist-id",
				"name": "Playlist",
				"description": "Description",
				"ownerV2": {"data": {"displayName": "Owner"}},
				"images": {"items": [{"sources": [{"url": "playlist-cover", "width": 640, "height": 640}]}]},
				"content": {
					"totalCount": 1,
					"items": [{
						"itemV2": {
							"data": {
								"id": "track-id",
								"name": "Track",
								"uri": "spotify:track:track-id",
								"artists": {"items": [{"id": "artist-id", "profile": {"name": "Artist"}}]},
								"albumOfTrack": {
									"id": "album-id",
									"name": "Album",
									"coverArt": {"sources": [{"url": "album-cover", "width": 640, "height": 640}]}
								}
							}
						}
					}]
				}
			}
		}
	}`)

	got, err := client.GetPlaylist(context.Background(), "playlist-id")
	if err != nil {
		t.Fatalf("GetPlaylist() error = %v", err)
	}
	if got.ID != "playlist-id" || got.Title != "Playlist" || got.Creator != "Owner" {
		t.Fatalf("playlist = %+v", got)
	}
	if got.CoverURL != "playlist-cover" || len(got.Tracks) != 1 {
		t.Fatalf("cover/tracks = %q/%+v", got.CoverURL, got.Tracks)
	}
}

func TestGetArtistUsesPathfinderWithoutClientCredentials(t *testing.T) {
	client := newPathfinderTestClient(t, queryArtistOperation, `{
		"data": {
			"artistUnion": {
				"__typename": "Artist",
				"id": "artist-id",
				"profile": {"name": "Artist"},
				"visuals": {
					"avatarImage": {"sources": [{"url": "avatar", "width": 640, "height": 640}]}
				}
			}
		}
	}`)

	got, err := client.GetArtist(context.Background(), "artist-id")
	if err != nil {
		t.Fatalf("GetArtist() error = %v", err)
	}
	if got.ID != "artist-id" || got.Name != "Artist" || got.AvatarURL != "avatar" {
		t.Fatalf("artist = %+v", got)
	}
}

func newPathfinderTestClient(t *testing.T, operation, body string) *Client {
	t.Helper()

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
		var payload struct {
			OperationName string `json:"operationName"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload.OperationName != operation {
			t.Fatalf("operationName = %q, want %q", payload.OperationName, operation)
		}
		return spotifyTestResponse(req, http.StatusOK, body), nil
	})}
	return client
}

func spotifyAlbumPathfinderBody() string {
	return `{
		"data": {
			"albumUnion": {
				"__typename": "Album",
				"id": "album-id",
				"name": "Album",
				"date": {"year": 2024, "month": 5, "day": 6, "precision": "DAY", "isoString": "2024-05-06T00:00:00Z"},
				"coverArt": {"sources": [{"url": "album-cover", "width": 640, "height": 640}]},
				"artists": {"items": [{"id": "album-artist-id", "profile": {"name": "Album Artist"}}]},
				"tracksV2": {
					"totalCount": 1,
					"items": [{
						"track": {
							"id": "track-id",
							"name": "Track",
							"uri": "spotify:track:track-id",
							"artists": {"items": [{"id": "artist-id", "profile": {"name": "Artist"}}]},
							"albumOfTrack": {
								"id": "album-id",
								"name": "Album",
								"coverArt": {"sources": [{"url": "album-cover", "width": 640, "height": 640}]}
							}
						}
					}]
				}
			}
		}
	}`
}

func spotifyTestResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
