package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type shortLinkTestPlatform struct {
	*stubPlatform
	hosts []string
}

func (p *shortLinkTestPlatform) ShortLinkHosts() []string {
	return append([]string(nil), p.hosts...)
}

func TestShouldResolveHostSupportsSubdomains(t *testing.T) {
	manager := newStubManager()
	manager.Register(&shortLinkTestPlatform{stubPlatform: newStubPlatform("soda"), hosts: []string{"qishui.douyin.com"}})

	if !shouldResolveHost("qishui.douyin.com", manager) {
		t.Fatalf("expected exact host to resolve")
	}
	if !shouldResolveHost("www.qishui.douyin.com", manager) {
		t.Fatalf("expected subdomain host to resolve")
	}
	if shouldResolveHost("fakeqishui.douyin.com.evil.com", manager) {
		t.Fatalf("expected unrelated host not to resolve")
	}
}

func TestResolveShortURLReturnsRedirectLocation(t *testing.T) {
	finalURL := "https://music.douyin.com/qishui/share/playlist?playlist_id=7067729297428070437"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", finalURL)
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	manager := newStubManager()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	host := parsed.Hostname()
	manager.Register(&shortLinkTestPlatform{stubPlatform: newStubPlatform("soda"), hosts: []string{host}})

	resolved, err := resolveShortURL(context.Background(), manager, server.URL)
	if err != nil {
		t.Fatalf("resolveShortURL() error = %v", err)
	}
	if resolved != finalURL {
		t.Fatalf("resolveShortURL() = %q, want %q", resolved, finalURL)
	}
}

func TestResolveShortLinkTextReplacesShortURL(t *testing.T) {
	finalURL := "https://music.douyin.com/qishui/share/track?track_id=123456789"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", finalURL)
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	manager := newStubManager()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	host := parsed.Hostname()
	manager.Register(&shortLinkTestPlatform{stubPlatform: newStubPlatform("soda"), hosts: []string{host}})

	text := "看看这个链接 " + server.URL
	resolved := resolveShortLinkText(context.Background(), manager, text)
	if !strings.Contains(resolved, finalURL) {
		t.Fatalf("resolveShortLinkText() = %q, want contains %q", resolved, finalURL)
	}
	if strings.Contains(resolved, server.URL) {
		t.Fatalf("resolveShortLinkText() still contains original short url: %q", resolved)
	}
}

var _ platform.ShortLinkProvider = (*shortLinkTestPlatform)(nil)
var _ platform.Platform = (*shortLinkTestPlatform)(nil)

func (p *shortLinkTestPlatform) SupportsDownload() bool    { return false }
func (p *shortLinkTestPlatform) SupportsSearch() bool      { return false }
func (p *shortLinkTestPlatform) SupportsLyrics() bool      { return false }
func (p *shortLinkTestPlatform) SupportsRecognition() bool { return false }
func (p *shortLinkTestPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{}
}
func (p *shortLinkTestPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	return nil, platform.ErrUnsupported
}
func (p *shortLinkTestPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	return nil, platform.ErrUnsupported
}
func (p *shortLinkTestPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	return nil, platform.ErrUnsupported
}
func (p *shortLinkTestPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.ErrUnsupported
}
func (p *shortLinkTestPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	return nil, platform.ErrUnsupported
}
func (p *shortLinkTestPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return nil, platform.ErrUnsupported
}
func (p *shortLinkTestPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	return nil, platform.ErrUnsupported
}
func (p *shortLinkTestPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	return nil, platform.ErrUnsupported
}
