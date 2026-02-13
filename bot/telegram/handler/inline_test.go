package handler

import (
	"context"
	"reflect"
	"strings"
	"testing"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/mymmrac/telego"
)

func TestParseInlineSearchOptions_PageSuffix(t *testing.T) {
	manager := newStubManager()
	manager.Register(newStubPlatform("netease"))
	manager.Register(newStubPlatform("qqmusic"))
	manager.aliases["qq"] = "qqmusic"

	tests := []struct {
		name                string
		input               string
		wantBase            string
		wantPlatform        string
		wantPage            int
		wantFallbackKeyword string
	}{
		{
			name:                "platform with page suffix",
			input:               "jj qq 2",
			wantBase:            "jj",
			wantPlatform:        "qqmusic",
			wantPage:            2,
			wantFallbackKeyword: "",
		},
		{
			name:                "numeric suffix parsed as page",
			input:               "歌名 1988",
			wantBase:            "歌名",
			wantPlatform:        "",
			wantPage:            1988,
			wantFallbackKeyword: "歌名 1988",
		},
		{
			name:                "large numeric after platform parsed as page then clamped later",
			input:               "歌名 qq 1988",
			wantBase:            "歌名",
			wantPlatform:        "qqmusic",
			wantPage:            1988,
			wantFallbackKeyword: "",
		},
		{
			name:                "page one should not pollute keyword",
			input:               "jj qq 1",
			wantBase:            "jj",
			wantPlatform:        "qqmusic",
			wantPage:            1,
			wantFallbackKeyword: "",
		},
		{
			name:                "keyword plus numeric prefers paging with fallback keyword",
			input:               "jj 2",
			wantBase:            "jj",
			wantPlatform:        "",
			wantPage:            2,
			wantFallbackKeyword: "jj 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, platformName, _, page, fallbackKeyword := parseInlineSearchOptions(tt.input, manager)
			if base != tt.wantBase {
				t.Fatalf("base = %q, want %q", base, tt.wantBase)
			}
			if platformName != tt.wantPlatform {
				t.Fatalf("platform = %q, want %q", platformName, tt.wantPlatform)
			}
			if page != tt.wantPage {
				t.Fatalf("page = %d, want %d", page, tt.wantPage)
			}
			if fallbackKeyword != tt.wantFallbackKeyword {
				t.Fatalf("fallbackKeyword = %q, want %q", fallbackKeyword, tt.wantFallbackKeyword)
			}
		})
	}
}

func TestBuildInlineSearchPageFooter_HintQuery(t *testing.T) {
	result := buildInlineSearchPageFooter("jj", "qqmusic", "", 1, 6, 48)
	article, ok := result.(*telego.InlineQueryResultArticle)
	if !ok {
		t.Fatalf("result type = %T, want *telego.InlineQueryResultArticle", result)
	}
	if article.Title != "第 1 页 / 共 6 页" {
		t.Fatalf("title = %q", article.Title)
	}
	if !strings.Contains(article.Description, "jj qq 2") {
		t.Fatalf("description = %q, want contains %q", article.Description, "jj qq 2")
	}
	if strings.Contains(article.Description, "qqmusic") {
		t.Fatalf("description should use alias qq, got %q", article.Description)
	}
	if strings.Contains(article.Description, "jj qq 1 2") {
		t.Fatalf("description contains polluted keyword: %q", article.Description)
	}
}

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
