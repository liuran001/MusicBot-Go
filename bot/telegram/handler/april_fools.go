package handler

import (
	"fmt"
	"math/rand"
	"sync/atomic"
	"time"
)

const (
	aprilFoolsTrackHijackProbability = 0.10
)

var (
	aprilFoolsTimezone = time.FixedZone("UTC+8", 8*60*60)
	aprilFoolsEnabled  atomic.Bool
)

var aprilFoolsReplacementTracks = []struct {
	platform string
	trackID  string
}{
	{platform: "netease", trackID: "18520488"},
	{platform: "netease", trackID: "2722442265"},
	{platform: "qqmusic", trackID: "0037LAOz0eqQCm"},
	{platform: "netease", trackID: "484311588"},
}

func SetAprilFoolsEnabled(enabled bool) {
	aprilFoolsEnabled.Store(enabled)
}

func isAprilFoolsDayNow() bool {
	now := time.Now().In(aprilFoolsTimezone)
	return now.Month() == time.April && now.Day() == 1
}

func shouldApplyAprilFoolsTrackHijack() bool {
	if !aprilFoolsEnabled.Load() || !isAprilFoolsDayNow() {
		return false
	}
	return rand.Float64() < aprilFoolsTrackHijackProbability
}

func pickAprilFoolsReplacementTrack() (string, string) {
	if len(aprilFoolsReplacementTracks) == 0 {
		return "", ""
	}
	picked := aprilFoolsReplacementTracks[rand.Intn(len(aprilFoolsReplacementTracks))]
	return picked.platform, picked.trackID
}

func maybeApplyAprilFoolsTrackHijack(platformName, trackID string) (string, string, bool, string) {
	if !shouldApplyAprilFoolsTrackHijack() {
		return platformName, trackID, false, ""
	}
	replacementPlatform, replacementTrackID := pickAprilFoolsReplacementTrack()
	if replacementPlatform == "" || replacementTrackID == "" {
		return platformName, trackID, false, ""
	}
	return replacementPlatform, replacementTrackID, true, fmt.Sprintf("%s:%s", replacementPlatform, replacementTrackID)
}
