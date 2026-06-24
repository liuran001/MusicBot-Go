package handler

import (
	"context"
	"strings"
	"testing"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/mymmrac/telego"
)

func TestFavoriteToggleCallbackRoundTrip(t *testing.T) {
	// Personal.
	data := buildFavoriteToggleData(botpkg.FavoriteScopeUser, "netease", "12345", 0)
	if data != "fav t u netease 12345" {
		t.Fatalf("unexpected personal data: %q", data)
	}
	p := parseFavoriteToggleData(strings.Fields(data))
	if !p.ok || p.scope != botpkg.FavoriteScopeUser || p.platform != "netease" || p.trackID != "12345" {
		t.Fatalf("unexpected parse: %+v", p)
	}

	// Group carries the chat ID.
	data = buildFavoriteToggleData(botpkg.FavoriteScopeGroup, "qqmusic", "abcDEF", -1001234567890)
	p = parseFavoriteToggleData(strings.Fields(data))
	if !p.ok || p.scope != botpkg.FavoriteScopeGroup || p.trackID != "abcDEF" || p.chatID != -1001234567890 {
		t.Fatalf("unexpected group parse: %+v", p)
	}

	// Group without chat ID is rejected at build time.
	if got := buildFavoriteToggleData(botpkg.FavoriteScopeGroup, "netease", "1", 0); got != "" {
		t.Fatalf("expected empty group data without chatID, got %q", got)
	}
}

func TestFavoriteToggleCallbackTokenFallback(t *testing.T) {
	// A trackID with characters unsafe for the plaintext form must round-trip
	// through the token store.
	longID := "id with spaces/and+special"
	data := buildFavoriteToggleData(botpkg.FavoriteScopeUser, "netease", longID, 0)
	if data == "" {
		t.Fatalf("expected token-based data for unsafe trackID")
	}
	if got := strings.Fields(data); got[1] != "tt" {
		t.Fatalf("expected token form, got %q", data)
	}
	p := parseFavoriteToggleData(strings.Fields(data))
	if !p.ok || p.trackID != longID || p.scope != botpkg.FavoriteScopeUser {
		t.Fatalf("token payload did not round-trip: %+v", p)
	}

	// An unknown token reports expired rather than ok.
	p = parseFavoriteToggleData([]string{"fav", "tt", "nonexistent-token"})
	if p.ok || !p.expired {
		t.Fatalf("expected expired for unknown token, got %+v", p)
	}
}

func TestToggleFavoritePersonal(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()
	const uid int64 = 42

	out, err := toggleFavorite(ctx, nil, repo, nil, botpkg.FavoriteScopeUser, uid, uid, "Tester", "netease", "777")
	if err != nil || !out.added {
		t.Fatalf("expected added, got %+v err=%v", out, err)
	}
	if msg := favoriteToggleMessage(out, botpkg.FavoriteScopeUser); msg != "⭐ 已收藏" {
		t.Fatalf("unexpected add message: %q", msg)
	}
	if ok, _ := repo.IsFavorited(ctx, botpkg.FavoriteScopeUser, uid, "netease", "777"); !ok {
		t.Fatalf("expected favorited in repo")
	}

	out, err = toggleFavorite(ctx, nil, repo, nil, botpkg.FavoriteScopeUser, uid, uid, "Tester", "netease", "777")
	if err != nil || !out.removed {
		t.Fatalf("expected removed, got %+v err=%v", out, err)
	}
	if msg := favoriteToggleMessage(out, botpkg.FavoriteScopeUser); msg != "已取消收藏" {
		t.Fatalf("unexpected remove message: %q", msg)
	}
}

func TestToggleFavoriteGroupDisabled(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()
	const gid int64 = -100
	_ = repo.SetPluginSetting(ctx, botpkg.PluginScopeGroup, gid, GroupFavPlugin, GroupFavKey, GroupFavOff)

	out, err := toggleFavorite(ctx, nil, repo, nil, botpkg.FavoriteScopeGroup, gid, 7, "X", "netease", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.deny != "群聊收藏未启用" {
		t.Fatalf("expected deny for disabled group favorites, got %+v", out)
	}
	if n, _ := repo.CountFavorites(ctx, botpkg.FavoriteScopeGroup, gid); n != 0 {
		t.Fatalf("expected no favorite stored, got %d", n)
	}
}

func TestToggleFavoriteGroupAdminOnlyDeniesNonAdmin(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()
	const gid int64 = -200
	_ = repo.SetPluginSetting(ctx, botpkg.PluginScopeGroup, gid, GroupFavPlugin, GroupFavKey, GroupFavAdmin)

	// b is nil, so the admin check cannot succeed: a non-admin is denied.
	out, err := toggleFavorite(ctx, nil, repo, nil, botpkg.FavoriteScopeGroup, gid, 7, "X", "netease", "1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.deny != "仅管理员可收藏到群聊" {
		t.Fatalf("expected admin-only deny, got %+v", out)
	}
}

func TestParseLyricStartParameter(t *testing.T) {
	platformName, trackID, ok := parseLyricStartParameter("lyric_netease_12345")
	if !ok || platformName != "netease" || trackID != "12345" {
		t.Fatalf("unexpected parse: %s %s %v", platformName, trackID, ok)
	}
	// trackID may contain underscores.
	_, trackID, ok = parseLyricStartParameter("lyric_qqmusic_abc_def")
	if !ok || trackID != "abc_def" {
		t.Fatalf("expected trackID abc_def, got %q ok=%v", trackID, ok)
	}
	if _, _, ok := parseLyricStartParameter("cache_netease_1"); ok {
		t.Fatalf("non-lyric payload should not parse")
	}
}

func TestBuildLyricDeepLinkAndButton(t *testing.T) {
	link := buildLyricDeepLink("@MyBot", "netease", "12345")
	if link != "https://t.me/MyBot?start=lyric_netease_12345" {
		t.Fatalf("unexpected deep link: %q", link)
	}
	data := buildLyricButtonCallbackData("netease", "12345", "hires", 99)
	if data != "lyric netease 12345 hires 99" {
		t.Fatalf("unexpected lyric callback data: %q", data)
	}
}

func TestFavoriteListViewSendAndManageModes(t *testing.T) {
	repo := newStubRepo()
	ctx := context.Background()
	const uid int64 = 7
	if err := repo.AddFavorite(ctx, &botpkg.Favorite{ScopeType: botpkg.FavoriteScopeUser, ScopeID: uid, Platform: "netease", TrackID: "555", AddedByUserID: uid, SongName: "Song", SongArtists: "Artist"}); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	h := &FavoritesHandler{Repo: repo, PageSize: 8}

	scan := func(markup *telego.InlineKeyboardMarkup) (send, remove, manageEntry bool) {
		if markup == nil {
			return
		}
		for _, row := range markup.InlineKeyboard {
			for _, btn := range row {
				switch {
				case strings.HasPrefix(btn.CallbackData, "favm s TOK u 1 0"):
					send = true
				case strings.HasPrefix(btn.CallbackData, "favm x TOK u 1 0"):
					remove = true
				case strings.HasPrefix(btn.CallbackData, "favm n TOK u 1 m"):
					manageEntry = true
				}
			}
		}
		return
	}

	// Normal mode: a send button + a "manage" entry, but NO inline remove button.
	_, normal := h.buildListView(ctx, favoriteListContext{token: "TOK", requesterID: uid, view: "u", page: 1})
	if send, remove, manageEntry := scan(normal); !send || remove || !manageEntry {
		t.Fatalf("normal mode: send=%v remove=%v manageEntry=%v", send, remove, manageEntry)
	}

	// Manage mode: per-song delete buttons, no send.
	_, manage := h.buildListView(ctx, favoriteListContext{token: "TOK", requesterID: uid, view: "u", page: 1, manage: true})
	if send, remove, _ := scan(manage); send || !remove {
		t.Fatalf("manage mode: send=%v remove=%v", send, remove)
	}
}

func TestFavoriteSongHTML(t *testing.T) {
	// Track + artist links, with HTML escaping of the names.
	fav := &botpkg.Favorite{
		Platform:        "netease",
		TrackID:         "111",
		SongName:        "A<B",
		SongArtists:     "X&Y/Z",
		TrackURL:        "https://example.com/song?a=1&b=2",
		SongArtistsURLs: "https://example.com/x,https://example.com/z",
	}
	got := favoriteSongHTML(fav)
	want := `<a href="https://example.com/song?a=1&amp;b=2">A&lt;B</a> - <a href="https://example.com/x">X&amp;Y</a> / <a href="https://example.com/z">Z</a>`
	if got != want {
		t.Fatalf("favoriteSongHTML mismatch:\n got: %s\nwant: %s", got, want)
	}

	// netease without a stored TrackURL falls back to a constructed song URL.
	fav2 := &botpkg.Favorite{Platform: "netease", TrackID: "222", SongName: "Song", SongArtists: "Artist"}
	if got := favoriteSongHTML(fav2); !strings.Contains(got, `<a href="https://music.163.com/song?id=222">Song</a>`) {
		t.Fatalf("expected netease fallback link, got %s", got)
	}

	// No URL and non-netease: plain escaped text, no anchor.
	fav3 := &botpkg.Favorite{Platform: "qqmusic", TrackID: "333", SongName: "Name", SongArtists: "Artist"}
	if got := favoriteSongHTML(fav3); strings.Contains(got, "<a ") {
		t.Fatalf("expected no link without URL, got %s", got)
	}
}

func TestExtractGroupScopeToken(t *testing.T) {
	cases := []struct {
		in       string
		wantArgs string
		wantGrp  bool
	}{
		{"", "", false},
		{"晴天", "晴天", false},
		{"group", "", true},
		{"群", "", true},
		{"group 晴天", "晴天", true},
		{"晴天 群聊", "晴天", true},
	}
	for _, c := range cases {
		gotArgs, gotGrp := extractGroupScopeToken(c.in)
		if gotArgs != c.wantArgs || gotGrp != c.wantGrp {
			t.Fatalf("extractGroupScopeToken(%q) = (%q,%v), want (%q,%v)", c.in, gotArgs, gotGrp, c.wantArgs, c.wantGrp)
		}
	}
}
