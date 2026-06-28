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

	// ANDROID_VR is the client yt-dlp uses by default for downloads. Crucially,
	// its googlevideo stream URLs are NOT behind the PO Token wall that the IOS
	// client's URLs are: on a YouTube-flagged IP the IOS stream only serves the
	// first ~1 MiB (offset!=0 -> HTTP 403), whereas the ANDROID_VR stream serves
	// arbitrary byte ranges (verified 206 at any offset). It also returns direct,
	// un-ciphered URLs (REQUIRE_JS_PLAYER is false for this client), so no
	// signature/n-sig decipher is needed. Source of truth = yt-dlp master
	// yt_dlp/extractor/youtube/_base.py INNERTUBE_CLIENTS['android_vr']. When this
	// goes stale (player 400/“Sign in to confirm you’re not a bot”), re-copy the
	// clientVersion / userAgent / device fields from yt-dlp.
	androidVRClientName    = "ANDROID_VR"
	androidVRClientVersion = "1.62.27"
	androidVRUserAgent     = "com.google.android.apps.youtube.vr.oculus/1.62.27 (Linux; U; Android 12L; eureka-user Build/SQ3A.220605.009.A1) gzip"

	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

	// googleVideoMaxChunk is the largest single Range request googlevideo will
	// serve for a non-PoToken stream URL before replying 403. Empirically the
	// per-IP cap sits just above 1 MiB, so we stay at exactly 1 MiB and require
	// the downloader to fetch the stream in bounded Range chunks no larger than
	// this. HEAD, plain GET, and open-ended ("bytes=0-") requests are all
	// rejected too — only bounded Range chunks within this cap return 206.
	googleVideoMaxChunk = 1024 * 1024
)

// innertubeContext is the "context" object every InnerTube request carries.
type innertubeContext struct {
	Client     clientInfo       `json:"client"`
	ThirdParty *thirdPartyInfo  `json:"thirdParty,omitempty"`
	User       *userInfo        `json:"user,omitempty"`
	Request    *requestInfo     `json:"request,omitempty"`
}

type thirdPartyInfo struct {
	EmbedURL string `json:"embedUrl,omitempty"`
}

type userInfo struct {
	LockedSafetyMode bool `json:"lockedSafetyMode"`
}

type requestInfo struct {
	UseSSL                  bool     `json:"useSsl"`
	InternalExperimentFlags []string `json:"internalExperimentFlags"`
	ConsistencyTokenJars    []string `json:"consistencyTokenJars"`
}

type clientInfo struct {
	ClientName        string `json:"clientName"`
	ClientVersion     string `json:"clientVersion"`
	Hl                string `json:"hl,omitempty"`
	Gl                string `json:"gl,omitempty"`
	DeviceMake        string `json:"deviceMake,omitempty"`
	DeviceModel       string `json:"deviceModel,omitempty"`
	OsName            string `json:"osName,omitempty"`
	OsVersion         string `json:"osVersion,omitempty"`
	AndroidSDKVersion int    `json:"androidSdkVersion,omitempty"`
	// UserAgent is carried inside the client context for the ANDROID_VR client,
	// matching yt-dlp. Omitted for clients that don't set it.
	UserAgent       string `json:"userAgent,omitempty"`
	UtcOffsetMinute *int   `json:"utcOffsetMinutes,omitempty"`
	// VisitorData is the harvested Visitor ID, echoed inside the client context
	// (in addition to the X-Goog-Visitor-Id header) for ANDROID_VR /player.
	VisitorData string `json:"visitorData,omitempty"`
}

// searchRequest / playerRequest / nextRequest / browseRequest are the request
// envelopes for the four endpoints we use.
type searchRequest struct {
	Context innertubeContext `json:"context"`
	Query   string           `json:"query"`
	Params  string           `json:"params,omitempty"`
}

type playerRequest struct {
	Context         innertubeContext     `json:"context"`
	VideoID         string               `json:"videoId"`
	ContentCheckOK  bool                 `json:"contentCheckOk,omitempty"`
	RacyOK          bool                 `json:"racyCheckOk,omitempty"`
	PlaybackContext *playbackContextInfo `json:"playbackContext,omitempty"`
}

type playbackContextInfo struct {
	ContentPlaybackContext contentPlaybackContextInfo `json:"contentPlaybackContext"`
}

type contentPlaybackContextInfo struct {
	HTML5Preference    string `json:"html5Preference,omitempty"`
	SignatureTimestamp int    `json:"signatureTimestamp,omitempty"`
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
		// HlsManifestURL is a master m3u8 playlist. The iOS client context
		// returns it for music videos. HLS segment fetches are exempt from the
		// PO Token / non-zero-offset 403 wall that blocks the direct
		// adaptiveFormats URLs on flagged IPs, so it's our reliable download
		// path. See hls.go.
		HlsManifestURL string `json:"hlsManifestUrl"`
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
