package lyric

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// lrcEntry is one parsed LRC line: absolute time in seconds, its text, and a
// normalized "[mm:ss.cc]" tag used as a map key for translation/roma lookup.
type lrcEntry struct {
	Time float64
	Text string
	Tag  string
}

var (
	// lrcLineRe matches "[mm:ss.fff]text" or "[mm:ss:fff]text".
	lrcLineRe = regexp.MustCompile(`^\[(\d{1,2}):(\d{1,2})(?:[.:](\d{1,3}))?\](.*)$`)
	// lrcMetaRe matches "[ti:...]"/"[ar:...]"/"[by:...]" metadata lines.
	lrcMetaRe = regexp.MustCompile(`(?i)^\[(ti|ar|by)\s*:\s*(.*?)\]$`)
	// lrcAnyTagRe matches any inline "[..:...]" timestamp (for stripping).
	lrcAnyTagRe = regexp.MustCompile(`\[[0-9]{1,2}:[0-9]{2}(?:[.:][0-9]{1,3})?\]`)
)

// parseLRCEntries parses an LRC track into time-sorted entries, dropping empty
// lines. Mirrors LyricConverterService::parseLrcEntries.
func parseLRCEntries(lrc string) []lrcEntry {
	rows := splitLines(lrc)
	entries := make([]lrcEntry, 0, len(rows))
	for _, row := range rows {
		m := lrcLineRe.FindStringSubmatch(strings.TrimSpace(row))
		if m == nil {
			continue
		}
		min := mustAtoi(m[1])
		sec := mustAtoi(m[2])
		ms := parseLRCFractionToMs(m[3])
		text := strings.TrimSpace(m[4])
		if text == "" {
			continue
		}
		entries = append(entries, lrcEntry{
			Time: float64(min*60+sec) + float64(ms)/1000.0,
			Text: text,
			Tag:  formatLRCTagFromParts(min, sec, ms),
		})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Time < entries[j].Time })
	return entries
}

// parseTranslationMap maps each LRC tag to its text. Mirrors parseTranslationMap.
func parseTranslationMap(track string) map[string]string {
	m := map[string]string{}
	for _, row := range splitLines(track) {
		match := lrcLineRe.FindStringSubmatch(strings.TrimSpace(row))
		if match == nil {
			continue
		}
		min := mustAtoi(match[1])
		sec := mustAtoi(match[2])
		ms := parseLRCFractionToMs(match[3])
		m[formatLRCTagFromParts(min, sec, ms)] = strings.TrimSpace(match[4])
	}
	return m
}

// translationEntry is a time-sorted translation/roma line for nearest lookup.
type translationEntry struct {
	Time float64
	Text string
}

// parseTranslationEntries parses a track into non-empty time-sorted entries,
// using "." fraction separator semantics. Mirrors parseTranslationEntries.
func parseTranslationEntries(track string) []translationEntry {
	var entries []translationEntry
	dotRe := regexp.MustCompile(`^\[(\d{1,2}):(\d{1,2})(?:\.(\d{1,3}))?\](.*)$`)
	for _, row := range splitLines(track) {
		m := dotRe.FindStringSubmatch(strings.TrimSpace(row))
		if m == nil {
			continue
		}
		min := mustAtoi(m[1])
		sec := mustAtoi(m[2])
		msRaw := m[3]
		switch len(msRaw) {
		case 1:
			msRaw += "00"
		case 2:
			msRaw += "0"
		}
		ms := 0
		if len(msRaw) >= 3 {
			ms = mustAtoi(msRaw[:3])
		}
		text := strings.TrimSpace(m[4])
		if text == "" || text == "//" {
			continue
		}
		entries = append(entries, translationEntry{Time: float64(min*60+sec) + float64(ms)/1000.0, Text: text})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Time < entries[j].Time })
	return entries
}

// findNearestTranslationText returns the closest entry text within maxDiffSec.
func findNearestTranslationText(entries []translationEntry, timeSec, maxDiffSec float64) string {
	best := ""
	var bestDiff float64 = -1
	for _, e := range entries {
		diff := math.Abs(e.Time - timeSec)
		if diff > maxDiffSec {
			continue
		}
		if bestDiff < 0 || diff < bestDiff {
			bestDiff = diff
			best = e.Text
		}
	}
	return best
}

// --- time formatting helpers ---

// parseLRCFractionToMs interprets a fractional string as milliseconds,
// right-padding to 3 digits ("4" -> 400, "43" -> 430). Mirrors
// parseLrcFractionToMilliseconds.
func parseLRCFractionToMs(fraction string) int {
	if fraction == "" {
		fraction = "0"
	}
	padded := (fraction + "000")[:3]
	return mustAtoi(padded)
}

// msToRoundedCentis converts milliseconds to centiseconds with +5 rounding.
func msToRoundedCentis(ms int) int {
	if ms < 0 {
		ms = 0
	}
	return (ms + 5) / 10
}

// formatLRCTagFromParts builds a centisecond-precision "[mm:ss.cc]" tag.
func formatLRCTagFromParts(minutes, seconds, milliseconds int) string {
	totalMs := (minutes*60+seconds)*1000 + milliseconds
	if totalMs < 0 {
		totalMs = 0
	}
	centis := msToRoundedCentis(totalMs)
	min := centis / 6000
	sec := (centis % 6000) / 100
	cs := centis % 100
	return fmt.Sprintf("[%02d:%02d.%02d]", min, sec, cs)
}

// formatLRCTagFromMs builds an LRC tag from absolute milliseconds. precision>=3
// yields millisecond "[mm:ss.fff]"; otherwise centisecond "[mm:ss.cc]".
func formatLRCTagFromMs(ms, precision int) string {
	if ms < 0 {
		ms = 0
	}
	if precision >= 3 {
		secAll := ms / 1000
		return fmt.Sprintf("[%02d:%02d.%03d]", secAll/60, secAll%60, ms%1000)
	}
	return formatLRCTagFromParts(0, 0, ms)
}

// secondsToSRTTime formats seconds as "HH:MM:SS,mmm".
func secondsToSRTTime(seconds float64) string {
	ms := int(math.Round(seconds * 1000))
	h := ms / 3600000
	ms -= h * 3600000
	m := ms / 60000
	ms -= m * 60000
	s := ms / 1000
	ms -= s * 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

// secondsToTTMLTime formats seconds as TTML clock-time, dropping the hour field
// when zero ("mm:ss.fff" or "HH:MM:SS.fff").
func secondsToTTMLTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	ms := int(math.Round(seconds * 1000))
	totalSeconds := ms / 1000
	hours := totalSeconds / 3600
	remain := totalSeconds - hours*3600
	minutes := remain / 60
	secs := remain % 60
	millis := ms % 1000
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, millis)
	}
	return fmt.Sprintf("%02d:%02d.%03d", minutes, secs, millis)
}

// secondsToASSTime formats seconds as "H:MM:SS.cc".
func secondsToASSTime(seconds float64) string {
	centis := int(math.Round(seconds * 100))
	h := centis / 360000
	centis -= h * 360000
	m := centis / 6000
	centis -= m * 6000
	s := centis / 100
	centis -= s * 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, centis)
}

// formatSplLineModeTag formats seconds as a line-mode SPL tag "[mm:ss.cc]".
func formatSplLineModeTag(seconds float64) string {
	ms := int(math.Round(seconds * 1000))
	centis := msToRoundedCentis(ms)
	return fmt.Sprintf("[%02d:%02d.%02d]", centis/6000, (centis%6000)/100, centis%100)
}

// formatSplTimestamp formats ms as an SPL word "<mm:ss.cc>" or line "[mm:ss.cc]" tag.
func formatSplTimestamp(ms int, word bool) string {
	if ms < 0 {
		ms = 0
	}
	centis := msToRoundedCentis(ms)
	min := centis / 6000
	sec := (centis % 6000) / 100
	cs := centis % 100
	if word {
		return fmt.Sprintf("<%02d:%02d.%02d>", min, sec, cs)
	}
	return fmt.Sprintf("[%02d:%02d.%02d]", min, sec, cs)
}
