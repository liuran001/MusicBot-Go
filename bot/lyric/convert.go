package lyric

import "strings"

// Payload carries the raw lyric tracks a platform surfaced. Fields mirror the
// JSON keys consumed by LyricConverterService: the plain LRC, the translation
// and roma side-tracks, and any platform-native word-by-word raw track.
type Payload struct {
	// Lyric is the plain LRC (line-timed) lyric text.
	Lyric string
	// Translation is the translated LRC side-track (zh-Hans).
	Translation string
	// Roma is the romanization LRC side-track.
	Roma string

	// RawYRC / RawQRC / RawLYS hold a platform-native word-by-word raw track,
	// when available. Exactly one is typically set.
	RawYRC string
	RawQRC string
	RawLYS string

	// RawTTML is Apple Music's native word-timed TTML, when available. It is a
	// fully-formed target document and is returned verbatim for format "ttml".
	RawTTML string

	// Metadata used by document-style formats (lqe/ttml/amjson/elrc).
	MusicName  string
	Artist     string
	Album      string
	Source     string // platform name, e.g. "netease"/"tencent"
	NcmMusicID string
	QqMusicID  string
}

// Options controls optional side-track inclusion in formats that support it.
type Options struct {
	// IncludeTranslation merges the translation side-track. When unset for a
	// format, defaultsIncludeTranslation decides per-format.
	IncludeTranslation *bool
	// IncludeRoma merges the romanization side-track.
	IncludeRoma bool
	// RomaFirst emits roma before translation when both are present.
	RomaFirst bool
}

// pickLrcLikeLyric prefers an explicit plain LRC track. Mirrors pickLrcLikeLyric.
func (p Payload) pickLrcLikeLyric() string { return p.Lyric }

// pickTokenLyric returns the first available word-by-word raw track, or the
// plain lyric when it is itself token-shaped. Mirrors pickTokenLyric.
func (p Payload) pickTokenLyric() string {
	for _, t := range []string{p.RawYRC, p.RawQRC, p.RawLYS} {
		if strings.TrimSpace(t) != "" {
			return t
		}
	}
	// Apple Music only hands us TTML; derive a token track from its word spans
	// so non-TTML word formats (yrc/qrc/lys/spl/ass/elrc) still work.
	if strings.TrimSpace(p.RawTTML) != "" {
		if tok := ttmlToTokenTrack(p.RawTTML); strings.TrimSpace(tok) != "" {
			return tok
		}
	}
	if t := strings.TrimSpace(p.Lyric); t != "" && lineHeadRe.MatchString(t) {
		return p.Lyric
	}
	return ""
}

// NormalizeFormat resolves a user-facing format token to its canonical name,
// applying aliases. Empty/"auto" resolve to "lrc". Mirrors normalizeFormat.
func NormalizeFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" || format == "auto" {
		return "lrc"
	}
	aliases := map[string]string{
		"origin":         "raw",
		"original":       "raw",
		"default":        "lrc",
		"enhancedlrc":    "elrc",
		"lrcx":           "elrc",
		"alrc":           "elrc",
		"applemusicjson": "amjson",
	}
	if mapped, ok := aliases[format]; ok {
		return mapped
	}
	return format
}

// defaultsIncludeTranslation reports whether a format merges the translation
// track by default when the caller did not specify. Mirrors lyric-convert.php.
func defaultsIncludeTranslation(resolved string) bool {
	switch resolved {
	case "spl", "ttml", "amjson", "ass", "lqe":
		return true
	}
	return false
}

// SupportedFormats is the ordered list of formats offered for switching.
var SupportedFormats = []string{
	"lrc", "yrc", "qrc", "lys", "krc", "elrc", "spl", "ass", "lqe", "ttml", "amjson", "srt", "txt", "trans", "roma",
}

// FileExtension returns the file extension (without dot) for a format.
func FileExtension(format string) string {
	switch NormalizeFormat(format) {
	case "amjson":
		return "json"
	case "ass":
		return "ass"
	case "srt":
		return "srt"
	case "txt", "trans":
		return "txt"
	case "ttml":
		return "ttml"
	case "lqe":
		return "lqe"
	case "lys":
		return "lys"
	case "qrc":
		return "qrc"
	case "yrc":
		return "yrc"
	case "krc":
		return "krc"
	case "elrc":
		return "lrc"
	default:
		return "lrc"
	}
}

// Convert renders the payload into the requested format. It mirrors
// LyricConverterService::convert: token-shaped raw tracks are preferred for
// word-by-word output, with graceful fallback to the plain LRC.
func Convert(p Payload, format string, opts Options) string {
	lyric := p.pickLrcLikeLyric()
	tokenLyric := p.pickTokenLyric()
	tlyric := p.Translation
	roma := normalizeRomaLyric(pickRoma(p))
	resolved := NormalizeFormat(format)

	includeTranslation := defaultsIncludeTranslation(resolved)
	if opts.IncludeTranslation != nil {
		includeTranslation = *opts.IncludeTranslation
	}
	includeRoma := opts.IncludeRoma
	romaFirst := opts.RomaFirst

	if lyric == "" && tokenLyric != "" {
		lyric = tokenToLRC(tokenLyric)
	}

	switch resolved {
	case "roma":
		return roma
	case "trans":
		return translationOnly(tlyric)
	case "raw":
		// "raw" is always the verbatim platform LRC, never merged.
		return lyric
	case "lrc":
		// Plain LRC merges the translation/roma side-tracks as extra timestamped
		// lines only when explicitly requested (single-track by default).
		tl := ""
		if includeTranslation {
			tl = tlyric
		}
		rm := ""
		if includeRoma {
			rm = roma
		}
		if tl == "" && rm == "" {
			return lyric
		}
		return mergeLrcTracks(lyric, tl, rm, romaFirst)
	case "txt":
		return lrcToTxt(lyric)
	case "srt":
		return lrcToSrt(lyric)
	}

	if resolved == "yrc" {
		if strings.TrimSpace(p.RawYRC) != "" {
			return p.RawYRC
		}
		if tokenLyric != "" {
			return tokenToYRC(tokenLyric)
		}
		return lyric
	}
	if resolved == "qrc" {
		if strings.TrimSpace(p.RawQRC) != "" {
			return p.RawQRC
		}
		if tokenLyric != "" {
			return tokenToQRC(tokenLyric)
		}
		return lyric
	}
	if resolved == "krc" {
		// KRC has a bespoke on-wire encoding; for export we emit the LYS-style
		// document which carries the same word timing in a readable form.
		if tokenLyric != "" {
			return tokenToLysDocument(tokenLyric, p, lyric, tlyric, roma)
		}
		return lyric
	}
	if resolved == "lys" {
		if tokenLyric != "" {
			return tokenToLysDocument(tokenLyric, p, lyric, tlyric, roma)
		}
		return lyric
	}

	if resolved == "elrc" {
		if tokenLyric != "" {
			if converted := tokenToElrc(tokenLyric, p, lyric, tlyric, roma); converted != "" {
				return converted
			}
		}
		return lyric
	}

	romaTrack := ""
	if includeRoma {
		romaTrack = roma
	}

	switch resolved {
	case "spl":
		if tokenLyric != "" {
			if includeTranslation {
				return tokenToSpl(tokenLyric, tlyric, romaTrack, romaFirst)
			}
			return tokenToSpl(tokenLyric, "", romaTrack, romaFirst)
		}
		if includeTranslation {
			return lrcToSpl(lyric, tlyric, romaTrack, romaFirst)
		}
		return lrcToSpl(lyric, "", romaTrack, romaFirst)
	case "ass":
		if tokenLyric != "" {
			if includeTranslation {
				return tokenToAss(tokenLyric, tlyric, romaTrack, romaFirst)
			}
			return tokenToAss(tokenLyric, "", romaTrack, romaFirst)
		}
		if includeTranslation {
			return lrcToAss(lyric, tlyric, romaTrack, romaFirst)
		}
		return lrcToAss(lyric, "", romaTrack, romaFirst)
	case "lqe":
		return lrcToLqe(lyric, ternary(includeTranslation, tlyric, ""), romaTrack, p, tokenLyric, romaFirst)
	case "ttml":
		// Apple Music already hands us a complete word-timed TTML document.
		if strings.TrimSpace(p.RawTTML) != "" {
			return p.RawTTML
		}
		if tokenLyric != "" {
			return tokenToTTML(tokenLyric, ternary(includeTranslation, tlyric, ""), p, romaTrack, true, romaFirst)
		}
		return lrcToTTML(lyric, ternary(includeTranslation, tlyric, ""), p, romaTrack, true, romaFirst)
	case "amjson":
		if tokenLyric != "" {
			return ttmlToAppleMusicJSON(tokenToTTML(tokenLyric, ternary(includeTranslation, tlyric, ""), p, romaTrack, false, romaFirst))
		}
		return ttmlToAppleMusicJSON(lrcToTTML(lyric, ternary(includeTranslation, tlyric, ""), p, romaTrack, false, romaFirst))
	}

	return lyric
}

func pickRoma(p Payload) string { return p.Roma }

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
