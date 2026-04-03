package qqmusic

import (
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func TestFallbackQualityProfiles_FromHighOnlyFallsBackLower(t *testing.T) {
	profiles := qualityProfiles()
	info := &qqFileInfo{
		SizeHiRes: 1,
		SizeFlac:  1,
		Size320:   1,
		Size128:   1,
	}

	candidates := fallbackQualityProfiles(profiles, qualityIndex(platform.QualityHigh), info)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].Quality != platform.QualityHigh || candidates[1].Quality != platform.QualityStandard {
		t.Fatalf("unexpected fallback order: %+v", candidates)
	}
}

func TestFallbackQualityProfiles_SkipsZeroSizedProfiles(t *testing.T) {
	profiles := qualityProfiles()
	info := &qqFileInfo{
		SizeFlac: 1,
		Size128:  1,
	}

	candidates := fallbackQualityProfiles(profiles, qualityIndex(platform.QualityHiRes), info)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].Quality != platform.QualityLossless || candidates[1].Quality != platform.QualityStandard {
		t.Fatalf("unexpected fallback order: %+v", candidates)
	}
}
