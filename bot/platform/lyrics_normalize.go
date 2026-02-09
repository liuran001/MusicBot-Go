package platform

import (
	"fmt"
	"regexp"
	"strconv"
)

var lrcTimestampRe = regexp.MustCompile(`\[(\d+):(\d+)[.:](\d{1,3})\]`)

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
