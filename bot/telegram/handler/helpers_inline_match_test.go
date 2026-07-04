package handler

import (
	"context"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type strictTextMatcherPlatform struct {
	*stubPlatform
}

func (p *strictTextMatcherPlatform) MatchText(text string) (string, bool) {
	return "", false
}

func TestMatchPlatformTrack_DoesNotFallbackForNonID(t *testing.T) {
	manager := newStubManager()
	manager.Register(&strictTextMatcherPlatform{stubPlatform: newStubPlatform("netease")})

	id, ok := matchPlatformTrack(context.Background(), manager, "netease", "jj")
	if ok {
		t.Fatalf("expected no match, got id=%q", id)
	}
}

func TestMatchPlatformTrack_FallbackForLikelyID(t *testing.T) {
	manager := newStubManager()
	manager.Register(&strictTextMatcherPlatform{stubPlatform: newStubPlatform("netease")})

	id, ok := matchPlatformTrack(context.Background(), manager, "netease", "abc123")
	if !ok {
		t.Fatal("expected likely ID fallback to match")
	}
	if id != "abc123" {
		t.Fatalf("id=%q, want %q", id, "abc123")
	}
}

func TestMatchPlatformTrack_DoesNotFallbackForBareNumericID(t *testing.T) {
	manager := newStubManager()
	manager.Register(&strictTextMatcherPlatform{stubPlatform: newStubPlatform("netease")})

	id, ok := matchPlatformTrack(context.Background(), manager, "netease", "12345")
	if ok {
		t.Fatalf("expected numeric token to stay keyword, got id=%q", id)
	}
}

func (p *strictTextMatcherPlatform) Metadata() platform.Meta {
	return platform.Meta{Name: p.Name(), DisplayName: p.Name(), Emoji: "🎵"}
}
