package spotify

// Spotify metadata response types. Full audio is fetched and decrypted by the
// native Widevine subpackage, so there is no streaming/format type here.

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

type pathfinderArtist struct {
	ID      string `json:"id"`
	URI     string `json:"uri"`
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
}

type pathfinderTrack struct {
	ID       string `json:"id"`
	URI      string `json:"uri"`
	Name     string `json:"name"`
	Duration struct {
		TotalMilliseconds int `json:"totalMilliseconds"`
	} `json:"duration"`
	TrackNumber int `json:"trackNumber"`
	SharingInfo struct {
		ShareURL string `json:"shareUrl"`
	} `json:"sharingInfo"`
	Artists struct {
		Items []pathfinderArtist `json:"items"`
	} `json:"artists"`
	FirstArtist struct {
		Items []pathfinderArtist `json:"items"`
	} `json:"firstArtist"`
	OtherArtists struct {
		Items []pathfinderArtist `json:"items"`
	} `json:"otherArtists"`
	Album struct {
		ID   string `json:"id"`
		URI  string `json:"uri"`
		Name string `json:"name"`
		Date struct {
			ISOString string `json:"isoString"`
			Precision string `json:"precision"`
			Year      int    `json:"year"`
		} `json:"date"`
		SharingInfo struct {
			ShareURL string `json:"shareUrl"`
		} `json:"sharingInfo"`
		Tracks struct {
			TotalCount int `json:"totalCount"`
		} `json:"tracks"`
		CoverArt struct {
			Sources []spotifyImage `json:"sources"`
		} `json:"coverArt"`
	} `json:"albumOfTrack"`
}

type pathfinderTrackResponse struct {
	Data struct {
		Track pathfinderTrack `json:"trackUnion"`
	} `json:"data"`
	Errors []struct{} `json:"errors"`
}

type pathfinderSearchResponse struct {
	Data struct {
		SearchV2 struct {
			TracksV2 struct {
				Items []struct {
					Item struct {
						Data pathfinderTrack `json:"data"`
					} `json:"item"`
				} `json:"items"`
				TotalCount int `json:"totalCount"`
			} `json:"tracksV2"`
		} `json:"searchV2"`
	} `json:"data"`
	Errors []struct{} `json:"errors"`
}
