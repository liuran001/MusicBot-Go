package platform

import "fmt"

// Quality represents the audio quality level for music tracks.
// Different platforms may support different quality levels, and this enum
// provides a unified representation across all platforms.
type Quality int

const (
	// QualityStandard represents standard quality audio (typically 128-192 kbps MP3).
	QualityStandard Quality = iota

	// QualityHigh represents high quality audio (typically 256-320 kbps MP3).
	QualityHigh

	// QualityLossless represents lossless quality audio (typically FLAC).
	QualityLossless

	// QualityHiRes represents high-resolution audio (typically 24-bit FLAC or higher).
	QualityHiRes
)

// String returns the string representation of the Quality enum.
func (q Quality) String() string {
	switch q {
	case QualityStandard:
		return "standard"
	case QualityHigh:
		return "high"
	case QualityLossless:
		return "lossless"
	case QualityHiRes:
		return "hires"
	default:
		return "unknown"
	}
}

// Bitrate returns the approximate bitrate in kbps for the quality level.
// This is a guideline value as actual bitrates may vary by platform and codec.
func (q Quality) Bitrate() int {
	switch q {
	case QualityStandard:
		return 128
	case QualityHigh:
		return 320
	case QualityLossless:
		return 1411 // CD quality FLAC
	case QualityHiRes:
		return 2400 // 24-bit/96kHz approximate
	default:
		return 0
	}
}

// ParseQuality converts a string to Quality enum.
// Returns an error if the string does not match any known quality level.
func ParseQuality(s string) (Quality, error) {
	switch s {
	case "standard":
		return QualityStandard, nil
	case "high":
		return QualityHigh, nil
	case "lossless":
		return QualityLossless, nil
	case "hires":
		return QualityHiRes, nil
	default:
		return QualityStandard, fmt.Errorf("unknown quality level: %s", s)
	}
}
