package spotify

import (
	"context"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// audioResolver is the minimal surface Spotify needs from an audio-capable
// platform (YouTube Music) to fulfil downloads. Kept as a local interface so the
// spotify package depends on a behaviour, not a concrete plugin, and so it can
// be stubbed in tests.
type audioResolver interface {
	Search(ctx context.Context, query string, limit int) ([]platform.Track, error)
	GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error)
}

// resolveAudio finds the YouTube Music recording that best matches a Spotify
// track and returns its download info. Matching strategy, in order:
//  1. ISRC search — the International Standard Recording Code identifies the
//     exact recording, so an ISRC hit is authoritative.
//  2. "title artist" text search, scored by title/artist similarity and
//     duration proximity, rejecting weak matches.
func resolveAudio(ctx context.Context, resolver audioResolver, track *platform.Track, quality platform.Quality) (*platform.DownloadInfo, error) {
	if resolver == nil || track == nil {
		return nil, platform.NewUnavailableError(platformName, "audio", "")
	}

	best := matchOnYouTube(ctx, resolver, track)
	if best == "" {
		return nil, platform.NewUnavailableError(platformName, "audio", track.ID)
	}
	info, err := resolver.GetDownloadInfo(ctx, best, quality)
	if err != nil {
		return nil, err
	}
	return info, nil
}

// matchOnYouTube returns the best-matching YouTube Music videoID for a Spotify
// track, or "" when nothing crosses the confidence threshold.
func matchOnYouTube(ctx context.Context, resolver audioResolver, track *platform.Track) string {
	// 1) ISRC search (most precise).
	if isrc := strings.TrimSpace(track.ISRC); isrc != "" {
		if candidates, err := resolver.Search(ctx, isrc, 5); err == nil {
			if id := pickBestCandidate(track, candidates, true); id != "" {
				return id
			}
		}
	}

	// 2) Text search by title + primary artist.
	query := strings.TrimSpace(track.Title)
	if len(track.Artists) > 0 {
		query = strings.TrimSpace(track.Artists[0].Name + " " + track.Title)
	}
	if query == "" {
		return ""
	}
	candidates, err := resolver.Search(ctx, query, 8)
	if err != nil {
		return ""
	}
	return pickBestCandidate(track, candidates, false)
}

// pickBestCandidate scores candidates against the reference Spotify track and
// returns the best videoID above a confidence floor. When isrcMatch is true the
// candidates came from an ISRC query, so the bar is lower (the recording is
// already very likely correct) — only a sane duration is required.
func pickBestCandidate(ref *platform.Track, candidates []platform.Track, isrcMatch bool) string {
	bestID := ""
	bestScore := 0.0
	for _, cand := range candidates {
		if strings.TrimSpace(cand.ID) == "" {
			continue
		}
		score := scoreCandidate(ref, cand)
		if isrcMatch {
			score += 0.3 // ISRC provenance bonus
		}
		if score > bestScore {
			bestScore = score
			bestID = cand.ID
		}
	}
	threshold := 0.55
	if isrcMatch {
		threshold = 0.4
	}
	if bestScore < threshold {
		return ""
	}
	return bestID
}

// scoreCandidate returns a 0..1-ish confidence that cand is the same recording
// as ref, combining title similarity, artist overlap, and duration proximity.
func scoreCandidate(ref *platform.Track, cand platform.Track) float64 {
	titleScore := tokenOverlap(normalizeText(ref.Title), normalizeText(cand.Title))

	artistScore := 0.0
	if len(ref.Artists) > 0 && len(cand.Artists) > 0 {
		refArtists := normalizeText(joinArtists(ref.Artists))
		candArtists := normalizeText(joinArtists(cand.Artists))
		artistScore = tokenOverlap(refArtists, candArtists)
	}

	durationScore := 0.0
	if ref.Duration > 0 && cand.Duration > 0 {
		diff := ref.Duration - cand.Duration
		if diff < 0 {
			diff = -diff
		}
		switch {
		case diff <= 2*time.Second:
			durationScore = 1.0
		case diff <= 5*time.Second:
			durationScore = 0.6
		case diff <= 10*time.Second:
			durationScore = 0.2
		}
	}

	// Reject obvious wrong versions (live/remix/cover/sped up) unless the
	// reference itself mentions them.
	penalty := 0.0
	if hasForbiddenVariant(cand.Title) && !hasForbiddenVariant(ref.Title) {
		penalty = 0.3
	}

	// Weighted blend; title and artist dominate, duration confirms.
	score := 0.45*titleScore + 0.30*artistScore + 0.25*durationScore - penalty
	if score < 0 {
		return 0
	}
	return score
}

var forbiddenVariantWords = []string{"live", "remix", "cover", "sped up", "slowed", "instrumental", "karaoke", "8d audio", "nightcore", "reverb"}

func hasForbiddenVariant(title string) bool {
	low := strings.ToLower(title)
	for _, w := range forbiddenVariantWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

func joinArtists(artists []platform.Artist) string {
	names := make([]string, 0, len(artists))
	for _, a := range artists {
		names = append(names, a.Name)
	}
	return strings.Join(names, " ")
}

// normalizeText lowercases and strips punctuation/feat. noise for comparison.
func normalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Drop common "(feat. X)" / "[official video]" style noise.
	for _, cut := range []string{"(feat.", "(ft.", "feat.", "ft.", "(official", "[official", "(lyric", "(audio"} {
		if i := strings.Index(s, cut); i >= 0 {
			s = s[:i]
		}
	}
	var b strings.Builder
	for _, r := range s {
		if r == '\'' || r == '’' {
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// tokenOverlap is the Jaccard-ish overlap of word sets, in 0..1.
func tokenOverlap(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	setA := map[string]struct{}{}
	for _, w := range strings.Fields(a) {
		setA[w] = struct{}{}
	}
	if len(setA) == 0 {
		return 0
	}
	matched := 0
	setB := map[string]struct{}{}
	for _, w := range strings.Fields(b) {
		setB[w] = struct{}{}
	}
	for w := range setA {
		if _, ok := setB[w]; ok {
			matched++
		}
	}
	union := len(setA) + len(setB) - matched
	if union == 0 {
		return 0
	}
	return float64(matched) / float64(union)
}
