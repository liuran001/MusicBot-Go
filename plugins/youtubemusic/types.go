package youtubemusic

// InnerTube is YouTube's internal API. We speak it directly (the same protocol
// the youtube.com / music.youtube.com web clients use) rather than depending on
// a third-party SDK, matching how every other platform plugin in this project
// implements its own HTTP client.
//
// Two client "contexts" are used:
//   - WEB_REMIX  : music.youtube.com search / metadata / lyrics
//   - IOS        : the /player call for DOWNLOAD, because the iOS client context
//     returns adaptiveFormats with DIRECT googlevideo URLs (no signatureCipher),
//     sidestepping the web client's n-sig / cipher problem entirely.
const (
	innerTubeBaseMusic = "https://music.youtube.com/youtubei/v1"
	innerTubeBaseVideo = "https://www.youtube.com/youtubei/v1"

	// webRemixKey is the long-standing public InnerTube API key for the
	// music.youtube.com web client. It is not a secret (it ships in the page).
	webRemixKey = "AIzaSyC9XL3ZjWddXya6X74dJoCTL-WEYFDNX30"

	webRemixClientName    = "WEB_REMIX"
	webRemixClientVersion = "1.20240101.01.00"

	iosClientName    = "IOS"
	iosClientVersion = "19.45.4"
	iosUserAgent     = "com.google.ios.youtube/19.45.4 (iPhone16,2; U; CPU iOS 18_1 like Mac OS X)"

	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// innertubeContext is the "context" object every InnerTube request carries.
type innertubeContext struct {
	Client clientInfo `json:"client"`
}

type clientInfo struct {
	ClientName        string `json:"clientName"`
	ClientVersion     string `json:"clientVersion"`
	Hl                string `json:"hl,omitempty"`
	Gl                string `json:"gl,omitempty"`
	DeviceModel       string `json:"deviceModel,omitempty"`
	OsName            string `json:"osName,omitempty"`
	OsVersion         string `json:"osVersion,omitempty"`
	AndroidSDKVersion int    `json:"androidSdkVersion,omitempty"`
}

// searchRequest / playerRequest / nextRequest / browseRequest are the request
// envelopes for the four endpoints we use.
type searchRequest struct {
	Context innertubeContext `json:"context"`
	Query   string           `json:"query"`
	Params  string           `json:"params,omitempty"`
}

type playerRequest struct {
	Context        innertubeContext `json:"context"`
	VideoID        string           `json:"videoId"`
	ContentCheckOK bool             `json:"contentCheckOk,omitempty"`
	RacyOK         bool             `json:"racyCheckOk,omitempty"`
}

type nextRequest struct {
	Context innertubeContext `json:"context"`
	VideoID string           `json:"videoId"`
}

type browseRequest struct {
	Context  innertubeContext `json:"context"`
	BrowseID string           `json:"browseId"`
}

// --- player response (download) ---

type playerResponse struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	StreamingData struct {
		ExpiresInSeconds string         `json:"expiresInSeconds"`
		Formats          []streamFormat `json:"formats"`
		AdaptiveFormats  []streamFormat `json:"adaptiveFormats"`
	} `json:"streamingData"`
	VideoDetails struct {
		VideoID       string `json:"videoId"`
		Title         string `json:"title"`
		LengthSeconds string `json:"lengthSeconds"`
		Author        string `json:"author"`
		Thumbnail     struct {
			Thumbnails []thumbnail `json:"thumbnails"`
		} `json:"thumbnail"`
	} `json:"videoDetails"`
}

type streamFormat struct {
	Itag             int    `json:"itag"`
	URL              string `json:"url"`
	MimeType         string `json:"mimeType"`
	Bitrate          int    `json:"bitrate"`
	AverageBitrate   int    `json:"averageBitrate"`
	ContentLength    string `json:"contentLength"`
	AudioQuality     string `json:"audioQuality"`
	AudioSampleRate  string `json:"audioSampleRate"`
	AudioChannels    int    `json:"audioChannels"`
	SignatureCipher  string `json:"signatureCipher"`
	Quality          string `json:"quality"`
}

type thumbnail struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}
