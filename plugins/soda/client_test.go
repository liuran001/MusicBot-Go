package soda

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot"
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
			ID:          "ab1",
			Name:        "Album",
			Intro:       "真实简介",
			ReleaseDate: sodaFlexibleDate("2024-02-03"),
			TrackCount:  9,
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
	if album.Year != 2024 {
		t.Fatalf("GetAlbum() album year = %d, want 2024", album.Year)
	}
	if album.ReleaseDate == nil || album.ReleaseDate.Format("2006-01-02") != "2024-02-03" {
		t.Fatalf("GetAlbum() album release date = %v, want 2024-02-03", album.ReleaseDate)
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

func TestSodaFlexibleDateSupportsStringAndNumber(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantDate string
		wantYear int
	}{
		{
			name:     "string date",
			payload:  `{"id":"ab1","name":"Album","release_date":"2024-02-03"}`,
			wantDate: "2024-02-03",
			wantYear: 2024,
		},
		{
			name:     "numeric date",
			payload:  `{"id":"ab1","name":"Album","release_date":20240203}`,
			wantDate: "2024-02-03",
			wantYear: 2024,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var meta sodaAlbumMeta
			if err := json.Unmarshal([]byte(tt.payload), &meta); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			album := convertSodaAlbum(meta)
			if album == nil {
				t.Fatal("convertSodaAlbum() returned nil")
			}
			if album.Year != tt.wantYear {
				t.Fatalf("convertSodaAlbum() year = %d, want %d", album.Year, tt.wantYear)
			}
			if album.ReleaseDate == nil || album.ReleaseDate.Format("2006-01-02") != tt.wantDate {
				t.Fatalf("convertSodaAlbum() release date = %v, want %s", album.ReleaseDate, tt.wantDate)
			}
		})
	}
}

func TestSodaAlbumPayloadParsesNumericReleaseDate(t *testing.T) {
	const payload = `{"albumInfo":{"id":"6696534425410209793","name":"Album","intro":"简介","release_date":20240203,"track_count":1,"artists":[{"id":"ar1","name":"Artist"}]},"trackList":[{"id":"t1","name":"Track 1"}]}`
	var got sodaShareAlbumPayload
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.AlbumInfo.ID != "6696534425410209793" {
		t.Fatalf("album id = %q", got.AlbumInfo.ID)
	}
	album := convertSodaAlbum(got.AlbumInfo)
	if album == nil {
		t.Fatal("convertSodaAlbum() returned nil")
	}
	if album.Year != 2024 {
		t.Fatalf("convertSodaAlbum() year = %d, want 2024", album.Year)
	}
	if album.ReleaseDate == nil || album.ReleaseDate.Format("2006-01-02") != "2024-02-03" {
		t.Fatalf("convertSodaAlbum() release date = %v, want 2024-02-03", album.ReleaseDate)
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
		if filepath.Ext(gotDst) != ".flac" || !strings.Contains(gotDst, ".extracting") {
			t.Fatalf("extract dst = %q, want *.extracting.flac", gotDst)
		}
		return os.WriteFile(gotDst, []byte("extracted-flac"), 0o644)
	}
	sodaRewriteLosslessFLAC = func(_ context.Context, gotSrc, gotDst string) error {
		if filepath.Ext(gotSrc) != ".flac" || !strings.Contains(gotSrc, ".extracting") {
			t.Fatalf("rewrite src = %q, want *.extracting.flac", gotSrc)
		}
		if filepath.Ext(gotDst) != ".flac" || !strings.Contains(gotDst, ".rewritten") {
			t.Fatalf("rewrite dst = %q, want *.rewritten.flac", gotDst)
		}
		return os.WriteFile(gotDst, []byte("rewritten-flac"), 0o644)
	}
	sodaValidateAudioFile = func(_ context.Context, gotPath, codec string) error {
		if codec != "flac" {
			t.Fatalf("validate codec = %q, want flac", codec)
		}
		validateCalls = append(validateCalls, gotPath)
		return nil
	}

	if err := ensureSodaPlayableLosslessFLAC(context.Background(), srcPath, dstPath); err != nil {
		t.Fatalf("ensureSodaPlayableLosslessFLAC() error = %v", err)
	}
	if got := string(mustReadFile(t, dstPath)); got != "rewritten-flac" {
		t.Fatalf("dst content = %q, want rewritten-flac", got)
	}
	if len(validateCalls) != 2 || !strings.Contains(validateCalls[0], ".extracting") || !strings.Contains(validateCalls[1], ".rewritten") {
		t.Fatalf("validate calls = %#v, want extracting then rewritten paths", validateCalls)
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
		if strings.Contains(gotPath, ".rewritten") {
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

func TestClientDownloadAndDecryptOnce_LosslessMP4KeepsDecryptedData(t *testing.T) {
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "track.mp4")
	rawData := []byte("0000ftyp0000fLaCraw-lossless-data")
	decryptedData := []byte("decrypted-lossless-data")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(rawData)
	}))
	defer server.Close()

	originalProbe := sodaProbeAudioCodec
	originalEnsure := sodaEnsurePlayableFLAC
	originalDecrypt := sodaDecryptAudio
	originalDecryptWithLog := sodaDecryptAudioWithLog
	t.Cleanup(func() {
		sodaProbeAudioCodec = originalProbe
		sodaEnsurePlayableFLAC = originalEnsure
		sodaDecryptAudio = originalDecrypt
		sodaDecryptAudioWithLog = originalDecryptWithLog
	})

	sodaDecryptAudio = func(data []byte, playAuth string) ([]byte, error) {
		if string(data) != string(rawData) {
			return nil, fmt.Errorf("unexpected encrypted data: %q", string(data))
		}
		if playAuth != "ok" {
			return nil, fmt.Errorf("unexpected play auth: %q", playAuth)
		}
		return append([]byte(nil), decryptedData...), nil
	}
	sodaDecryptAudioWithLog = func(data []byte, playAuth string, _ bot.Logger) ([]byte, error) {
		return sodaDecryptAudio(data, playAuth)
	}
	sodaProbeAudioCodec = func(filePath string) (string, error) {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", err
		}
		if string(data) != string(decryptedData) {
			return "", fmt.Errorf("unexpected file content during probe: %q", string(data))
		}
		return "aac", nil
	}
	sodaEnsurePlayableFLAC = func(_ context.Context, _ string, _ string) error {
		t.Fatal("lossless flac extraction should not run for non-flac codec")
		return nil
	}

	client := &Client{httpClient: server.Client()}
	info := &platform.DownloadInfo{
		URL:     server.URL,
		Format:  "mp4",
		Headers: map[string]string{"X-Soda-Play-Auth": "ok"},
	}

	written, err := client.downloadAndDecryptOnce(context.Background(), server.URL, info, destPath, nil)
	if err != nil {
		t.Fatalf("downloadAndDecryptOnce() error = %v", err)
	}
	if written != int64(len(decryptedData)) {
		t.Fatalf("downloadAndDecryptOnce() written = %d, want %d", written, len(decryptedData))
	}
	if got := mustReadFile(t, destPath); string(got) != string(decryptedData) {
		t.Fatalf("downloaded file = %q, want decrypted data %q", string(got), string(decryptedData))
	}
}

func TestClientDownloadAndDecryptOnce_LosslessFLACExtractionFailureReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "track.mp4")
	rawData := []byte("lossless-source")
	extractErr := errors.New("ffmpeg decode failed")
	logger := &captureLogger{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(rawData)
	}))
	defer server.Close()

	originalProbe := sodaProbeAudioCodec
	originalEnsure := sodaEnsurePlayableFLAC
	t.Cleanup(func() {
		sodaProbeAudioCodec = originalProbe
		sodaEnsurePlayableFLAC = originalEnsure
	})

	sodaProbeAudioCodec = func(_ string) (string, error) {
		return "flac", nil
	}
	sodaEnsurePlayableFLAC = func(_ context.Context, srcPath, dstPath string) error {
		if srcPath != destPath {
			t.Fatalf("ensure srcPath = %q, want %q", srcPath, destPath)
		}
		if dstPath != strings.TrimSuffix(destPath, filepath.Ext(destPath))+".flac" {
			t.Fatalf("ensure dstPath = %q", dstPath)
		}
		return extractErr
	}

	client := &Client{httpClient: server.Client(), logger: logger}
	info := &platform.DownloadInfo{
		URL:     server.URL,
		Format:  "mp4",
		Headers: map[string]string{},
	}

	written, err := client.downloadAndDecryptOnce(context.Background(), server.URL, info, destPath, nil)
	if err == nil || !strings.Contains(err.Error(), "extract playable flac from lossless container") {
		t.Fatalf("downloadAndDecryptOnce() error = %v, want extraction failure", err)
	}
	if written != 0 {
		t.Fatalf("downloadAndDecryptOnce() written = %d, want 0", written)
	}
	if !logger.hasWarnContaining("failed to extract playable flac from lossless container") {
		t.Fatalf("expected warn log, got %#v", logger.warns)
	}
}

func TestParseSodaSenc_WithSubsamplesAndIVSizes(t *testing.T) {
	t.Run("8-byte iv with subsamples", func(t *testing.T) {
		data := []byte{
			0x00, 0x00, 0x00, 0x02,
			0x00, 0x00, 0x00, 0x01,
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
			0x00, 0x02,
			0x00, 0x03, 0x00, 0x00, 0x00, 0x05,
			0x00, 0x01, 0x00, 0x00, 0x00, 0x02,
		}

		samples, ivSize, hasSubsamples := parseSodaSenc(data)
		if !hasSubsamples {
			t.Fatal("expected hasSubsamples=true")
		}
		if ivSize != 8 {
			t.Fatalf("ivSize=%d, want 8", ivSize)
		}
		if len(samples) != 1 {
			t.Fatalf("sample count=%d, want 1", len(samples))
		}
		if got := samples[0].IV; len(got) != 8 || got[0] != 0x01 || got[7] != 0x08 {
			t.Fatalf("unexpected iv=%v", got)
		}
		if len(samples[0].Subsamples) != 2 {
			t.Fatalf("subsample count=%d, want 2", len(samples[0].Subsamples))
		}
		if samples[0].Subsamples[0].ClearBytes != 3 || samples[0].Subsamples[0].EncryptedBytes != 5 {
			t.Fatalf("unexpected first subsample=%+v", samples[0].Subsamples[0])
		}
	})

	t.Run("16-byte iv without subsamples", func(t *testing.T) {
		data := []byte{
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x01,
			0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
			0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
		}

		samples, ivSize, hasSubsamples := parseSodaSenc(data)
		if hasSubsamples {
			t.Fatal("expected hasSubsamples=false")
		}
		if ivSize != 16 {
			t.Fatalf("ivSize=%d, want 16", ivSize)
		}
		if len(samples) != 1 || len(samples[0].IV) != 16 {
			t.Fatalf("unexpected samples=%+v", samples)
		}
	})
}

func TestDecryptSodaSample_OnlyDecryptsEncryptedRanges(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := []byte{0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}

	plain := []byte("ABChelloZ!!")
	stream := cipher.NewCTR(block, append(iv, make([]byte, 8)...))
	encryptedRanges := make([]byte, 7)
	encryptedSource := append(append([]byte{}, plain[3:8]...), plain[9:]...)
	stream.XORKeyStream(encryptedRanges, encryptedSource)

	sample := append([]byte{}, plain[:3]...)
	sample = append(sample, encryptedRanges[:5]...)
	sample = append(sample, plain[8:9]...)
	sample = append(sample, encryptedRanges[5:]...)

	decrypted, err := decryptSodaSample(block, sample, sodaSencSample{
		IV: iv,
		Subsamples: []sodaSencSubsample{
			{ClearBytes: 3, EncryptedBytes: 5},
			{ClearBytes: 1, EncryptedBytes: 2},
		},
	})
	if err != nil {
		t.Fatalf("decryptSodaSample() error = %v", err)
	}
	if string(decrypted) != string(plain) {
		t.Fatalf("decrypted=%q, want %q", string(decrypted), string(plain))
	}
}

func TestParseSodaSampleRanges_UsesChunkOffsets(t *testing.T) {
	stblPayload := bytesJoin(
		makeBox("stsz", bytesJoin([]byte{
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x03,
		}, u32(4), u32(5), u32(6))),
		makeBox("stsc", bytesJoin([]byte{
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x02,
		}, bytesJoin(u32(1), u32(2), u32(1)), bytesJoin(u32(2), u32(1), u32(1)))),
		makeBox("stco", bytesJoin([]byte{
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x02,
		}, u32(100), u32(200))),
	)
	fileData := make([]byte, 256)
	copy(fileData[40:], makeBox("stbl", stblPayload))
	stbl, err := findSodaBox(fileData, "stbl", 40, len(fileData))
	if err != nil {
		t.Fatalf("findSodaBox(stbl) error = %v", err)
	}
	ranges, err := parseSodaSampleRanges(stbl, fileData)
	if err != nil {
		t.Fatalf("parseSodaSampleRanges() error = %v", err)
	}
	if len(ranges) != 3 {
		t.Fatalf("range count=%d, want 3", len(ranges))
	}
	got := [][2]int{{ranges[0].Offset, ranges[0].Size}, {ranges[1].Offset, ranges[1].Size}, {ranges[2].Offset, ranges[2].Size}}
	want := [][2]int{{100, 4}, {104, 5}, {200, 6}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ranges=%v, want %v", got, want)
	}
}

func TestRewriteSodaStsdAudioSampleEntry_UsesFrmaCodec(t *testing.T) {
	entry := makeAudioSampleEntry("enca", makeBox("sinf", bytesJoin(
		makeBox("frma", []byte("mp4a")),
		makeBox("schm", append([]byte{0, 0, 0, 0}, []byte("cenc")...)),
	)))
	stsd := makeBox("stsd", append([]byte{
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01,
	}, entry...))
	rewriteSodaStsdAudioSampleEntry(stsd)
	if got := string(stsd[20:24]); got != "mp4a" {
		t.Fatalf("sample entry type=%q, want mp4a", got)
	}
}

type captureLogger struct {
	debugs []string
	warns  []string
}

func (l *captureLogger) Debug(msg string, args ...any) {
	l.debugs = append(l.debugs, formatLogMessage(msg, args...))
}

func (l *captureLogger) Info(string, ...any) {}

func (l *captureLogger) Warn(msg string, args ...any) {
	l.warns = append(l.warns, formatLogMessage(msg, args...))
}

func (l *captureLogger) Error(string, ...any) {}

func (l *captureLogger) With(args ...any) bot.Logger {
	return l
}

func (l *captureLogger) hasWarnContaining(substr string) bool {
	for _, item := range l.warns {
		if strings.Contains(item, substr) {
			return true
		}
	}
	return false
}

func formatLogMessage(msg string, args ...any) string {
	var b strings.Builder
	b.WriteString(msg)
	for i := 0; i+1 < len(args); i += 2 {
		b.WriteString(" ")
		b.WriteString(fmt.Sprint(args[i]))
		b.WriteString("=")
		b.WriteString(fmt.Sprint(args[i+1]))
	}
	if len(args)%2 == 1 {
		b.WriteString(" ")
		b.WriteString(fmt.Sprint(args[len(args)-1]))
	}
	return b.String()
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

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func makeBox(boxType string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b[:4], uint32(len(b)))
	copy(b[4:8], []byte(boxType))
	copy(b[8:], payload)
	return b
}

func bytesJoin(parts ...[]byte) []byte {
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	out := make([]byte, 0, total)
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func makeAudioSampleEntry(sampleType string, childBoxes []byte) []byte {
	prefix := make([]byte, 28)
	entry := make([]byte, 8+len(prefix)+len(childBoxes))
	binary.BigEndian.PutUint32(entry[:4], uint32(len(entry)))
	copy(entry[4:8], []byte(sampleType))
	copy(entry[8:], prefix)
	copy(entry[8+len(prefix):], childBoxes)
	return entry
}
