package platform

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var lrcTimestampRe = regexp.MustCompile(`\[(\d+):(\d+)[.:](\d{1,3})\]`)
var lrcLineRe = regexp.MustCompile(`^\[(\d+):(\d+)[.:](\d+)\](.*)$`)

// NormalizeLRCTimestamps normalizes LRC timestamps to [mm:ss.xx].
func NormalizeLRCTimestamps(lyrics string) string {
	return lrcTimestampRe.ReplaceAllStringFunc(lyrics, func(match string) string {
		parts := lrcTimestampRe.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}

		minutes, err := strconv.Atoi(parts[1])
		if err != nil {
			return match
		}
		seconds, err := strconv.Atoi(parts[2])
		if err != nil {
			return match
		}
		frac := parts[3]

		centis := 0
		switch len(frac) {
		case 1:
			centis = mustAtoi(frac) * 10
		case 2:
			centis = mustAtoi(frac)
		default:
			centis = mustAtoi(frac[:2])
		}

		return fmt.Sprintf("[%02d:%02d.%02d]", minutes, seconds, centis)
	})
}

func mustAtoi(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func parseLRCCentiseconds(value string) int {
	if value == "" {
		return 0
	}
	switch len(value) {
	case 1:
		return mustAtoi(value) * 10
	case 2:
		return mustAtoi(value)
	default:
		return mustAtoi(value[:2])
	}
}

// ParseLRCTimestampedLines parses LRC lines into timestamped lyric lines.
func ParseLRCTimestampedLines(lrc string) []LyricLine {
	lines := strings.Split(lrc, "\n")
	result := make([]LyricLine, 0, len(lines))
	for _, line := range lines {
		matches := lrcLineRe.FindStringSubmatch(line)
		if len(matches) != 5 {
			continue
		}
		minutes, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		seconds, err := strconv.Atoi(matches[2])
		if err != nil {
			continue
		}
		centis := parseLRCCentiseconds(matches[3])
		text := strings.TrimSpace(matches[4])
		if text == "" {
			continue
		}
		duration := time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second + time.Duration(centis)*10*time.Millisecond
		result = append(result, LyricLine{Time: duration, Text: text})
	}
	return result
}
