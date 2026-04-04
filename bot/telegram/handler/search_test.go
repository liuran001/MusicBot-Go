package handler

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

func TestSearchHandler_searchLimit(t *testing.T) {
	handler := &SearchHandler{}

	tests := []struct {
		name         string
		platformName string
		want         int
	}{
		{
			name:         "netease",
			platformName: "netease",
			want:         neteaseSearchLimit,
		},
		{
			name:         "netease with whitespace",
			platformName: "  netease  ",
			want:         neteaseSearchLimit,
		},
		{
			name:         "spotify",
			platformName: "spotify",
			want:         defaultSearchLimit,
		},
		{
			name:         "empty",
			platformName: "",
			want:         defaultSearchLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.searchLimit(tt.platformName)
			if got != tt.want {
				t.Errorf("searchLimit(%q) = %d, want %d", tt.platformName, got, tt.want)
			}
		})
	}
}

func TestSearchHandler_buildPlatformSwitchRowsWrapAtThree(t *testing.T) {
	handler := &SearchHandler{PlatformManager: platform.NewManager()}
	platforms := []platform.Platform{
		stubSearchPlatform{name: "netease", displayName: "网易云音乐", emoji: "🎵"},
		stubSearchPlatform{name: "qqmusic", displayName: "QQ音乐", emoji: "🎶"},
		stubSearchPlatform{name: "kugou", displayName: "酷狗音乐", emoji: "🐶"},
		stubSearchPlatform{name: "bilibili", displayName: "哔哩哔哩", emoji: "📺"},
		stubSearchPlatform{name: "soda", displayName: "汽水音乐", emoji: "🥤"},
	}
	for _, plat := range platforms {
		handler.PlatformManager.Register(plat)
	}

	rows := handler.buildPlatformSwitchRows("soda", 12345, 100, nil)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if len(rows[0]) != 3 {
		t.Fatalf("expected first row has 3 buttons, got %d", len(rows[0]))
	}
	if len(rows[1]) != 2 {
		t.Fatalf("expected second row has 2 buttons, got %d", len(rows[1]))
	}
	if rows[1][1].Text != "✅ 汽水音乐" {
		t.Fatalf("expected current platform full label in wrapped row, got %q", rows[1][1].Text)
	}
}

type stubSearchPlatform struct {
	name        string
	displayName string
	emoji       string
}

func (s stubSearchPlatform) Name() string              { return s.name }
func (s stubSearchPlatform) SupportsDownload() bool    { return false }
func (s stubSearchPlatform) SupportsSearch() bool      { return true }
func (s stubSearchPlatform) SupportsLyrics() bool      { return false }
func (s stubSearchPlatform) SupportsRecognition() bool { return false }
func (s stubSearchPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{Search: true}
}
func (s stubSearchPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	return nil, nil
}
func (s stubSearchPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s stubSearchPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s stubSearchPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s stubSearchPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s stubSearchPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s stubSearchPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s stubSearchPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s stubSearchPlatform) Metadata() platform.Meta {
	return platform.Meta{Name: s.name, DisplayName: s.displayName, Emoji: s.emoji}
}

func TestParseSearchCallbackData(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want parsedSearchCallback
	}{
		{
			name: "close",
			args: []string{"search", "100", "close", "12345"},
			want: parsedSearchCallback{messageID: 100, action: "close", requesterID: 12345, ok: true},
		},
		{
			name: "home",
			args: []string{"search", "100", "home", "12345"},
			want: parsedSearchCallback{messageID: 100, action: "home", requesterID: 12345, ok: true},
		},
		{
			name: "page",
			args: []string{"search", "100", "page", "2", "12345"},
			want: parsedSearchCallback{messageID: 100, action: "page", page: 2, requesterID: 12345, ok: true},
		},
		{
			name: "platform",
			args: []string{"search", "100", "platform", "qqmusic", "12345"},
			want: parsedSearchCallback{messageID: 100, action: "platform", platformName: "qqmusic", requesterID: 12345, ok: true},
		},
		{
			name: "bilifilter on",
			args: []string{"search", "100", "bilifilter", "on", "12345"},
			want: parsedSearchCallback{messageID: 100, action: "bilifilter", filterEnabled: true, requesterID: 12345, ok: true},
		},
		{
			name: "bilifilter off",
			args: []string{"search", "100", "bilifilter", "off", "12345"},
			want: parsedSearchCallback{messageID: 100, action: "bilifilter", filterEnabled: false, requesterID: 12345, ok: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSearchCallbackData(tt.args)
			if got != tt.want {
				t.Fatalf("parse mismatch: got %+v want %+v", got, tt.want)
			}
		})
	}
}

func TestParseSearchCallbackData_InvalidArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "empty", args: nil},
		{name: "wrong prefix", args: []string{"music", "100", "close", "12345"}},
		{name: "platform requester wrong index", args: []string{"search", "100", "platform", "qqmusic"}},
		{name: "platform requester not number", args: []string{"search", "100", "platform", "qqmusic", "abc"}},
		{name: "page not number", args: []string{"search", "100", "page", "x", "12345"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSearchCallbackData(tt.args)
			if got.ok {
				t.Fatalf("expected invalid parse, got %+v", got)
			}
		})
	}
}

func TestSearchHandler_resolveDefaultQuality_Group(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	err := repo.UpdateGroupSettings(ctx, &botpkg.GroupSettings{
		ChatID:         -100123,
		DefaultQuality: "lossless",
	})
	if err != nil {
		t.Fatalf("failed to setup group settings: %v", err)
	}

	handler := &SearchHandler{Repo: repo}
	msg := &telego.Message{
		Chat: telego.Chat{
			ID:   -100123,
			Type: "group",
		},
	}

	got := handler.resolveDefaultQuality(ctx, msg, 0)
	if got != "lossless" {
		t.Errorf("resolveDefaultQuality(group) = %q, want %q", got, "lossless")
	}
}

func TestSearchHandler_resolveDefaultQuality_Private(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()

	err := repo.UpdateUserSettings(ctx, &botpkg.UserSettings{
		UserID:         12345,
		DefaultQuality: "high",
	})
	if err != nil {
		t.Fatalf("failed to setup user settings: %v", err)
	}

	handler := &SearchHandler{Repo: repo}
	msg := &telego.Message{
		Chat: telego.Chat{
			ID:   12345,
			Type: "private",
		},
	}

	got := handler.resolveDefaultQuality(ctx, msg, 12345)
	if got != "high" {
		t.Errorf("resolveDefaultQuality(private) = %q, want %q", got, "high")
	}
}

func TestSearchHandler_resolveDefaultQuality_NoSettings(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()
	handler := &SearchHandler{Repo: repo}

	msg := &telego.Message{
		Chat: telego.Chat{
			ID:   -100123,
			Type: "group",
		},
	}

	got := handler.resolveDefaultQuality(ctx, msg, 0)
	if got != "hires" {
		t.Errorf("resolveDefaultQuality(no settings) = %q, want %q", got, "hires")
	}
}

func TestSearchHandler_resolveDefaultQuality_NilRepo(t *testing.T) {
	handler := &SearchHandler{Repo: nil}
	ctx := context.Background()
	msg := &telego.Message{
		Chat: telego.Chat{
			ID:   -100123,
			Type: "group",
		},
	}

	got := handler.resolveDefaultQuality(ctx, msg, 0)
	if got != "hires" {
		t.Errorf("resolveDefaultQuality(nil repo) = %q, want %q", got, "hires")
	}
}

func TestResolvePlatformQualityValue_KugouDefaultHiResDowngrades(t *testing.T) {
	repo := newStubRepo()
	got := resolvePlatformQualityValue(context.Background(), repo, botpkg.PluginScopeUser, 12345, "kugou", "hires", false)
	if got != "lossless" {
		t.Fatalf("resolvePlatformQualityValue() = %q, want %q", got, "lossless")
	}
}

func TestResolvePlatformQualityValue_KugouExplicitHiResNotDowngraded(t *testing.T) {
	repo := newStubRepo()
	got := resolvePlatformQualityValue(context.Background(), repo, botpkg.PluginScopeUser, 12345, "kugou", "hires", true)
	if got != "hires" {
		t.Fatalf("resolvePlatformQualityValue() = %q, want %q", got, "hires")
	}
}

func TestSearchHandler_buildSearchPage_Basic(t *testing.T) {
	handler := &SearchHandler{}

	tracks := []platform.Track{
		{
			ID:    "1",
			Title: "Song One",
			Artists: []platform.Artist{
				{Name: "Artist A"},
			},
		},
		{
			ID:    "2",
			Title: "Song Two",
			Artists: []platform.Artist{
				{Name: "Artist B"},
				{Name: "Artist C"},
			},
		},
	}

	pageText, keyboard := handler.buildSearchPage(tracks, "netease", "test", "hires", 12345, 100, 1, nil, false, 48, true, "")

	if !strings.Contains(pageText, "test") {
		t.Errorf("buildSearchPage: pageText missing keyword 'test'")
	}
	if keyboard == nil {
		t.Fatal("buildSearchPage: keyboard is nil")
	}
	if len(keyboard.InlineKeyboard) == 0 {
		t.Fatal("buildSearchPage: keyboard has no rows")
	}

	buttonRow := keyboard.InlineKeyboard[0]
	if len(buttonRow) != 2 {
		t.Errorf("buildSearchPage: button row has %d buttons, want 2", len(buttonRow))
	}

	if buttonRow[0].Text != "1" {
		t.Errorf("buildSearchPage: first button text = %q, want %q", buttonRow[0].Text, "1")
	}
	if !strings.Contains(buttonRow[0].CallbackData, "music") {
		t.Errorf("buildSearchPage: callback data missing 'music'")
	}
	if !strings.Contains(buttonRow[0].CallbackData, "netease") {
		t.Errorf("buildSearchPage: callback data missing 'netease'")
	}
	if !strings.Contains(buttonRow[0].CallbackData, "hires") {
		t.Errorf("buildSearchPage: callback data missing 'hires'")
	}
}

func TestSearchHandler_buildSearchPage_Pagination(t *testing.T) {
	handler := &SearchHandler{}

	tracks := make([]platform.Track, 20)
	for i := 0; i < 20; i++ {
		tracks[i] = platform.Track{
			ID:    string(rune('A' + i)),
			Title: "Song " + string(rune('A'+i)),
			Artists: []platform.Artist{
				{Name: "Artist"},
			},
		}
	}

	pageText, keyboard := handler.buildSearchPage(tracks, "netease", "test", "hires", 12345, 100, 1, nil, false, 48, true, "")
	if !strings.Contains(pageText, "1/3") {
		t.Errorf("buildSearchPage page 1: missing pagination '1/3'")
	}
	if len(keyboard.InlineKeyboard) < 2 {
		t.Fatal("buildSearchPage page 1: missing navigation row")
	}
	navRow := keyboard.InlineKeyboard[1]
	if len(navRow) != 2 {
		t.Errorf("buildSearchPage page 1: nav row has %d buttons, want 2", len(navRow))
	}
	if navRow[0].Text != "❌ 关闭" {
		t.Errorf("buildSearchPage page 1: first nav button = %q, want %q", navRow[0].Text, "❌ 关闭")
	}
	if navRow[1].Text != "➡️ 下一页" {
		t.Errorf("buildSearchPage page 1: second nav button = %q, want %q", navRow[1].Text, "➡️ 下一页")
	}

	pageText2, keyboard2 := handler.buildSearchPage(tracks, "netease", "test", "hires", 12345, 100, 2, nil, false, 48, true, "")
	if !strings.Contains(pageText2, "2/3") {
		t.Errorf("buildSearchPage page 2: missing pagination '2/3'")
	}
	navRow2 := keyboard2.InlineKeyboard[1]
	if len(navRow2) != 2 {
		t.Errorf("buildSearchPage page 2: nav row has %d buttons, want 2", len(navRow2))
	}
	if navRow2[0].Text != "⬅️ 上一页" {
		t.Errorf("buildSearchPage page 2: first nav button = %q, want %q", navRow2[0].Text, "⬅️ 上一页")
	}
	if navRow2[1].Text != "➡️ 下一页" {
		t.Errorf("buildSearchPage page 2: second nav button = %q, want %q", navRow2[1].Text, "➡️ 下一页")
	}

	pageText3, keyboard3 := handler.buildSearchPage(tracks, "netease", "test", "hires", 12345, 100, 3, nil, false, 48, true, "")
	if !strings.Contains(pageText3, "3/3") {
		t.Errorf("buildSearchPage page 3: missing pagination '3/3'")
	}
	navRow3 := keyboard3.InlineKeyboard[1]
	if len(navRow3) != 2 {
		t.Errorf("buildSearchPage page 3: nav row has %d buttons, want 2", len(navRow3))
	}
	if navRow3[0].Text != "⬅️ 上一页" {
		t.Errorf("buildSearchPage page 3: first nav button = %q, want %q", navRow3[0].Text, "⬅️ 上一页")
	}
	if navRow3[1].Text != "🏠 回到主页" {
		t.Errorf("buildSearchPage page 3: second nav button = %q, want %q", navRow3[1].Text, "🏠 回到主页")
	}
}

func TestSearchHandler_buildSearchPage_SinglePage(t *testing.T) {
	handler := &SearchHandler{}

	tracks := []platform.Track{
		{ID: "1", Title: "Song", Artists: []platform.Artist{{Name: "Artist"}}},
	}

	pageText, keyboard := handler.buildSearchPage(tracks, "netease", "test", "hires", 12345, 100, 1, nil, false, 48, true, "")
	if strings.Contains(pageText, "/") {
		t.Errorf("buildSearchPage single page: should not show pagination")
	}
	if len(keyboard.InlineKeyboard) < 2 {
		t.Fatal("buildSearchPage single page: missing close button row")
	}
	closeRow := keyboard.InlineKeyboard[1]
	if len(closeRow) != 1 {
		t.Errorf("buildSearchPage single page: close row has %d buttons, want 1", len(closeRow))
	}
	if closeRow[0].Text != "❌ 关闭" {
		t.Errorf("buildSearchPage single page: close button = %q, want %q", closeRow[0].Text, "❌ 关闭")
	}
}

func TestSearchHandler_storeSearchState(t *testing.T) {
	handler := &SearchHandler{}

	state := &searchState{
		keyword:     "test",
		platform:    "netease",
		quality:     "hires",
		requesterID: 12345,
		limit:       10,
		updatedAt:   time.Now(),
	}

	handler.storeSearchState(100, state)

	got, ok := handler.getSearchState(100)
	if !ok {
		t.Fatal("getSearchState: state not found")
	}
	if got.keyword != "test" {
		t.Errorf("getSearchState: keyword = %q, want %q", got.keyword, "test")
	}
	if got.platform != "netease" {
		t.Errorf("getSearchState: platform = %q, want %q", got.platform, "netease")
	}
	if got.quality != "hires" {
		t.Errorf("getSearchState: quality = %q, want %q", got.quality, "hires")
	}
}

func TestSearchHandler_getSearchState_NotFound(t *testing.T) {
	handler := &SearchHandler{}

	_, ok := handler.getSearchState(999)
	if ok {
		t.Error("getSearchState: found non-existent state")
	}
}

func TestSearchHandler_storeSearchState_ZeroMessageID(t *testing.T) {
	handler := &SearchHandler{}

	state := &searchState{keyword: "test"}
	handler.storeSearchState(0, state)

	_, ok := handler.getSearchState(0)
	if ok {
		t.Error("storeSearchState(0): should not store zero messageID")
	}
}

func TestSearchHandler_storeSearchState_NilState(t *testing.T) {
	handler := &SearchHandler{}

	handler.storeSearchState(100, nil)

	_, ok := handler.getSearchState(100)
	if ok {
		t.Error("storeSearchState(nil): should not store nil state")
	}
}

func TestSearchHandler_cleanupSearchStateLocked(t *testing.T) {
	handler := &SearchHandler{}

	now := time.Now()
	oldState := &searchState{
		keyword:   "old",
		updatedAt: now.Add(-20 * time.Minute),
	}
	recentState := &searchState{
		keyword:   "recent",
		updatedAt: now.Add(-5 * time.Minute),
	}

	handler.searchMu.Lock()
	if handler.searchCache == nil {
		handler.searchCache = make(map[int]*searchState)
	}
	handler.searchCache[1] = oldState
	handler.searchCache[2] = recentState
	handler.searchMu.Unlock()

	handler.searchMu.Lock()
	handler.cleanupSearchStateLocked()
	handler.searchMu.Unlock()

	_, ok1 := handler.getSearchState(1)
	if ok1 {
		t.Error("cleanupSearchStateLocked: old state should be removed")
	}

	_, ok2 := handler.getSearchState(2)
	if !ok2 {
		t.Error("cleanupSearchStateLocked: recent state should be kept")
	}
}

func TestSearchState_TracksCacheByPlatform(t *testing.T) {
	state := &searchState{}
	tracks := []platform.Track{{ID: "1", Title: "Song 1"}}
	state.setTracks("netease", tracks)

	cached, ok := state.getTracks("netease")
	if !ok {
		t.Fatal("getTracks: expected cache hit")
	}
	if len(cached) != 1 || cached[0].ID != "1" {
		t.Fatalf("getTracks: unexpected cached tracks: %+v", cached)
	}

	tracks[0].Title = "mutated"
	cached2, ok := state.getTracks("netease")
	if !ok {
		t.Fatal("getTracks: expected cache hit after source mutation")
	}
	if cached2[0].Title != "Song 1" {
		t.Errorf("setTracks should copy input slice, got title %q", cached2[0].Title)
	}
}

func TestSearchHandler_cleanupSearchStateLocked_MaxEntries(t *testing.T) {
	handler := &SearchHandler{}
	now := time.Now()

	handler.searchMu.Lock()
	handler.searchCache = make(map[int]*searchState, searchCacheMaxEntries+8)
	for i := 1; i <= searchCacheMaxEntries+8; i++ {
		handler.searchCache[i] = &searchState{
			keyword:   "k",
			updatedAt: now.Add(time.Duration(-i) * time.Second),
		}
	}
	handler.cleanupSearchStateLocked()
	finalSize := len(handler.searchCache)
	handler.searchMu.Unlock()

	if finalSize > searchCacheMaxEntries {
		t.Fatalf("cleanupSearchStateLocked: size=%d, want <= %d", finalSize, searchCacheMaxEntries)
	}
}
