package spotify

import (
	"context"
	"io"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// SpotifyPlatform implements platform.Platform. Spotify is a METADATA + SEARCH
// source; audio is delegated to an audioResolver (YouTube Music) because the
// Spotify API does not serve full audio. When no resolver is configured,
// downloads degrade to ErrUnavailable while search/metadata keep working.
type SpotifyPlatform struct {
	client   *Client
	resolver audioResolver
}

// NewPlatform builds a Spotify platform. resolver may be nil (downloads then
// return ErrUnavailable, metadata/search still work).
func NewPlatform(client *Client, resolver audioResolver) *SpotifyPlatform {
	return &SpotifyPlatform{client: client, resolver: resolver}
}

func (p *SpotifyPlatform) Name() string { return platformName }

// SupportsDownload reports true only when an audio resolver is wired, so the
// router/UI won't offer downloads that can't be fulfilled.
func (p *SpotifyPlatform) SupportsDownload() bool { return p != nil && p.resolver != nil }
func (p *SpotifyPlatform) SupportsSearch() bool   { return true }
func (p *SpotifyPlatform) SupportsLyrics() bool   { return false }
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

// GetDownloadInfo resolves audio by matching this Spotify track to a YouTube
// Music recording (ISRC first, then title+artist+duration) and returning that
// platform's stream info.
func (p *SpotifyPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError(platformName, "track", trackID)
	}
	if p.resolver == nil {
		return nil, platform.NewUnavailableError(platformName, "download", trackID)
	}
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
		album, err := p.client.GetAlbum(ctx, id)
		if err != nil {
			return nil, err
		}
		return albumAsPlaylist(album), nil
	}
	return p.client.GetPlaylist(ctx, id)
}

// albumAsPlaylist adapts an Album into a Playlist view for the collection UI.
func albumAsPlaylist(album *platform.Album) *platform.Playlist {
	if album == nil {
		return nil
	}
	creator := ""
	if len(album.Artists) > 0 {
		creator = album.Artists[0].Name
	}
	return &platform.Playlist{
		ID:         "album:" + album.ID,
		Platform:   platformName,
		Title:      album.Title,
		CoverURL:   album.CoverURL,
		Creator:    creator,
		TrackCount: album.TrackCount,
		URL:        album.URL,
	}
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
