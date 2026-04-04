package soda

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func newSodaTestClient(serverURL string) *Client {
	target, _ := url.Parse(serverURL)
	baseTransport := http.DefaultTransport
	return &Client{
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				clone := req.Clone(req.Context())
				rewritten := *clone.URL
				rewritten.Scheme = target.Scheme
				rewritten.Host = target.Host
				clone.URL = &rewritten
				clone.Host = target.Host
				return baseTransport.RoundTrip(clone)
			}),
		},
	}
}

func TestClientGetPlaylistHonorsOffsetLimit(t *testing.T) {
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/luna/pc/playlist/detail" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		requests = append(requests, r.URL.RawQuery)
		cursor := r.URL.Query().Get("cursor")
		cnt := r.URL.Query().Get("cnt")
		resp := sodaPlaylistDetailResponse{
			Playlist: sodaPlaylistMeta{ID: "pl1", Title: "Playlist", CountTracks: 55},
		}
		switch cursor {
		case "30":
			if cnt != "20" {
				t.Fatalf("first cnt=%s, want 20", cnt)
			}
			resp.MediaResources = []sodaPlaylistEntry{
				makePlaylistTrackEntry("t31", "Track 31"),
				makePlaylistTrackEntry("t32", "Track 32"),
				makePlaylistTrackEntry("t33", "Track 33"),
				makePlaylistTrackEntry("t34", "Track 34"),
				makePlaylistTrackEntry("t35", "Track 35"),
				makePlaylistTrackEntry("t36", "Track 36"),
				makePlaylistTrackEntry("t37", "Track 37"),
				makePlaylistTrackEntry("t38", "Track 38"),
				makePlaylistTrackEntry("t39", "Track 39"),
				makePlaylistTrackEntry("t40", "Track 40"),
				makePlaylistTrackEntry("t41", "Track 41"),
				makePlaylistTrackEntry("t42", "Track 42"),
				makePlaylistTrackEntry("t43", "Track 43"),
				makePlaylistTrackEntry("t44", "Track 44"),
				makePlaylistTrackEntry("t45", "Track 45"),
				makePlaylistTrackEntry("t46", "Track 46"),
				makePlaylistTrackEntry("t47", "Track 47"),
				makePlaylistTrackEntry("t48", "Track 48"),
				makePlaylistTrackEntry("t49", "Track 49"),
				makePlaylistTrackEntry("t50", "Track 50"),
			}
		case "50":
			if cnt != "5" {
				t.Fatalf("second cnt=%s, want 5", cnt)
			}
			resp.MediaResources = []sodaPlaylistEntry{
				makePlaylistTrackEntry("t51", "Track 51"),
				makePlaylistTrackEntry("t52", "Track 52"),
				makePlaylistTrackEntry("t53", "Track 53"),
				makePlaylistTrackEntry("t54", "Track 54"),
				makePlaylistTrackEntry("t55", "Track 55"),
			}
		default:
			t.Fatalf("unexpected cursor: %s", cursor)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := newSodaTestClient(server.URL)
	ctx := platform.WithPlaylistOffset(context.Background(), 30)
	ctx = platform.WithPlaylistLimit(ctx, 25)
	playlist, err := client.GetPlaylist(ctx, "pl1")
	if err != nil {
		t.Fatalf("GetPlaylist() error = %v", err)
	}
	if playlist.TrackCount != 55 {
		t.Fatalf("GetPlaylist() track count = %d, want 55", playlist.TrackCount)
	}
	if len(playlist.Tracks) != 25 {
		t.Fatalf("GetPlaylist() returned %d tracks, want 25", len(playlist.Tracks))
	}
	if playlist.Tracks[0].ID != "t31" || playlist.Tracks[24].ID != "t55" {
		t.Fatalf("GetPlaylist() returned unexpected range: first=%s last=%s", playlist.Tracks[0].ID, playlist.Tracks[24].ID)
	}
	if len(requests) != 2 {
		t.Fatalf("GetPlaylist() requests = %d, want 2", len(requests))
	}
}

func TestClientGetAlbumHonorsOffsetLimitAndTrackCount(t *testing.T) {
	payload := sodaShareAlbumPayload{
		AlbumInfo: sodaAlbumMeta{
			ID:         "ab1",
			Name:       "Album",
			Intro:      "真实简介",
			TrackCount: 9,
			Artists: []struct {
				Name string `json:"name"`
				ID   string `json:"id"`
			}{
				{Name: "Artist", ID: "ar1"},
			},
		},
		TrackList: []sodaTrack{
			{ID: "t1", Name: "Track 1"},
			{ID: "t2", Name: "Track 2"},
			{ID: "t3", Name: "Track 3"},
			{ID: "t4", Name: "Track 4"},
			{ID: "t5", Name: "Track 5"},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	page := `<html><script>window._ROUTER_DATA = {"loaderData":{"album_page":` + string(raw) + `}}</script></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/qishui/share/album" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	client := newSodaTestClient(server.URL)
	ctx := platform.WithPlaylistOffset(context.Background(), 1)
	ctx = platform.WithPlaylistLimit(ctx, 2)
	album, tracks, err := client.GetAlbum(ctx, "ab1")
	if err != nil {
		t.Fatalf("GetAlbum() error = %v", err)
	}
	if album.TrackCount != 9 {
		t.Fatalf("GetAlbum() album track count = %d, want 9", album.TrackCount)
	}
	if album.Description != "真实简介" {
		t.Fatalf("GetAlbum() album description = %q", album.Description)
	}
	if len(tracks) != 2 || tracks[0].ID != "t2" || tracks[1].ID != "t3" {
		t.Fatalf("GetAlbum() tracks = %+v", tracks)
	}
	if tracks[0].Album == nil || tracks[0].Album.ID != "ab1" {
		t.Fatalf("GetAlbum() track album missing: %+v", tracks[0].Album)
	}
	playlist := &platform.Playlist{
		ID:          encodeAlbumCollectionID("ab1"),
		Platform:    "soda",
		Title:       album.Title,
		Description: firstNonEmptyString(album.Description, "专辑"),
		Creator:     firstNonEmptyString(joinSodaArtistNames(album.Artists), "汽水音乐"),
		TrackCount:  maxInt(album.TrackCount, len(tracks)),
		Tracks:      tracks,
	}
	if playlist.Description != "真实简介" {
		t.Fatalf("album->playlist description = %q", playlist.Description)
	}
}

func TestClientGetTrackKeepsShareURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/luna/pc/track_v2" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(sodaTrackV2Response{
			TrackInfo: sodaTrack{
				ID:   "123456789",
				Name: "Track",
			},
			TrackPlayer: struct {
				URLPlayerInfo string `json:"url_player_info"`
			}{
				URLPlayerInfo: "https://media.example.com/player?video_id=abc",
			},
		})
	}))
	defer server.Close()

	client := newSodaTestClient(server.URL)
	track, lyric, err := client.GetTrack(context.Background(), "123456789")
	if err != nil {
		t.Fatalf("GetTrack() error = %v", err)
	}
	if lyric != "" {
		t.Fatalf("GetTrack() lyric = %q, want empty", lyric)
	}
	if track == nil {
		t.Fatal("GetTrack() returned nil track")
	}
	if track.URL != "https://music.douyin.com/qishui/share/track?track_id=123456789" {
		t.Fatalf("GetTrack() url = %q", track.URL)
	}
}

func TestClientFetchDownloadInfoUsesPlayerInfoURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/luna/pc/track_v2":
			_ = json.NewEncoder(w).Encode(sodaTrackV2Response{
				TrackInfo: sodaTrack{
					ID:   "123456789",
					Name: "Track",
				},
				TrackPlayer: struct {
					URLPlayerInfo string `json:"url_player_info"`
				}{
					URLPlayerInfo: "https://media.example.com/player?video_id=abc",
				},
			})
		case "/player":
			resp := sodaPlayInfoResponse{}
			resp.Result.Data.PlayInfoList = []sodaPlayInfo{{
				MainPlayURL: "https://download.example.com/audio.m4a",
				PlayAuth:    "auth-token",
				Size:        1024,
				Bitrate:     320,
				Format:      "m4a",
				Quality:     "higher",
			}}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newSodaTestClient(server.URL)
	info, err := client.FetchDownloadInfo(context.Background(), "123456789", platform.QualityHigh)
	if err != nil {
		t.Fatalf("FetchDownloadInfo() error = %v", err)
	}
	if info == nil {
		t.Fatal("FetchDownloadInfo() returned nil")
	}
	if info.URL != "https://download.example.com/audio.m4a" {
		t.Fatalf("FetchDownloadInfo() url = %q", info.URL)
	}
	if info.Headers["X-Soda-Play-Auth"] != "auth-token" {
		t.Fatalf("FetchDownloadInfo() auth header = %q", info.Headers["X-Soda-Play-Auth"])
	}
}

func TestEnsureSodaPlayableLosslessFLAC_RewritesAfterValidation(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "track.mp4")
	dstPath := filepath.Join(tmpDir, "track.flac")
	if err := os.WriteFile(srcPath, []byte("mp4"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	originalExtract := sodaExtractLosslessFLAC
	originalRewrite := sodaRewriteLosslessFLAC
	originalValidate := sodaValidateAudioFile
	t.Cleanup(func() {
		sodaExtractLosslessFLAC = originalExtract
		sodaRewriteLosslessFLAC = originalRewrite
		sodaValidateAudioFile = originalValidate
	})

	var validateCalls []string
	sodaExtractLosslessFLAC = func(_ context.Context, gotSrc, gotDst string) error {
		if gotSrc != srcPath {
			t.Fatalf("extract src = %q, want %q", gotSrc, srcPath)
		}
		if filepath.Ext(gotDst) != ".extracting" {
			t.Fatalf("extract dst ext = %q, want .extracting", filepath.Ext(gotDst))
		}
		return os.WriteFile(gotDst, []byte("extracted-flac"), 0o644)
	}
	sodaRewriteLosslessFLAC = func(_ context.Context, gotSrc, gotDst string) error {
		if filepath.Ext(gotSrc) != ".extracting" {
			t.Fatalf("rewrite src ext = %q, want .extracting", filepath.Ext(gotSrc))
		}
		if filepath.Ext(gotDst) != ".rewritten" {
			t.Fatalf("rewrite dst ext = %q, want .rewritten", filepath.Ext(gotDst))
		}
		return os.WriteFile(gotDst, []byte("rewritten-flac"), 0o644)
	}
	sodaValidateAudioFile = func(_ context.Context, gotPath, codec string) error {
		if codec != "flac" {
			t.Fatalf("validate codec = %q, want flac", codec)
		}
		validateCalls = append(validateCalls, filepath.Ext(gotPath))
		return nil
	}

	if err := ensureSodaPlayableLosslessFLAC(context.Background(), srcPath, dstPath); err != nil {
		t.Fatalf("ensureSodaPlayableLosslessFLAC() error = %v", err)
	}
	if got := string(mustReadFile(t, dstPath)); got != "rewritten-flac" {
		t.Fatalf("dst content = %q, want rewritten-flac", got)
	}
	if len(validateCalls) != 2 || validateCalls[0] != ".extracting" || validateCalls[1] != ".rewritten" {
		t.Fatalf("validate calls = %#v, want [.extracting .rewritten]", validateCalls)
	}
}

func TestEnsureSodaPlayableLosslessFLAC_FailsWhenRewriteValidationFails(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "track.mp4")
	dstPath := filepath.Join(tmpDir, "track.flac")
	if err := os.WriteFile(srcPath, []byte("mp4"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	originalExtract := sodaExtractLosslessFLAC
	originalRewrite := sodaRewriteLosslessFLAC
	originalValidate := sodaValidateAudioFile
	t.Cleanup(func() {
		sodaExtractLosslessFLAC = originalExtract
		sodaRewriteLosslessFLAC = originalRewrite
		sodaValidateAudioFile = originalValidate
	})

	sodaExtractLosslessFLAC = func(_ context.Context, _, gotDst string) error {
		return os.WriteFile(gotDst, []byte("extracted-flac"), 0o644)
	}
	sodaRewriteLosslessFLAC = func(_ context.Context, _, gotDst string) error {
		return os.WriteFile(gotDst, []byte("rewritten-flac"), 0o644)
	}
	sodaValidateAudioFile = func(_ context.Context, gotPath, _ string) error {
		if filepath.Ext(gotPath) == ".rewritten" {
			return errors.New("decode failed")
		}
		return nil
	}

	err := ensureSodaPlayableLosslessFLAC(context.Background(), srcPath, dstPath)
	if err == nil || !strings.Contains(err.Error(), "validate rewritten soda flac") {
		t.Fatalf("ensureSodaPlayableLosslessFLAC() error = %v, want rewrite validation error", err)
	}
	if _, statErr := os.Stat(dstPath); !os.IsNotExist(statErr) {
		t.Fatalf("dst should not exist, stat err = %v", statErr)
	}
}

func mustReadFile(t *testing.T, filePath string) []byte {
	t.Helper()
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read %s: %v", filePath, err)
	}
	return data
}

func makePlaylistTrackEntry(id, name string) sodaPlaylistEntry {
	entry := sodaPlaylistEntry{Type: "track"}
	entry.Entity.TrackWrapper.Track = sodaTrack{ID: id, Name: name}
	return entry
}
