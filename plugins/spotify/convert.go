package spotify

import (
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// convertArtists maps Spotify artists to the unified type.
func convertArtists(in []spotifyArtist) []platform.Artist {
	out := make([]platform.Artist, 0, len(in))
	for _, a := range in {
		out = append(out, platform.Artist{
			ID:       a.ID,
			Platform: platformName,
			Name:     a.Name,
			URL:      a.ExternalURLs["spotify"],
		})
	}
	return out
}

// convertTrack maps a Spotify track to the unified Track, carrying the ISRC,
// which is the high-precision key used to find the same recording on the
// delegated audio platform (YouTube Music).
func convertTrack(t spotifyTrack) platform.Track {
	track := platform.Track{
		ID:          t.ID,
		Platform:    platformName,
		Title:       t.Name,
		Artists:     convertArtists(t.Artists),
		Duration:    time.Duration(t.DurationMs) * time.Millisecond,
		ISRC:        strings.ToUpper(strings.TrimSpace(t.ExternalIDs.ISRC)),
		TrackNumber: t.TrackNumber,
		DiscNumber:  t.DiscNumber,
		URL:         t.ExternalURLs["spotify"],
	}
	if strings.TrimSpace(t.Album.ID) != "" || strings.TrimSpace(t.Album.Name) != "" {
		album := convertAlbum(t.Album)
		track.Album = &album
		track.CoverURL = album.CoverURL
		track.Year = album.Year
	}
	return track
}

// convertAlbum maps a Spotify album to the unified Album.
func convertAlbum(a spotifyAlbum) platform.Album {
	return platform.Album{
		ID:          a.ID,
		Platform:    platformName,
		Title:       a.Name,
		Artists:     convertArtists(a.Artists),
		CoverURL:    firstImage(a.Images),
		TrackCount:  a.TotalTracks,
		URL:         a.ExternalURLs["spotify"],
		Year:        parseReleaseYear(a.ReleaseDate),
		ReleaseDate: parseReleaseDate(a.ReleaseDate, a.ReleaseDatePrecision),
	}
}

// firstImage returns the URL of the first (largest) image, or "".
func firstImage(images []spotifyImage) string {
	if len(images) == 0 {
		return ""
	}
	return images[0].URL
}

// parseReleaseYear extracts the leading year from a Spotify release_date
// ("2021", "2021-03", or "2021-03-15").
func parseReleaseYear(date string) int {
	date = strings.TrimSpace(date)
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return y
}

// parseReleaseDate parses a Spotify release_date according to its precision,
// returning nil when only a year/month is known (so callers don't show a
// misleadingly precise day).
func parseReleaseDate(date, precision string) *time.Time {
	date = strings.TrimSpace(date)
	if precision != "day" || len(date) < 10 {
		return nil
	}
	t, err := time.Parse("2006-01-02", date[:10])
	if err != nil {
		return nil
	}
	return &t
}
