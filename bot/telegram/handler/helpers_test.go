package handler

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type fallbackTestPlatform struct {
	name           string
	searchFunc     func(ctx context.Context, query string, limit int) ([]platform.Track, error)
	supportsSearch bool
}

func (p *fallbackTestPlatform) Name() string              { return p.name }
func (p *fallbackTestPlatform) SupportsDownload() bool    { return false }
func (p *fallbackTestPlatform) SupportsSearch() bool      { return p.supportsSearch }
func (p *fallbackTestPlatform) SupportsLyrics() bool      { return false }
func (p *fallbackTestPlatform) SupportsRecognition() bool { return false }
func (p *fallbackTestPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{Search: p.supportsSearch}
}
func (p *fallbackTestPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	return nil, platform.ErrUnsupported
}
func (p *fallbackTestPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if p.searchFunc != nil {
		return p.searchFunc(ctx, query, limit)
	}
	return nil, platform.ErrUnsupported
}
func (p *fallbackTestPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	return nil, platform.ErrUnsupported
}
func (p *fallbackTestPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.ErrUnsupported
}
func (p *fallbackTestPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	return &platform.Track{ID: trackID, Title: "t", Duration: time.Second}, nil
}
func (p *fallbackTestPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return nil, platform.ErrUnsupported
}
func (p *fallbackTestPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	return nil, platform.ErrUnsupported
}
func (p *fallbackTestPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	return nil, platform.ErrUnsupported
}

func TestSanitizeFileName(t *testing.T) {
	name := "a/b:c*?d|e\\f\"g"
	safe := sanitizeFileName(name)
	if safe == name {
		t.Fatalf("expected sanitized name")
	}
}

func TestBuildMusicCaption(t *testing.T) {
	info := &botpkg.SongInfo{
		MusicID:     1,
		SongName:    "Song",
		SongArtists: "Artist",
		SongAlbum:   "Album",
		FileExt:     "mp3",
		MusicSize:   1024,
		BitRate:     320000,
	}
	caption := buildMusicCaption(nil, info, "botname")
	if caption == "" {
		t.Fatalf("expected caption")
	}
	if !strings.Contains(caption, "专辑: Album") {
		t.Fatalf("expected caption contains album line")
	}
}

func TestBuildMusicCaptionHidesAlbumLineWhenEmpty(t *testing.T) {
	info := &botpkg.SongInfo{
		SongName:    "Song",
		SongArtists: "Artist",
		SongAlbum:   "",
		FileExt:     "mp3",
		MusicSize:   1024,
		BitRate:     320000,
	}
	caption := buildMusicCaption(nil, info, "botname")
	if strings.Contains(caption, "专辑:") {
		t.Fatalf("expected caption to hide album line when album is empty, got %q", caption)
	}
}

func TestBuildMusicInfoTextHideAlbumLineWhenEmpty(t *testing.T) {
	text := buildMusicInfoText("Song", "", "mp3 1MB", "下载中...")
	if strings.Contains(text, "专辑:") {
		t.Fatalf("expected status text to hide album line when album is empty, got %q", text)
	}
	if !strings.Contains(text, "Song\nmp3 1MB\n下载中...") {
		t.Fatalf("unexpected status text: %q", text)
	}
}

func TestBuildMusicInfoTextKeepAlbumLine(t *testing.T) {
	text := buildMusicInfoText("Song", "Album", "mp3 1MB", "下载中...")
	if !strings.Contains(text, "专辑: Album") {
		t.Fatalf("expected status text contains album line, got %q", text)
	}
}

func TestUserVisibleDownloadErrorMappings(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: "处理超时，请稍后重试"},
		{name: "context canceled", err: context.Canceled, want: "任务已取消，请稍后重试"},
		{name: "download overloaded", err: errDownloadQueueOverloaded, want: "当前下载任务过多，请稍后再试"},
		{name: "upload queue full text", err: errors.New("upload queue is full"), want: "当前发送任务过多，请稍后再试"},
		{name: "rate limited", err: platform.ErrRateLimited, want: "请求过于频繁，请稍后重试"},
		{name: "auth required", err: platform.ErrAuthRequired, want: "平台认证已失效，请联系管理员更新凭据"},
		{name: "unavailable", err: platform.ErrUnavailable, want: "当前歌曲暂不可用，请稍后再试"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := userVisibleDownloadError(tt.err)
			if got != tt.want {
				t.Fatalf("unexpected message: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestIsTelegramFileIDInvalid(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "wrong file identifier", err: errors.New("Bad Request: wrong file identifier/HTTP URL specified"), want: true},
		{name: "file_id_invalid", err: errors.New("400 FILE_ID_INVALID"), want: true},
		{name: "invalid file id", err: errors.New("invalid file id"), want: true},
		{name: "other error", err: errors.New("network timeout"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTelegramFileIDInvalid(tt.err); got != tt.want {
				t.Fatalf("unexpected result: got %v want %v", got, tt.want)
			}
		})
	}
}

func TestSearchTracksWithFallback_PrimarySuccess(t *testing.T) {
	mgr := newStubManager()
	primary := &fallbackTestPlatform{name: "netease", supportsSearch: true, searchFunc: func(ctx context.Context, query string, limit int) ([]platform.Track, error) {
		return []platform.Track{{ID: "1", Title: "ok"}}, nil
	}}
	fallback := &fallbackTestPlatform{name: "qqmusic", supportsSearch: true, searchFunc: func(ctx context.Context, query string, limit int) ([]platform.Track, error) {
		return []platform.Track{{ID: "2", Title: "fallback"}}, nil
	}}
	mgr.Register(primary)
	mgr.Register(fallback)

	tracks, usedPlatform, usedFallback, err := searchTracksWithFallback(context.Background(), mgr, "netease", "qqmusic", "k", func(platformName string) int { return 10 }, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if usedFallback {
		t.Fatalf("expected primary path")
	}
	if usedPlatform != "netease" || len(tracks) != 1 || tracks[0].ID != "1" {
		t.Fatalf("unexpected result: platform=%s tracks=%v", usedPlatform, tracks)
	}
}

func TestSearchTracksWithFallback_FallbackOnEmpty(t *testing.T) {
	mgr := newStubManager()
	mgr.Register(&fallbackTestPlatform{name: "netease", supportsSearch: true, searchFunc: func(ctx context.Context, query string, limit int) ([]platform.Track, error) {
		return []platform.Track{}, nil
	}})
	mgr.Register(&fallbackTestPlatform{name: "qqmusic", supportsSearch: true, searchFunc: func(ctx context.Context, query string, limit int) ([]platform.Track, error) {
		return []platform.Track{{ID: "2", Title: "fallback"}}, nil
	}})

	tracks, usedPlatform, usedFallback, err := searchTracksWithFallback(context.Background(), mgr, "netease", "qqmusic", "k", nil, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !usedFallback || usedPlatform != "qqmusic" || len(tracks) != 1 {
		t.Fatalf("expected fallback result, got fallback=%v platform=%s tracks=%v", usedFallback, usedPlatform, tracks)
	}
}

func TestSearchTracksWithFallback_FallbackOnError(t *testing.T) {
	mgr := newStubManager()
	mgr.Register(&fallbackTestPlatform{name: "netease", supportsSearch: true, searchFunc: func(ctx context.Context, query string, limit int) ([]platform.Track, error) {
		return nil, platform.ErrUnavailable
	}})
	mgr.Register(&fallbackTestPlatform{name: "qqmusic", supportsSearch: true, searchFunc: func(ctx context.Context, query string, limit int) ([]platform.Track, error) {
		return []platform.Track{{ID: "2", Title: "fallback"}}, nil
	}})

	tracks, usedPlatform, usedFallback, err := searchTracksWithFallback(context.Background(), mgr, "netease", "qqmusic", "k", nil, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !usedFallback || usedPlatform != "qqmusic" || len(tracks) != 1 {
		t.Fatalf("expected fallback result, got fallback=%v platform=%s tracks=%v", usedFallback, usedPlatform, tracks)
	}
}

func TestSearchTracksWithFallback_NoFallbackWhenDisabled(t *testing.T) {
	mgr := newStubManager()
	mgr.Register(&fallbackTestPlatform{name: "netease", supportsSearch: true, searchFunc: func(ctx context.Context, query string, limit int) ([]platform.Track, error) {
		return []platform.Track{}, nil
	}})
	mgr.Register(&fallbackTestPlatform{name: "qqmusic", supportsSearch: true, searchFunc: func(ctx context.Context, query string, limit int) ([]platform.Track, error) {
		return []platform.Track{{ID: "2", Title: "fallback"}}, nil
	}})

	tracks, usedPlatform, usedFallback, err := searchTracksWithFallback(context.Background(), mgr, "netease", "qqmusic", "k", nil, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if usedFallback {
		t.Fatalf("did not expect fallback when fallbackOnEmpty=false")
	}
	if usedPlatform != "netease" || len(tracks) != 0 {
		t.Fatalf("expected empty primary result, got platform=%s tracks=%v", usedPlatform, tracks)
	}
}

func TestSearchTracksWithFallback_PrimaryUnsupported(t *testing.T) {
	mgr := newStubManager()
	mgr.Register(&fallbackTestPlatform{name: "netease", supportsSearch: false})
	mgr.Register(&fallbackTestPlatform{name: "qqmusic", supportsSearch: true, searchFunc: func(ctx context.Context, query string, limit int) ([]platform.Track, error) {
		return []platform.Track{{ID: "2", Title: "fallback"}}, nil
	}})

	tracks, usedPlatform, usedFallback, err := searchTracksWithFallback(context.Background(), mgr, "netease", "qqmusic", "k", nil, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !usedFallback || usedPlatform != "qqmusic" || len(tracks) != 1 {
		t.Fatalf("expected fallback result for unsupported primary, got fallback=%v platform=%s tracks=%v", usedFallback, usedPlatform, tracks)
	}
}

func TestSearchTracksWithFallback_PrimaryUnsupportedNoFallback(t *testing.T) {
	mgr := newStubManager()
	mgr.Register(&fallbackTestPlatform{name: "netease", supportsSearch: false})

	_, _, _, err := searchTracksWithFallback(context.Background(), mgr, "netease", "qqmusic", "k", nil, true)
	if !errors.Is(err, platform.ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

func TestUserVisibleSearchError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "unsupported", err: platform.ErrUnsupported, want: "此平台不支持搜索功能"},
		{name: "rate limited", err: platform.ErrRateLimited, want: "请求过于频繁，请稍后再试"},
		{name: "unavailable", err: platform.ErrUnavailable, want: "搜索服务暂时不可用"},
		{name: "default", err: errors.New("other"), want: noResults},
		{name: "nil", err: nil, want: noResults},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := userVisibleSearchError(tt.err, ""); got != tt.want {
				t.Fatalf("unexpected search error text: got %q want %q", got, tt.want)
			}
		})
	}

	if got := userVisibleSearchError(platform.ErrUnavailable, "自定义不可用"); got != "自定义不可用" {
		t.Fatalf("expected custom unavailable text, got %q", got)
	}
}

func TestUserVisiblePlaylistError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "unsupported", err: platform.ErrUnsupported, want: "此平台不支持获取歌单"},
		{name: "rate limited", err: platform.ErrRateLimited, want: "请求过于频繁，请稍后再试"},
		{name: "unavailable", err: platform.ErrUnavailable, want: "歌单服务暂时不可用"},
		{name: "not found", err: platform.ErrNotFound, want: "未找到歌单"},
		{name: "default", err: errors.New("other"), want: noResults},
		{name: "nil", err: nil, want: noResults},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := userVisiblePlaylistError(tt.err); got != tt.want {
				t.Fatalf("unexpected playlist error text: got %q want %q", got, tt.want)
			}
		})
	}
}
