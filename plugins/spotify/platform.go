package spotify

import (
	"context"
	"io"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// SpotifyPlatform implements platform.Platform. Metadata + search always come
// from the Spotify Web API. Audio is resolved in two tiers:
//
//  1. native — real Spotify audio (decrypted Ogg Vorbis) via the embedded
//     librespot path, when the operator has completed the one-time login.
//  2. resolver — a YouTube Music delegate used as a fallback when native is
//     unavailable, not authenticated, or the track is DRM/region-blocked.
//
// Either tier may be nil. SupportsDownload is true when at least one is wired.
type SpotifyPlatform struct {
	client   *Client
	resolver audioResolver
	native   directAudioSource
}

// NewPlatform builds a Spotify platform. resolver may be nil (downloads then
// return ErrUnavailable, metadata/search still work).
func NewPlatform(client *Client, resolver audioResolver) *SpotifyPlatform {
	return &SpotifyPlatform{client: client, resolver: resolver}
}

// WithNativeSource attaches the native (real Spotify audio) source. Returns the
// platform for chaining. A nil source leaves the platform delegate-only.
func (p *SpotifyPlatform) WithNativeSource(src directAudioSource) *SpotifyPlatform {
	if p != nil {
		p.native = src
	}
	return p
}

func (p *SpotifyPlatform) Name() string { return platformName }

// SupportsDownload reports true when either a native source or an audio
// resolver is wired, so the router/UI won't offer downloads that can't be
// fulfilled.
func (p *SpotifyPlatform) SupportsDownload() bool {
	return p != nil && (p.native != nil || p.resolver != nil)
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

// GetDownloadInfo resolves audio in two tiers:
//
//  1. native — real Spotify audio (decrypted Ogg Vorbis) when the native
//     source is wired and authenticated. This is the authentic Spotify stream.
//  2. resolver — a YouTube Music delegate, matched by ISRC then
//     title+artist+duration, used when native is unavailable for this track
//     (not authenticated, DRM/region-blocked, or no Ogg Vorbis tier).
//
// The native tier is tried first; any native failure transparently falls back
// to the resolver so a download never just dies when an alternative exists.
func (p *SpotifyPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "track", trackID)
	}
	if p.native == nil && p.resolver == nil {
		return nil, platform.NewUnavailableError(platformName, "download", trackID)
	}

	// Tier 1: native Spotify audio.
	if p.native != nil {
		info, err := p.native.BuildDownloadInfo(ctx, trackID, quality)
		if err == nil {
			return info, nil
		}
		// Native failed; fall through to the resolver if one is available.
		if p.resolver == nil {
			return nil, err
		}
	}

	// Tier 2: YouTube Music delegate.
	track, err := p.client.GetTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	return resolveAudio(ctx, p.resolver, track, quality)
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
