package spotify

import (
	"context"
	"io"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// SpotifyPlatform implements platform.Platform. Metadata comes from the Web API
// or authenticated web-player Pathfinder queries. Audio is decrypted AAC/MP4
// from Spotify's CDN; there is deliberately no cross-platform fallback.
type SpotifyPlatform struct {
	client *Client
	native directAudioSource
}

// NewPlatform builds a Spotify platform. The native audio source is attached
// separately via WithNativeSource.
func NewPlatform(client *Client) *SpotifyPlatform {
	return &SpotifyPlatform{client: client}
}

// WithNativeSource attaches the native (real Spotify audio) source. Returns the
// platform for chaining.
func (p *SpotifyPlatform) WithNativeSource(src directAudioSource) *SpotifyPlatform {
	if p != nil {
		p.native = src
	}
	return p
}

func (p *SpotifyPlatform) Name() string { return platformName }

// SupportsDownload reports true when the native audio source is wired, so the
// router/UI won't offer downloads that can't be fulfilled.
func (p *SpotifyPlatform) SupportsDownload() bool {
	return p != nil && p.native != nil
}
func (p *SpotifyPlatform) SupportsSearch() bool      { return true }
func (p *SpotifyPlatform) SupportsLyrics() bool      { return false }
func (p *SpotifyPlatform) SupportsRecognition() bool { return false }

func (p *SpotifyPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Download:    p.SupportsDownload(),
		Search:      true,
		Lyrics:      false,
		Recognition: false,
		HiRes:       false,
	}
}

// Metadata exposes display/alias info (optional MetadataProvider interface).
func (p *SpotifyPlatform) Metadata() platform.Meta { return metadata() }

// GetDownloadInfo returns real Spotify AAC audio via the native Widevine path.
// Failures are returned directly rather than substituting another platform's
// recording.
func (p *SpotifyPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "track", trackID)
	}
	if p.native == nil {
		return nil, platform.NewUnavailableError(platformName, "download", trackID)
	}
	return p.native.BuildDownloadInfo(ctx, trackID, quality)
}

func (p *SpotifyPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "search", query)
	}
	return p.client.Search(ctx, query, limit)
}

func (p *SpotifyPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	return nil, platform.NewUnsupportedError(platformName, "lyrics")
}

func (p *SpotifyPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.NewUnsupportedError(platformName, "recognition")
}

func (p *SpotifyPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "track", trackID)
	}
	return p.client.GetTrack(ctx, trackID)
}

func (p *SpotifyPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "artist", artistID)
	}
	return p.client.GetArtist(ctx, artistID)
}

func (p *SpotifyPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "album", albumID)
	}
	return p.client.GetAlbum(ctx, albumID)
}

// GetPlaylist resolves both real playlists and albums (the URL matcher encodes
// albums as "album:<id>" so they can be browsed as track lists).
func (p *SpotifyPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "playlist", playlistID)
	}
	kind, id := decodeCollectionID(playlistID)
	if kind == "album" {
		return p.client.GetAlbumAsPlaylist(ctx, id)
	}
	return p.client.GetPlaylist(ctx, id)
}

// --- optional matcher interfaces ---

func (p *SpotifyPlatform) MatchURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchURL(rawURL)
}

func (p *SpotifyPlatform) MatchPlaylistURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchPlaylistURL(rawURL)
}

func (p *SpotifyPlatform) MatchArtistURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchArtistURL(rawURL)
}

func (p *SpotifyPlatform) MatchText(text string) (string, bool) {
	return NewTextMatcher().MatchText(text)
}
