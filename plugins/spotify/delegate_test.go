package spotify

import (
	"context"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// stubResolver is an in-memory audioResolver for testing the matching logic
// without any network calls.
type stubResolver struct {
	byQuery map[string][]platform.Track
	infos   map[string]*platform.DownloadInfo
}

func (s *stubResolver) Search(_ context.Context, query string, _ int) ([]platform.Track, error) {
	return s.byQuery[query], nil
}

func (s *stubResolver) GetDownloadInfo(_ context.Context, trackID string, _ platform.Quality) (*platform.DownloadInfo, error) {
	if info, ok := s.infos[trackID]; ok {
		return info, nil
	}
	return nil, platform.ErrUnavailable
}

func TestResolveAudio_ISRCMatch(t *testing.T) {
	ref := &platform.Track{
		Title:    "Bohemian Rhapsody",
		Artists:  []platform.Artist{{Name: "Queen"}},
		Duration: 354 * time.Second,
		ISRC:     "GBUM71029604",
	}
	resolver := &stubResolver{
		byQuery: map[string][]platform.Track{
			"GBUM71029604": {
				{ID: "vidISRC", Title: "Bohemian Rhapsody", Artists: []platform.Artist{{Name: "Queen"}}, Duration: 355 * time.Second},
			},
		},
		infos: map[string]*platform.DownloadInfo{
			"vidISRC": {URL: "https://stream/vidISRC", Format: "opus"},
		},
	}
	info, err := resolveAudio(context.Background(), resolver, ref, platform.QualityHigh)
	if err != nil {
		t.Fatalf("resolveAudio error: %v", err)
	}
	if info == nil || info.URL != "https://stream/vidISRC" {
		t.Fatalf("expected ISRC-matched stream, got %+v", info)
	}
}

func TestResolveAudio_TitleArtistFallback(t *testing.T) {
	ref := &platform.Track{
		Title:    "Shape of You",
		Artists:  []platform.Artist{{Name: "Ed Sheeran"}},
		Duration: 233 * time.Second,
		// no ISRC -> must fall back to text search
	}
	resolver := &stubResolver{
		byQuery: map[string][]platform.Track{
			"Ed Sheeran Shape of You": {
				{ID: "wrongLive", Title: "Shape of You (Live at Wembley)", Artists: []platform.Artist{{Name: "Ed Sheeran"}}, Duration: 240 * time.Second},
				{ID: "rightStudio", Title: "Shape of You", Artists: []platform.Artist{{Name: "Ed Sheeran"}}, Duration: 233 * time.Second},
			},
		},
		infos: map[string]*platform.DownloadInfo{
			"rightStudio": {URL: "https://stream/rightStudio", Format: "m4a"},
			"wrongLive":   {URL: "https://stream/wrongLive", Format: "m4a"},
		},
	}
	info, err := resolveAudio(context.Background(), resolver, ref, platform.QualityHigh)
	if err != nil {
		t.Fatalf("resolveAudio error: %v", err)
	}
	if info == nil || info.URL != "https://stream/rightStudio" {
		t.Fatalf("expected studio version preferred over live, got %+v", info)
	}
}

func TestResolveAudio_NoMatch(t *testing.T) {
	ref := &platform.Track{
		Title:    "Totally Unrelated Song XYZ",
		Artists:  []platform.Artist{{Name: "Nobody"}},
		Duration: 200 * time.Second,
	}
	resolver := &stubResolver{
		byQuery: map[string][]platform.Track{
			"Nobody Totally Unrelated Song XYZ": {
				{ID: "garbage", Title: "Completely Different Track", Artists: []platform.Artist{{Name: "Other Artist"}}, Duration: 60 * time.Second},
			},
		},
	}
	_, err := resolveAudio(context.Background(), resolver, ref, platform.QualityHigh)
	if err == nil {
		t.Fatal("expected no-match to return an error")
	}
}

func TestScoreCandidate_RejectsForbiddenVariant(t *testing.T) {
	ref := &platform.Track{Title: "Hello", Artists: []platform.Artist{{Name: "Adele"}}, Duration: 295 * time.Second}
	studio := platform.Track{Title: "Hello", Artists: []platform.Artist{{Name: "Adele"}}, Duration: 295 * time.Second}
	remix := platform.Track{Title: "Hello (Remix)", Artists: []platform.Artist{{Name: "Adele"}}, Duration: 295 * time.Second}
	if scoreCandidate(ref, studio) <= scoreCandidate(ref, remix) {
		t.Fatal("studio version should score higher than remix")
	}
}
