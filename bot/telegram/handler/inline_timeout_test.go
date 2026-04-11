package handler

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type blockingInlineTimeoutPlatform struct {
	*stubPlatform
}

func (p *blockingInlineTimeoutPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (p *blockingInlineTimeoutPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (p *blockingInlineTimeoutPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	return nil, platform.ErrUnsupported
}

func (p *blockingInlineTimeoutPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	return nil, platform.ErrUnsupported
}

func (p *blockingInlineTimeoutPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.ErrUnsupported
}

func TestPrepareInlineSongWithTimeout_UsesProcessTimeout(t *testing.T) {
	manager := newStubManager()
	manager.Register(&blockingInlineTimeoutPlatform{stubPlatform: newStubPlatform("netease")})

	h := &MusicHandler{
		PlatformManager: manager,
		ProcessTimeout:  20 * time.Millisecond,
	}

	start := time.Now()
	_, err := h.prepareInlineSongWithTimeout(context.Background(), nil, 123, "tester", "netease", "track-1", "", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout elapsed too long: %v", elapsed)
	}
}
