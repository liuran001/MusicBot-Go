package spotify

// Spotify Web API response types. We bind only the fields the bot needs. The
// Web API serves metadata + search only (no full audio); the actual audio is
// fetched and decrypted via the embedded librespot path in the native/
// subpackage, so there is deliberately no streaming/format type here.

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type spotifyImage struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type spotifyArtist struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	ExternalURLs map[string]string `json:"external_urls"`
	Images       []spotifyImage    `json:"images"`
}

type spotifyAlbum struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	Artists              []spotifyArtist   `json:"artists"`
	Images               []spotifyImage    `json:"images"`
	ReleaseDate          string            `json:"release_date"`
	ReleaseDatePrecision string            `json:"release_date_precision"`
	TotalTracks          int               `json:"total_tracks"`
	ExternalURLs         map[string]string `json:"external_urls"`
	Tracks               struct {
		Items []spotifyTrack `json:"items"`
	} `json:"tracks"`
}

type spotifyTrack struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Artists      []spotifyArtist   `json:"artists"`
	Album        spotifyAlbum      `json:"album"`
	DurationMs   int               `json:"duration_ms"`
	TrackNumber  int               `json:"track_number"`
	DiscNumber   int               `json:"disc_number"`
	ExternalURLs map[string]string `json:"external_urls"`
	ExternalIDs  struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
}

type spotifySearchResponse struct {
	Tracks struct {
		Items []spotifyTrack `json:"items"`
	} `json:"tracks"`
}

type spotifyPlaylist struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Images       []spotifyImage    `json:"images"`
	ExternalURLs map[string]string `json:"external_urls"`
	Owner        struct {
		DisplayName string `json:"display_name"`
	} `json:"owner"`
	Tracks struct {
		Total int `json:"total"`
		Items []struct {
			Track spotifyTrack `json:"track"`
		} `json:"items"`
	} `json:"tracks"`
}
