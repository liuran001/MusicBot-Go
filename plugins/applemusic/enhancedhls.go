package applemusic

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// enhancedHLSVariant is one selectable stream from the enhancedHls master
// playlist (e.g. ALAC lossless, Dolby Atmos, or an AAC tier).
type enhancedHLSVariant struct {
	Codecs    string // e.g. "alac", "ec-3", "mp4a.40.2"
	AudioCh   string // AUDIO group id, e.g. "audio-alac-stereo-44100-24"
	Bandwidth int    // BANDWIDTH attr
	AvgBW     int    // AVERAGE-BANDWIDTH attr
	URI       string // media playlist URI (relative to master)

	// Parsed from the AUDIO group id where available.
	SampleRate int // e.g. 44100, 48000, 96000, 192000
	BitDepth   int // e.g. 16 or 24
}

// kind classifies a variant into a coarse audio family.
func (v enhancedHLSVariant) isALAC() bool  { return v.Codecs == "alac" }
func (v enhancedHLSVariant) isAtmos() bool { return v.Codecs == "ec-3" }
func (v enhancedHLSVariant) isAAC() bool   { return strings.HasPrefix(v.Codecs, "mp4a.40") }

var (
	reStreamInf = regexp.MustCompile(`^#EXT-X-STREAM-INF:(.*)$`)
	reAttrAudio = regexp.MustCompile(`AUDIO="([^"]*)"`)
	reAttrCodec = regexp.MustCompile(`CODECS="([^"]*)"`)
	reAttrBW    = regexp.MustCompile(`[^-]BANDWIDTH=(\d+)`)
	reAttrAvgBW = regexp.MustCompile(`AVERAGE-BANDWIDTH=(\d+)`)
)

// parseEnhancedHLSMaster parses the enhancedHls master playlist and returns all
// audio stream variants, sorted by average bandwidth descending (best first).
func parseEnhancedHLSMaster(content string) ([]enhancedHLSVariant, error) {
	var variants []enhancedHLSVariant
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		m := reStreamInf.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		attrs := m[1]
		var v enhancedHLSVariant
		if cm := reAttrCodec.FindStringSubmatch(attrs); cm != nil {
			v.Codecs = cm[1]
		}
		if am := reAttrAudio.FindStringSubmatch(attrs); am != nil {
			v.AudioCh = am[1]
		}
		// BANDWIDTH= but not AVERAGE-BANDWIDTH=; the regex requires a leading
		// non-dash char so it won't match the tail of AVERAGE-BANDWIDTH.
		if bm := reAttrBW.FindStringSubmatch("," + attrs); bm != nil {
			v.Bandwidth, _ = strconv.Atoi(bm[1])
		}
		if bm := reAttrAvgBW.FindStringSubmatch(attrs); bm != nil {
			v.AvgBW, _ = strconv.Atoi(bm[1])
		}
		// The URI is on the next non-comment, non-empty line.
		for j := i + 1; j < len(lines); j++ {
			u := strings.TrimSpace(lines[j])
			if u == "" || strings.HasPrefix(u, "#") {
				continue
			}
			v.URI = u
			i = j
			break
		}
		v.SampleRate, v.BitDepth = parseALACGroupDetails(v.AudioCh)
		if v.URI != "" {
			variants = append(variants, v)
		}
	}
	if len(variants) == 0 {
		return nil, fmt.Errorf("no stream variants in enhancedHls master")
	}
	sort.SliceStable(variants, func(i, j int) bool {
		return variants[i].AvgBW > variants[j].AvgBW
	})
	return variants, nil
}

// parseALACGroupDetails extracts sample rate and bit depth from an ALAC audio
// group id like "audio-alac-stereo-44100-24" (rate=44100, depth=24). Returns
// zeros if the pattern doesn't match.
func parseALACGroupDetails(group string) (sampleRate, bitDepth int) {
	if !strings.Contains(group, "alac") {
		return 0, 0
	}
	parts := strings.Split(group, "-")
	if len(parts) < 2 {
		return 0, 0
	}
	// Last two numeric-looking segments are <sampleRate>-<bitDepth>.
	depth, err1 := strconv.Atoi(parts[len(parts)-1])
	rate, err2 := strconv.Atoi(parts[len(parts)-2])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return rate, depth
}

// selectVariantForQuality picks the best stream variant for a requested quality,
// honoring the track's available audioTraits and falling back downward.
//
// Mapping:
//   - QualityHiRes   -> highest-rate ALAC (24-bit / Hi-Res), else any ALAC
//   - QualityLossless-> ALAC (any, prefer 16-bit/44100 CD), else fall through
//   - QualityHigh    -> AAC ~256k
//   - QualityStandard-> AAC ~128k (or lowest AAC)
//
// Atmos (ec-3) is not part of the standard 4-tier ladder; it is only chosen
// when explicitly requested via wantAtmos.
func selectVariantForQuality(variants []enhancedHLSVariant, quality platform.Quality, wantAtmos bool) (enhancedHLSVariant, bool) {
	if wantAtmos {
		if v, ok := bestAtmos(variants); ok {
			return v, true
		}
	}
	switch {
	case quality >= platform.QualityHiRes:
		if v, ok := bestALAC(variants, true); ok {
			return v, true
		}
		if v, ok := bestALAC(variants, false); ok {
			return v, true
		}
		// fall through to AAC
		return bestAAC(variants, 256000)
	case quality == platform.QualityLossless:
		if v, ok := bestALAC(variants, false); ok {
			return v, true
		}
		return bestAAC(variants, 256000)
	case quality == platform.QualityHigh:
		return bestAAC(variants, 256000)
	default: // QualityStandard
		return bestAAC(variants, 128000)
	}
}

func bestALAC(variants []enhancedHLSVariant, hiResOnly bool) (enhancedHLSVariant, bool) {
	var best enhancedHLSVariant
	found := false
	for _, v := range variants {
		if !v.isALAC() {
			continue
		}
		if hiResOnly && v.BitDepth < 24 && v.SampleRate <= 44100 {
			continue
		}
		// variants are sorted by bandwidth desc; prefer higher sample rate.
		if !found || v.SampleRate > best.SampleRate {
			best = v
			found = true
		}
	}
	return best, found
}

func bestAtmos(variants []enhancedHLSVariant) (enhancedHLSVariant, bool) {
	for _, v := range variants { // sorted desc -> first ec-3 is highest bitrate
		if v.isAtmos() {
			return v, true
		}
	}
	return enhancedHLSVariant{}, false
}

// bestAAC returns the AAC variant whose bandwidth is closest to (but not far
// above) the target; falls back to the highest available AAC.
func bestAAC(variants []enhancedHLSVariant, targetBW int) (enhancedHLSVariant, bool) {
	var best enhancedHLSVariant
	found := false
	bestDiff := 1 << 30
	for _, v := range variants {
		if !v.isAAC() {
			continue
		}
		diff := v.AvgBW - targetBW
		if diff < 0 {
			diff = -diff
		}
		if !found || diff < bestDiff {
			best = v
			bestDiff = diff
			found = true
		}
	}
	return best, found
}

// enhancedHLSMedia is the parsed media (sub) playlist for one variant: the
// single byte-range mp4 URL and the ordered FairPlay key URI for each segment
// (fragment). The wrapper expects one key per fragment, in order.
type enhancedHLSMedia struct {
	MP4URL  string   // absolute URL of the single mp4 file
	SegKeys []string // FairPlay skd:// key URI per segment, in playlist order
}

var reMediaKey = regexp.MustCompile(`#EXT-X-KEY:[^\n]*URI="(skd://[^"]+)"`)

// parseEnhancedHLSMedia parses a variant's media playlist. EXT-X-KEY lines set
// the "current" FairPlay key; each subsequent segment (a non-comment line, or
// an EXT-X-MAP init reference) inherits the most recent key. We record the key
// in effect for each fragment so the wrapper handshake can be driven per
// fragment (matching runv2's playlistSegments[i].Key).
func parseEnhancedHLSMedia(mediaURL, content string) (enhancedHLSMedia, error) {
	var m enhancedHLSMedia
	baseURL := mediaURL[:strings.LastIndex(mediaURL, "/")+1]

	currentKey := ""
	mp4Name := ""
	lines := strings.Split(content, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-KEY") {
			if km := reMediaKey.FindStringSubmatch(line); km != nil {
				currentKey = km[1]
			}
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MAP") {
			// init segment; capture the mp4 file name from its URI.
			if im := regexp.MustCompile(`URI="([^"]+)"`).FindStringSubmatch(line); im != nil {
				mp4Name = im[1]
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		// A media segment line: it references the mp4 file (byte-range mode).
		if mp4Name == "" {
			mp4Name = line
		}
		m.SegKeys = append(m.SegKeys, currentKey)
	}

	if mp4Name == "" {
		return m, fmt.Errorf("no mp4 segment in media playlist")
	}
	if strings.HasPrefix(mp4Name, "http") {
		m.MP4URL = mp4Name
	} else {
		m.MP4URL = baseURL + mp4Name
	}
	if len(m.SegKeys) == 0 {
		return m, fmt.Errorf("no segments in media playlist")
	}
	return m, nil
}
