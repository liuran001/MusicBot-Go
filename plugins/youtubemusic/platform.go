package youtubemusic

import (
	"context"
	"io"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// YouTubeMusicPlatform implements platform.Platform backed by an InnerTube client.
type YouTubeMusicPlatform struct {
	client *Client
}

// NewPlatform wraps a client as a Platform.
func NewPlatform(client *Client) *YouTubeMusicPlatform {
	return &YouTubeMusicPlatform{client: client}
}

func (p *YouTubeMusicPlatform) Name() string { return platformName }

func (p *YouTubeMusicPlatform) SupportsDownload() bool    { return true }
func (p *YouTubeMusicPlatform) SupportsSearch() bool      { return true }
func (p *YouTubeMusicPlatform) SupportsLyrics() bool      { return true }
func (p *YouTubeMusicPlatform) SupportsRecognition() bool { return false }

func (p *YouTubeMusicPlatform) Capabilities() platform.Capabilities {
	// YouTube Music tops out around opus/AAC 256k — no lossless/Hi-Res.
	return platform.Capabilities{Download: true, Search: true, Lyrics: true, Recognition: false, HiRes: false}
}

// Metadata exposes display/alias info (optional MetadataProvider interface).
func (p *YouTubeMusicPlatform) Metadata() platform.Meta { return metadata() }

func (p *YouTubeMusicPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "track", trackID)
	}
	return p.client.GetDownloadInfo(ctx, trackID, quality)
}

func (p *YouTubeMusicPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "search", query)
	}
	return p.client.Search(ctx, query, limit)
}

func (p *YouTubeMusicPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnsupportedError(platformName, "lyrics")
	}
	return p.client.GetLyrics(ctx, trackID)
}

func (p *YouTubeMusicPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.NewUnsupportedError(platformName, "recognition")
}

func (p *YouTubeMusicPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "track", trackID)
	}
	return p.client.GetTrack(ctx, trackID)
}

func (p *YouTubeMusicPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return nil, platform.NewUnsupportedError(platformName, "artist")
}

func (p *YouTubeMusicPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	return nil, platform.NewUnsupportedError(platformName, "album")
}

func (p *YouTubeMusicPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	return nil, platform.NewUnsupportedError(platformName, "playlist")
}

// --- optional matcher interfaces (delegate to the stateless matchers) ---

func (p *YouTubeMusicPlatform) MatchURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchURL(rawURL)
}

func (p *YouTubeMusicPlatform) MatchPlaylistURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchPlaylistURL(rawURL)
}

func (p *YouTubeMusicPlatform) MatchText(text string) (string, bool) {
	return NewTextMatcher().MatchText(text)
}

// ShortLinkHosts declares youtu.be so short links are resolved before matching.
func (p *YouTubeMusicPlatform) ShortLinkHosts() []string {
	return []string{"youtu.be"}
}
