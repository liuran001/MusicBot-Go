package handler

import (
	"context"
	"reflect"
	"testing"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
)

func TestQualityFallbacks(t *testing.T) {
	tests := []struct {
		name    string
		primary string
		want    []string
	}{
		{
			name:    "hires primary",
			primary: "hires",
			want:    []string{"hires", "lossless", "high", "standard"},
		},
		{
			name:    "lossless primary",
			primary: "lossless",
			want:    []string{"lossless", "hires", "high", "standard"},
		},
		{
			name:    "high primary",
			primary: "high",
			want:    []string{"high", "hires", "lossless", "standard"},
		},
		{
			name:    "standard primary",
			primary: "standard",
			want:    []string{"standard", "hires", "lossless", "high"},
		},
		{
			name:    "empty primary",
			primary: "",
			want:    []string{"hires", "lossless", "high", "standard"},
		},
		{
			name:    "whitespace primary",
			primary: "  ",
			want:    []string{"hires", "lossless", "high", "standard"},
		},
		{
			name:    "unknown primary",
			primary: "unknown",
			want:    []string{"unknown", "hires", "lossless", "high", "standard"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := qualityFallbacks(tt.primary)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("qualityFallbacks(%q) = %v, want %v", tt.primary, got, tt.want)
			}
		})
	}
}

func TestQualityFallbacks_Order(t *testing.T) {
	order := qualityFallbacks("hires")
	expectedOrder := []string{"hires", "lossless", "high", "standard"}
	if !reflect.DeepEqual(order, expectedOrder) {
		t.Errorf("qualityFallbacks order = %v, want %v", order, expectedOrder)
	}

	if order[0] != "hires" {
		t.Errorf("qualityFallbacks: primary should be first")
	}
}

func TestInlineSearchHandler_resolveDefaultQuality_UserSettings(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	err := repo.UpdateUserSettings(ctx, &botpkg.UserSettings{
		UserID:         12345,
		DefaultQuality: "lossless",
	})
	if err != nil {
		t.Fatalf("failed to setup user settings: %v", err)
	}

	handler := &InlineSearchHandler{
		Repo:           repo,
		DefaultQuality: "standard",
	}

	got := handler.resolveDefaultQuality(ctx, 12345)
	if got != "lossless" {
		t.Errorf("resolveDefaultQuality(user settings) = %q, want %q", got, "lossless")
	}
}

func TestInlineSearchHandler_resolveDefaultQuality_DefaultQuality(t *testing.T) {
	handler := &InlineSearchHandler{
		DefaultQuality: "high",
	}

	got := handler.resolveDefaultQuality(context.Background(), 12345)
	if got != "high" {
		t.Errorf("resolveDefaultQuality(default) = %q, want %q", got, "high")
	}
}

func TestInlineSearchHandler_resolveDefaultQuality_Fallback(t *testing.T) {
	handler := &InlineSearchHandler{
		DefaultQuality: "",
	}

	got := handler.resolveDefaultQuality(context.Background(), 12345)
	if got != "hires" {
		t.Errorf("resolveDefaultQuality(fallback) = %q, want %q", got, "hires")
	}
}

func TestInlineSearchHandler_resolveDefaultQuality_NilRepo(t *testing.T) {
	handler := &InlineSearchHandler{
		Repo:           nil,
		DefaultQuality: "standard",
	}

	got := handler.resolveDefaultQuality(context.Background(), 12345)
	if got != "standard" {
		t.Errorf("resolveDefaultQuality(nil repo) = %q, want %q", got, "standard")
	}
}

func TestInlineSearchHandler_findCachedSong_ExactMatch(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	song := &botpkg.SongInfo{
		Platform: "netease",
		TrackID:  "12345",
		Quality:  "hires",
		FileID:   "file123",
		SongName: "Test Song",
	}
	err := repo.Create(ctx, song)
	if err != nil {
		t.Fatalf("failed to create song: %v", err)
	}

	handler := &InlineSearchHandler{Repo: repo}
	got := handler.findCachedSong(ctx, "netease", "12345", "hires")
	if got == nil {
		t.Fatal("findCachedSong: expected song, got nil")
	}
	if got.SongName != "Test Song" {
		t.Errorf("findCachedSong: SongName = %q, want %q", got.SongName, "Test Song")
	}
}

func TestInlineSearchHandler_findCachedSong_QualityFallback(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	song := &botpkg.SongInfo{
		Platform: "netease",
		TrackID:  "12345",
		Quality:  "lossless",
		FileID:   "file123",
		SongName: "Test Song",
	}
	err := repo.Create(ctx, song)
	if err != nil {
		t.Fatalf("failed to create song: %v", err)
	}

	handler := &InlineSearchHandler{Repo: repo}
	got := handler.findCachedSong(ctx, "netease", "12345", "hires")
	if got == nil {
		t.Fatal("findCachedSong: expected fallback to lossless, got nil")
	}
	if got.Quality != "lossless" {
		t.Errorf("findCachedSong: Quality = %q, want %q", got.Quality, "lossless")
	}
}

func TestInlineSearchHandler_findCachedSong_NeteaseMusicID(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	song := &botpkg.SongInfo{
		Platform: "netease",
		MusicID:  12345,
		TrackID:  "12345",
		Quality:  "hires",
		FileID:   "file123",
		SongName: "Test Song",
	}
	err := repo.Create(ctx, song)
	if err != nil {
		t.Fatalf("failed to create song: %v", err)
	}

	handler := &InlineSearchHandler{Repo: repo}
	got := handler.findCachedSong(ctx, "netease", "12345", "standard")
	if got == nil {
		t.Fatal("findCachedSong: expected netease musicID fallback, got nil")
	}
	if got.SongName != "Test Song" {
		t.Errorf("findCachedSong: SongName = %q, want %q", got.SongName, "Test Song")
	}
}

func TestInlineSearchHandler_findCachedSong_NotFound(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	handler := &InlineSearchHandler{Repo: repo}
	got := handler.findCachedSong(ctx, "netease", "99999", "hires")
	if got != nil {
		t.Errorf("findCachedSong(not found): expected nil, got %+v", got)
	}
}

func TestInlineSearchHandler_findCachedSong_EmptyPlatform(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	handler := &InlineSearchHandler{Repo: repo}
	got := handler.findCachedSong(ctx, "", "12345", "hires")
	if got != nil {
		t.Errorf("findCachedSong(empty platform): expected nil, got %+v", got)
	}
}

func TestInlineSearchHandler_findCachedSong_EmptyTrackID(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	handler := &InlineSearchHandler{Repo: repo}
	got := handler.findCachedSong(ctx, "netease", "", "hires")
	if got != nil {
		t.Errorf("findCachedSong(empty trackID): expected nil, got %+v", got)
	}
}

func TestInlineSearchHandler_findCachedSong_NilRepo(t *testing.T) {
	handler := &InlineSearchHandler{Repo: nil}
	got := handler.findCachedSong(context.Background(), "netease", "12345", "hires")
	if got != nil {
		t.Errorf("findCachedSong(nil repo): expected nil, got %+v", got)
	}
}

func TestInlineSearchHandler_findCachedSong_InvalidFileID(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	song := &botpkg.SongInfo{
		Platform: "netease",
		TrackID:  "12345",
		Quality:  "hires",
		FileID:   "",
		SongName: "Test Song",
	}
	err := repo.Create(ctx, song)
	if err != nil {
		t.Fatalf("failed to create song: %v", err)
	}

	handler := &InlineSearchHandler{Repo: repo}
	got := handler.findCachedSong(ctx, "netease", "12345", "hires")
	if got != nil {
		t.Errorf("findCachedSong(empty FileID): expected nil, got %+v", got)
	}
}

func TestInlineSearchHandler_findCachedSong_InvalidSongName(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	song := &botpkg.SongInfo{
		Platform: "netease",
		TrackID:  "12345",
		Quality:  "hires",
		FileID:   "file123",
		SongName: "",
	}
	err := repo.Create(ctx, song)
	if err != nil {
		t.Fatalf("failed to create song: %v", err)
	}

	handler := &InlineSearchHandler{Repo: repo}
	got := handler.findCachedSong(ctx, "netease", "12345", "hires")
	if got != nil {
		t.Errorf("findCachedSong(empty SongName): expected nil, got %+v", got)
	}
}
