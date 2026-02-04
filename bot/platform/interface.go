package platform

import (
	"context"
	"io"
)

// Platform defines the interface that all music platform implementations must satisfy.
// This interface uses a capability-based design where platforms can indicate which
// features they support through the Supports* methods, allowing graceful degradation
// when certain features are unavailable.
//
// Platform implementations should be safe for concurrent use by multiple goroutines.
type Platform interface {
	// Name returns the platform identifier (e.g., "netease", "spotify", "youtube-music").
	// This name should be lowercase and URL-safe.
	Name() string

	// SupportsDownload indicates whether this platform supports downloading audio files.
	SupportsDownload() bool

	// SupportsSearch indicates whether this platform supports searching for tracks.
	SupportsSearch() bool

	// SupportsLyrics indicates whether this platform supports fetching lyrics.
	SupportsLyrics() bool

	// SupportsRecognition indicates whether this platform supports audio recognition (听歌识曲).
	// This feature allows identifying a track from an audio sample.
	SupportsRecognition() bool

	Capabilities() Capabilities

	GetDownloadInfo(ctx context.Context, trackID string, quality Quality) (*DownloadInfo, error)

	// Search searches for tracks matching the given query string.
	// The limit parameter controls the maximum number of results to return.
	//
	// Returns ErrUnsupported if search is not supported by this platform.
	Search(ctx context.Context, query string, limit int) ([]Track, error)

	// GetLyrics retrieves the lyrics for the given track ID.
	//
	// Returns ErrNotFound if the track doesn't exist, ErrUnavailable if lyrics are not
	// available for this track, or ErrUnsupported if lyrics are not supported by this platform.
	GetLyrics(ctx context.Context, trackID string) (*Lyrics, error)

	// RecognizeAudio attempts to identify a track from the provided audio data.
	// The audioData should be the raw audio file content (any common format is acceptable).
	//
	// Returns ErrUnsupported if audio recognition is not supported by this platform.
	RecognizeAudio(ctx context.Context, audioData io.Reader) (*Track, error)

	// GetTrack retrieves detailed information about a track by its ID.
	//
	// Returns ErrNotFound if the track doesn't exist.
	GetTrack(ctx context.Context, trackID string) (*Track, error)

	// GetArtist retrieves detailed information about an artist by their ID.
	//
	// Returns ErrNotFound if the artist doesn't exist or ErrUnsupported if not supported.
	GetArtist(ctx context.Context, artistID string) (*Artist, error)

	// GetAlbum retrieves detailed information about an album by its ID.
	//
	// Returns ErrNotFound if the album doesn't exist or ErrUnsupported if not supported.
	GetAlbum(ctx context.Context, albumID string) (*Album, error)

	// GetPlaylist retrieves detailed information about a playlist by its ID.
	//
	// Returns ErrNotFound if the playlist doesn't exist or ErrUnsupported if not supported.
	GetPlaylist(ctx context.Context, playlistID string) (*Playlist, error)
}

// URLMatcher defines the interface for platforms that support URL matching.
// This allows extracting track IDs from platform-specific URLs.
//
// For example, a NetEase implementation might match:
//   - https://music.163.com/#/song?id=1234567
//   - https://y.music.163.com/m/song?id=1234567
//
// Implementations should be safe for concurrent use by multiple goroutines.
type URLMatcher interface {
	// MatchURL attempts to extract a track ID from a platform-specific URL.
	// Returns the track ID and true if the URL matches this platform's format,
	// or an empty string and false if the URL is not recognized.
	MatchURL(url string) (trackID string, matched bool)
}

// PlaylistURLMatcher defines the interface for platforms that support playlist URL matching.
// This allows extracting playlist IDs from platform-specific URLs.
//
// Implementations should be safe for concurrent use by multiple goroutines.
type PlaylistURLMatcher interface {
	// MatchPlaylistURL attempts to extract a playlist ID from a platform-specific URL.
	// Returns the playlist ID and true if the URL matches this platform's playlist format,
	// or an empty string and false if the URL is not recognized.
	MatchPlaylistURL(url string) (playlistID string, matched bool)
}

// ShortLinkProvider defines the interface for platforms that declare short-link hosts
// which should be resolved before URL matching.
type ShortLinkProvider interface {
	ShortLinkHosts() []string
}

// TextMatcher defines the interface for platforms that support parsing track IDs
// from arbitrary text input (e.g., short links or plain IDs).
type TextMatcher interface {
	// MatchText attempts to extract a track ID from arbitrary text input.
	// Returns the track ID and true if the text matches this platform's format.
	MatchText(text string) (trackID string, matched bool)
}

// Manager provides a registry for multiple platform implementations.
// This allows the bot to work with multiple music platforms simultaneously.
type Manager interface {
	// Register adds a platform implementation to the manager.
	// If a platform with the same name already exists, it will be replaced.
	Register(platform Platform)

	// Get retrieves a platform by name.
	// Returns nil if no platform with that name is registered.
	Get(name string) Platform

	// List returns all registered platform names.
	List() []string

	// MatchURL attempts to match a URL against all registered platforms.
	// Returns the platform name, track ID, and true if a match is found.
	// Returns empty strings and false if no platform matches the URL.
	MatchURL(url string) (platformName, trackID string, matched bool)

	// MatchText attempts to match arbitrary text against all registered platforms.
	// Returns the platform name, track ID, and true if a match is found.
	// Returns empty strings and false if no platform matches the text.
	MatchText(text string) (platformName, trackID string, matched bool)

	// ResolveAlias resolves a platform alias to its canonical platform name.
	ResolveAlias(alias string) (platformName string, matched bool)

	// Meta returns metadata for a platform name.
	Meta(name string) (Meta, bool)

	// ListMeta returns metadata for all registered platforms.
	ListMeta() []Meta
}
