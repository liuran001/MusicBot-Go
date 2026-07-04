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

// convertTrack maps a Spotify Web API track to the unified Track.
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

func convertPathfinderTrack(t pathfinderTrack) platform.Track {
	pathfinderArtists := append([]pathfinderArtist(nil), t.FirstArtist.Items...)
	pathfinderArtists = append(pathfinderArtists, t.OtherArtists.Items...)
	if len(pathfinderArtists) == 0 {
		pathfinderArtists = append(pathfinderArtists, t.Artists.Items...)
	}
	artists := make([]platform.Artist, 0, len(pathfinderArtists))
	for _, artist := range pathfinderArtists {
		artists = append(artists, convertPathfinderArtist(artist))
	}

	trackID := pathfinderID(t.ID, t.URI)
	trackURL := strings.TrimSpace(t.SharingInfo.ShareURL)
	if trackURL == "" {
		trackURL = "https://open.spotify.com/track/" + trackID
	}
	track := platform.Track{
		ID:          trackID,
		Platform:    platformName,
		Title:       t.Name,
		Artists:     artists,
		Duration:    time.Duration(t.Duration.TotalMilliseconds) * time.Millisecond,
		TrackNumber: t.TrackNumber,
		URL:         trackURL,
	}

	albumID := pathfinderID(t.Album.ID, t.Album.URI)
	if albumID != "" || strings.TrimSpace(t.Album.Name) != "" {
		releaseDate := strings.TrimSpace(t.Album.Date.ISOString)
		precision := strings.ToLower(strings.TrimSpace(t.Album.Date.Precision))
		albumURL := strings.TrimSpace(t.Album.SharingInfo.ShareURL)
		if albumURL == "" && albumID != "" {
			albumURL = "https://open.spotify.com/album/" + albumID
		}
		album := platform.Album{
			ID:          albumID,
			Platform:    platformName,
			Title:       t.Album.Name,
			Artists:     artists,
			CoverURL:    largestImage(t.Album.CoverArt.Sources),
			TrackCount:  t.Album.Tracks.TotalCount,
			URL:         albumURL,
			Year:        t.Album.Date.Year,
			ReleaseDate: parseReleaseDate(releaseDate, precision),
		}
		track.Album = &album
		track.CoverURL = album.CoverURL
		track.Year = album.Year
	}
	return track
}

func convertPathfinderArtist(a pathfinderArtist) platform.Artist {
	id := pathfinderID(a.ID, a.URI)
	url := ""
	if id != "" {
		url = "https://open.spotify.com/artist/" + id
	}
	return platform.Artist{
		ID:       id,
		Platform: platformName,
		Name:     a.Profile.Name,
		URL:      url,
	}
}

func convertPathfinderArtistUnion(a pathfinderArtistUnion) platform.Artist {
	id := pathfinderID(a.ID, a.URI)
	name := strings.TrimSpace(a.Profile.Name)
	if name == "" {
		name = strings.TrimSpace(a.Name)
	}
	url := ""
	if id != "" {
		url = "https://open.spotify.com/artist/" + id
	}
	return platform.Artist{
		ID:        id,
		Platform:  platformName,
		Name:      name,
		AvatarURL: largestImage(a.Visuals.AvatarImage.Sources),
		URL:       url,
	}
}

func convertPathfinderAlbum(a pathfinderAlbum) platform.Album {
	albumID := pathfinderID(a.ID, a.URI)
	artists := make([]platform.Artist, 0, len(a.Artists.Items))
	for _, artist := range a.Artists.Items {
		artists = append(artists, convertPathfinderArtist(artist))
	}
	return platform.Album{
		ID:          albumID,
		Platform:    platformName,
		Title:       a.Name,
		Artists:     artists,
		CoverURL:    largestImage(a.CoverArt.Sources),
		TrackCount:  firstPositiveInt(a.TracksV2.TotalCount, len(a.TracksV2.Items)),
		URL:         spotifyOpenURL("album", albumID),
		Year:        a.Date.Year,
		ReleaseDate: pathfinderDate(a.Date.ISOString, a.Date.Precision, a.Date.Year, a.Date.Month, a.Date.Day),
	}
}

func convertPathfinderAlbumAsPlaylist(a pathfinderAlbum) *platform.Playlist {
	album := convertPathfinderAlbum(a)
	pl := &platform.Playlist{
		ID:         "album:" + album.ID,
		Platform:   platformName,
		Title:      album.Title,
		CoverURL:   album.CoverURL,
		TrackCount: album.TrackCount,
		URL:        album.URL,
	}
	if len(album.Artists) > 0 {
		pl.Creator = album.Artists[0].Name
	}
	for _, item := range a.TracksV2.Items {
		track := convertPathfinderTrack(item.Track)
		if strings.TrimSpace(track.ID) == "" {
			continue
		}
		if track.Album == nil {
			track.Album = &album
		}
		if track.CoverURL == "" {
			track.CoverURL = album.CoverURL
		}
		if track.Year == 0 {
			track.Year = album.Year
		}
		pl.Tracks = append(pl.Tracks, track)
	}
	return pl
}

func convertPathfinderPlaylist(p pathfinderPlaylist) *platform.Playlist {
	playlistID := pathfinderID(p.ID, p.URI)
	creator := firstNonEmptyString(p.OwnerV2.Data.DisplayName, p.OwnerV2.Data.Name, p.OwnerV2.Data.Username)
	pl := &platform.Playlist{
		ID:          playlistID,
		Platform:    platformName,
		Title:       p.Name,
		Description: p.Description,
		CoverURL:    firstPathfinderPlaylistImage(p),
		Creator:     creator,
		TrackCount:  firstPositiveInt(p.Content.TotalCount, len(p.Content.Items)),
		URL:         spotifyOpenURL("playlist", playlistID),
	}
	for _, item := range p.Content.Items {
		track := convertPathfinderTrack(item.ItemV2.Data)
		if strings.TrimSpace(track.ID) == "" {
			continue
		}
		pl.Tracks = append(pl.Tracks, track)
	}
	return pl
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

func largestImage(images []spotifyImage) string {
	var largest spotifyImage
	for _, image := range images {
		if image.Width*image.Height > largest.Width*largest.Height {
			largest = image
		}
	}
	return largest.URL
}

func firstPathfinderPlaylistImage(p pathfinderPlaylist) string {
	for _, item := range p.Images.Items {
		if url := largestImage(item.Sources); url != "" {
			return url
		}
	}
	return ""
}

func pathfinderID(id, uri string) string {
	id = strings.TrimSpace(id)
	if id != "" {
		return id
	}
	if parsedID, _, ok := parseSpotifyURI(uri); ok {
		return parsedID
	}
	return ""
}

func spotifyOpenURL(kind, id string) string {
	id = strings.TrimSpace(id)
	if kind == "" || id == "" {
		return ""
	}
	return "https://open.spotify.com/" + kind + "/" + id
}

func pathfinderDate(isoString, precision string, year, month, day int) *time.Time {
	if t := parseReleaseDate(strings.TrimSpace(isoString), strings.ToLower(strings.TrimSpace(precision))); t != nil {
		return t
	}
	if year <= 0 {
		return nil
	}
	if month <= 0 {
		month = 1
	}
	if day <= 0 {
		day = 1
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return &t
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
