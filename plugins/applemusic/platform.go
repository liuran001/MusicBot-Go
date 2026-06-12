package applemusic

import (
	"context"
	"io"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type AppleMusicPlatform struct {
	client *Client
}

func NewPlatform(client *Client) *AppleMusicPlatform {
	return &AppleMusicPlatform{client: client}
}

func (p *AppleMusicPlatform) Name() string { return "applemusic" }

func (p *AppleMusicPlatform) SupportsDownload() bool { return true }

func (p *AppleMusicPlatform) SupportsSearch() bool { return true }

func (p *AppleMusicPlatform) SupportsLyrics() bool { return true }

func (p *AppleMusicPlatform) SupportsRecognition() bool { return false }

func (p *AppleMusicPlatform) Capabilities() platform.Capabilities {
	// Hi-Res / lossless is only available through the external FairPlay wrapper;
	// the built-in native path tops out at AAC 256k (Apple blocks Widevine for
	// lossless). Report HiRes only when a wrapper is configured.
	hiRes := p != nil && p.client != nil && strings.TrimSpace(p.client.wrapperHost) != ""
	return platform.Capabilities{Download: true, Search: true, Lyrics: true, Recognition: false, HiRes: hiRes}
}

func (p *AppleMusicPlatform) Metadata() platform.Meta {
	return platform.Meta{
		Name:          "applemusic",
		DisplayName:   "Apple Music",
		Emoji:         "🍎",
		Aliases:       []string{"applemusic", "apple", "am", "apple-music", "apple_music", "苹果音乐"},
		AllowGroupURL: true,
	}
}

func (p *AppleMusicPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError("applemusic", "track", trackID)
	}
	return p.client.GetDownloadInfo(ctx, trackID, quality)
}

func (p *AppleMusicPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError("applemusic", "search", query)
	}
	return p.client.Search(ctx, query, limit)
}

func (p *AppleMusicPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError("applemusic", "lyrics", trackID)
	}
	plain, err := p.client.GetLyrics(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(plain) == "" {
		return nil, platform.NewUnavailableError("applemusic", "lyrics", trackID)
	}
	return &platform.Lyrics{Plain: plain, Timestamped: platform.ParseLRCTimestampedLines(plain)}, nil
}

func (p *AppleMusicPlatform) RecognizeAudio(_ context.Context, _ io.Reader) (*platform.Track, error) {
	return nil, platform.NewUnsupportedError("applemusic", "audio recognition")
}

func (p *AppleMusicPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError("applemusic", "track", trackID)
	}
	track, err := p.client.GetTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if track == nil {
		return nil, platform.NewNotFoundError("applemusic", "track", trackID)
	}
	return track, nil
}

func (p *AppleMusicPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError("applemusic", "artist", artistID)
	}
	artist, err := p.client.GetArtist(ctx, artistID)
	if err != nil {
		return nil, err
	}
	if artist == nil {
		return nil, platform.NewNotFoundError("applemusic", "artist", artistID)
	}
	return artist, nil
}

func (p *AppleMusicPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError("applemusic", "album", albumID)
	}
	album, _, err := p.client.GetAlbum(ctx, albumID)
	if err != nil {
		return nil, err
	}
	if album == nil {
		return nil, platform.NewNotFoundError("applemusic", "album", albumID)
	}
	return album, nil
}

func (p *AppleMusicPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	isAlbum, rawID := parseCollectionID(playlistID)
	if isAlbum {
		album, tracks, err := p.client.GetAlbum(ctx, rawID)
		if err != nil {
			return nil, err
		}
		if album == nil {
			return nil, platform.NewNotFoundError("applemusic", "album", rawID)
		}
		return &platform.Playlist{
			ID:         playlistID,
			Platform:   "applemusic",
			Title:      album.Title,
			CoverURL:   album.CoverURL,
			Creator:    joinArtistNames(album.Artists),
			TrackCount: max(album.TrackCount, len(tracks)),
			Tracks:     tracks,
			URL:        album.URL,
		}, nil
	}
	if p == nil || p.client == nil {
		return nil, platform.NewUnavailableError("applemusic", "playlist", playlistID)
	}
	return p.client.GetPlaylist(ctx, playlistID)
}

func joinArtistNames(artists []platform.Artist) string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		if strings.TrimSpace(artist.Name) == "" {
			continue
		}
		names = append(names, artist.Name)
	}
	return strings.Join(names, "/")
}

func (p *AppleMusicPlatform) MatchURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchURL(rawURL)
}

func (p *AppleMusicPlatform) MatchPlaylistURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchPlaylistURL(rawURL)
}

func (p *AppleMusicPlatform) MatchArtistURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchArtistURL(rawURL)
}

func (p *AppleMusicPlatform) MatchText(text string) (string, bool) {
	return NewTextMatcher().MatchText(text)
}

func (p *AppleMusicPlatform) ShortLinkHosts() []string {
	return []string{"itunes.apple.com"}
}
