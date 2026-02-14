package platform

import "time"

// Track represents a music track from any platform.
// This is the unified representation that maps from platform-specific types.
type Track struct {
	// ID is the platform-specific track identifier.
	ID string `json:"id"`

	// Platform is the source platform name (e.g., "netease", "spotify").
	Platform string `json:"platform"`

	// Title is the track name.
	Title string `json:"title"`

	// Artists is the list of artists for this track.
	Artists []Artist `json:"artists"`

	// Album is the album this track belongs to (may be nil for singles).
	Album *Album `json:"album,omitempty"`

	// Duration is the track length.
	Duration time.Duration `json:"duration"`

	// CoverURL is the URL to the track's cover art.
	CoverURL string `json:"cover_url,omitempty"`

	// URL is the direct URL to the track (if available without download).
	URL string `json:"url,omitempty"`

	// ISRC is the International Standard Recording Code (if available).
	ISRC string `json:"isrc,omitempty"`

	// Year is the release year (if available).
	Year int `json:"year,omitempty"`

	// TrackNumber is the track number within album (if available).
	TrackNumber int `json:"track_number,omitempty"`

	// DiscNumber is the disc number within multi-disc album (if available).
	DiscNumber int `json:"disc_number,omitempty"`
}

// Artist represents a music artist from any platform.
type Artist struct {
	// ID is the platform-specific artist identifier.
	ID string `json:"id"`

	// Platform is the source platform name.
	Platform string `json:"platform"`

	// Name is the artist name.
	Name string `json:"name"`

	// AvatarURL is the URL to the artist's avatar or profile picture.
	AvatarURL string `json:"avatar_url,omitempty"`

	// URL is the direct URL to the artist's page (if available).
	URL string `json:"url,omitempty"`
}

// Album represents a music album from any platform.
type Album struct {
	// ID is the platform-specific album identifier.
	ID string `json:"id"`

	// Platform is the source platform name.
	Platform string `json:"platform"`

	// Title is the album name.
	Title string `json:"title"`

	// Artists is the list of artists for this album.
	Artists []Artist `json:"artists"`

	// CoverURL is the URL to the album's cover art.
	CoverURL string `json:"cover_url,omitempty"`

	// ReleaseDate is the album release date (if available).
	ReleaseDate *time.Time `json:"release_date,omitempty"`

	// TrackCount is the number of tracks in the album.
	TrackCount int `json:"track_count,omitempty"`

	// URL is the direct URL to the album (if available).
	URL string `json:"url,omitempty"`

	// Year is the album release year (if available).
	Year int `json:"year,omitempty"`
}

// Playlist represents a music playlist from any platform.
type Playlist struct {
	// ID is the platform-specific playlist identifier.
	ID string `json:"id"`

	// Platform is the source platform name.
	Platform string `json:"platform"`

	// Title is the playlist name.
	Title string `json:"title"`

	// Description is the playlist description.
	Description string `json:"description,omitempty"`

	// CoverURL is the URL to the playlist's cover art.
	CoverURL string `json:"cover_url,omitempty"`

	// Creator is the user who created this playlist.
	Creator string `json:"creator,omitempty"`

	// TrackCount is the number of tracks in the playlist.
	TrackCount int `json:"track_count,omitempty"`

	// Tracks is the list of tracks in the playlist (may be empty if not loaded).
	Tracks []Track `json:"tracks,omitempty"`

	// URL is the direct URL to the playlist (if available).
	URL string `json:"url,omitempty"`
}

// Lyrics represents song lyrics with optional timestamped lines.
type Lyrics struct {
	// Plain is the plain text lyrics without timestamps.
	Plain string `json:"plain"`

	// Timestamped contains synchronized lyrics with timing information.
	Timestamped []LyricLine `json:"timestamped,omitempty"`

	// Translation contains translated lyrics (if available).
	Translation string `json:"translation,omitempty"`
}

// LyricLine represents a single line of synchronized lyrics.
type LyricLine struct {
	// Time is the timestamp when this line should be displayed.
	Time time.Duration `json:"time"`

	// Text is the lyric text for this line.
	Text string `json:"text"`
}

// TrackMetadata contains technical metadata about a downloaded track.
type TrackMetadata struct {
	// Format is the audio file format (e.g., "mp3", "flac", "m4a").
	Format string `json:"format"`

	// Bitrate is the audio bitrate in kbps.
	Bitrate int `json:"bitrate"`

	// SampleRate is the audio sample rate in Hz.
	SampleRate int `json:"sample_rate,omitempty"`

	// Channels is the number of audio channels (1 = mono, 2 = stereo).
	Channels int `json:"channels,omitempty"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`

	// MD5 is the MD5 checksum of the audio file (if provided by platform).
	MD5 string `json:"md5,omitempty"`

	// Quality is the quality level of this track.
	Quality Quality `json:"quality"`
}

type DownloadInfo struct {
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	Size      int64             `json:"size"`
	Format    string            `json:"format"`
	Bitrate   int               `json:"bitrate"`
	MD5       string            `json:"md5,omitempty"`
	Quality   Quality           `json:"quality"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
}

type Capabilities struct {
	Download    bool `json:"download"`
	Search      bool `json:"search"`
	Lyrics      bool `json:"lyrics"`
	Recognition bool `json:"recognition"`
	HiRes       bool `json:"hi_res"`
}
