package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/mymmrac/telego"
)

// Favorite-toggle callback data formats (space separated, <=64 bytes):
//
//	personal: "fav t u <platform> <trackID>"
//	group:    "fav t g <platform> <trackID> <chatID>"
//	token:    "fav tt <token>"   (fallback when the plaintext form is too long
//	                              or trackID carries unsafe characters)
//
// The clicker (callback From.ID) is always the acting user; it is never encoded.
// Group favorites carry the chat ID because a callback from an inline/guest
// message has no chat context — embedding it at build time is the only way to
// know which group's list to write.

type favoriteTogglePayload struct {
	scope    string
	platform string
	trackID  string
	chatID   int64
	storedAt time.Time
}

// 6h TTL: favorite buttons live on song messages that may be tapped long after
// sending, so the token outlives the 30m inline-callback store. Most tracks use
// short numeric IDs and never hit the token path at all.
var favoriteTogglePayloads = newTTLStore[favoriteTogglePayload](6 * time.Hour)
var favoriteTokenCounter uint64

func storeFavoriteTogglePayload(p favoriteTogglePayload) string {
	p.platform = strings.TrimSpace(p.platform)
	p.trackID = strings.TrimSpace(p.trackID)
	if p.platform == "" || p.trackID == "" {
		return ""
	}
	p.storedAt = time.Now()
	token := strconv.FormatUint(uint64(time.Now().UnixNano()), 36) + strconv.FormatUint(atomic.AddUint64(&favoriteTokenCounter, 1), 36)
	favoriteTogglePayloads.Store(token, p)
	return token
}

// buildFavoriteToggleData builds the callback data for a favorite toggle button.
// scope is botpkg.FavoriteScopeUser or FavoriteScopeGroup; chatID is required for
// the group scope. Returns "" when the inputs are unusable.
func buildFavoriteToggleData(scope, platformName, trackID string, chatID int64) string {
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	if platformName == "" || trackID == "" {
		return ""
	}
	if scope == botpkg.FavoriteScopeGroup {
		if chatID == 0 {
			return ""
		}
		if isInlineStartToken(platformName) && isInlineStartToken(trackID) {
			if data := fmt.Sprintf("fav t g %s %s %d", platformName, trackID, chatID); len(data) <= 64 {
				return data
			}
		}
	} else {
		if isInlineStartToken(platformName) && isInlineStartToken(trackID) {
			if data := fmt.Sprintf("fav t u %s %s", platformName, trackID); len(data) <= 64 {
				return data
			}
		}
	}
	if token := storeFavoriteTogglePayload(favoriteTogglePayload{scope: scope, platform: platformName, trackID: trackID, chatID: chatID}); token != "" {
		if data := "fav tt " + token; len(data) <= 64 {
			return data
		}
	}
	return ""
}

type parsedFavoriteToggle struct {
	scope    string
	platform string
	trackID  string
	chatID   int64
	ok       bool
	expired  bool
}

func parseFavoriteToggleData(args []string) parsedFavoriteToggle {
	if len(args) < 2 {
		return parsedFavoriteToggle{}
	}
	switch args[1] {
	case "tt":
		if len(args) < 3 {
			return parsedFavoriteToggle{}
		}
		p, ok := favoriteTogglePayloads.Load(strings.TrimSpace(args[2]))
		if !ok {
			return parsedFavoriteToggle{expired: true}
		}
		scope := botpkg.FavoriteScopeUser
		if p.scope == botpkg.FavoriteScopeGroup {
			scope = botpkg.FavoriteScopeGroup
		}
		return parsedFavoriteToggle{scope: scope, platform: p.platform, trackID: p.trackID, chatID: p.chatID, ok: true}
	case "t":
		// "fav t <u|g> <platform> <trackID> [chatID]"
		if len(args) < 5 {
			return parsedFavoriteToggle{}
		}
		scope := botpkg.FavoriteScopeUser
		if args[2] == "g" {
			scope = botpkg.FavoriteScopeGroup
		}
		res := parsedFavoriteToggle{scope: scope, platform: strings.TrimSpace(args[3]), trackID: strings.TrimSpace(args[4]), ok: true}
		if scope == botpkg.FavoriteScopeGroup {
			if len(args) < 6 {
				return parsedFavoriteToggle{}
			}
			cid, err := strconv.ParseInt(strings.TrimSpace(args[5]), 10, 64)
			if err != nil || cid == 0 {
				return parsedFavoriteToggle{}
			}
			res.chatID = cid
		}
		return res
	}
	return parsedFavoriteToggle{}
}

// favoriteMeta is the denormalized song metadata stored alongside a favorite.
type favoriteMeta struct {
	songName    string
	songArtists string
	songAlbum   string
	trackURL    string
}

// findSongMetaForFavorite resolves display metadata for a track, preferring the
// local song cache (no network) and falling back to a platform GetTrack call.
func findSongMetaForFavorite(ctx context.Context, repo botpkg.SongRepository, mgr platform.Manager, platformName, trackID string) favoriteMeta {
	var meta favoriteMeta
	if repo != nil {
		if s, err := repo.FindCachedSongMeta(ctx, platformName, trackID); err == nil && s != nil {
			meta.songName = s.SongName
			meta.songArtists = s.SongArtists
			meta.songAlbum = s.SongAlbum
			meta.trackURL = s.TrackURL
		}
	}
	if meta.songName == "" && mgr != nil {
		if plat := mgr.Get(platformName); plat != nil {
			if track, err := plat.GetTrack(ctx, trackID); err == nil && track != nil {
				var si botpkg.SongInfo
				fillSongInfoFromTrack(&si, track, platformName, trackID, nil)
				meta.songName = si.SongName
				meta.songArtists = si.SongArtists
				meta.songAlbum = si.SongAlbum
				meta.trackURL = si.TrackURL
			}
		}
	}
	return meta
}

// favoriteToggleOutcome reports what a toggle did. When deny is non-empty the
// action was blocked (e.g. group favorites disabled or admin-only); show deny to
// the user. Otherwise exactly one of added/removed is true.
type favoriteToggleOutcome struct {
	added    bool
	removed  bool
	deny     string
	songName string
}

// toggleFavorite is the shared add/remove core behind both the favorite button
// and the /fav command. It enforces group-favorites gating (enabled + admin-only)
// using the chat admin check, which degrades to "blocked" when it cannot be
// verified (e.g. guest mode, where the bot is not a group member).
func toggleFavorite(ctx context.Context, b *telego.Bot, repo botpkg.SongRepository, mgr platform.Manager, scopeType string, scopeID, clickerID int64, clickerName, platformName, trackID string) (favoriteToggleOutcome, error) {
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	if repo == nil || scopeID == 0 || clickerID == 0 || platformName == "" || trackID == "" {
		return favoriteToggleOutcome{}, fmt.Errorf("invalid favorite toggle request")
	}

	if scopeType == botpkg.FavoriteScopeGroup {
		mode := resolveGroupFavoritesMode(ctx, repo, scopeID)
		if !groupFavoritesAvailable(mode) {
			return favoriteToggleOutcome{deny: "群聊收藏未启用"}, nil
		}
		if mode == GroupFavAdmin && !isRequesterOrAdmin(ctx, b, scopeID, clickerID, 0) {
			return favoriteToggleOutcome{deny: "仅管理员可收藏到群聊"}, nil
		}
	}

	favorited, err := repo.IsFavorited(ctx, scopeType, scopeID, platformName, trackID)
	if err != nil {
		return favoriteToggleOutcome{}, err
	}
	if favorited {
		if err := repo.RemoveFavorite(ctx, scopeType, scopeID, platformName, trackID); err != nil {
			return favoriteToggleOutcome{}, err
		}
		return favoriteToggleOutcome{removed: true}, nil
	}

	meta := findSongMetaForFavorite(ctx, repo, mgr, platformName, trackID)
	fav := &botpkg.Favorite{
		ScopeType:     scopeType,
		ScopeID:       scopeID,
		Platform:      platformName,
		TrackID:       trackID,
		AddedByUserID: clickerID,
		AddedByName:   strings.TrimSpace(clickerName),
		SongName:      meta.songName,
		SongArtists:   meta.songArtists,
		SongAlbum:     meta.songAlbum,
		TrackURL:      meta.trackURL,
	}
	if err := repo.AddFavorite(ctx, fav); err != nil {
		return favoriteToggleOutcome{}, err
	}
	return favoriteToggleOutcome{added: true, songName: meta.songName}, nil
}

// favoriteToggleMessage renders the user-facing toast for a toggle outcome.
func favoriteToggleMessage(out favoriteToggleOutcome, scopeType string) string {
	if out.deny != "" {
		return out.deny
	}
	group := scopeType == botpkg.FavoriteScopeGroup
	if out.added {
		if group {
			return "⭐ 已收藏到群聊"
		}
		return "⭐ 已收藏"
	}
	if out.removed {
		if group {
			return "已从群聊取消收藏"
		}
		return "已取消收藏"
	}
	return ""
}

// callbackUserDisplayName builds a human label for a user (first+last, else
// username), used as the group-favorite collector name.
func callbackUserDisplayName(user *telego.User) string {
	if user == nil {
		return ""
	}
	name := strings.TrimSpace(strings.TrimSpace(user.FirstName) + " " + strings.TrimSpace(user.LastName))
	if name == "" {
		name = strings.TrimSpace(user.Username)
	}
	return name
}

// FavoriteCallbackHandler handles favorite button taps ("fav ...") and favorite
// list interactions ("favm ..."). The list verbs are implemented in
// favorite_list.go.
type FavoriteCallbackHandler struct {
	Repo            botpkg.SongRepository
	PlatformManager platform.Manager
	RateLimiter     *telegram.RateLimiter
	Music           *MusicHandler
	Favorites       *FavoritesHandler
	BotName         string
	Logger          botpkg.Logger
	PageSize        int
}

func (h *FavoriteCallbackHandler) answer(ctx context.Context, b *telego.Bot, callbackQueryID, text string) {
	params := &telego.AnswerCallbackQueryParams{CallbackQueryID: callbackQueryID}
	if text != "" {
		params.Text = text
	}
	_ = b.AnswerCallbackQuery(ctx, params)
}

func (h *FavoriteCallbackHandler) Handle(ctx context.Context, b *telego.Bot, update *telego.Update) {
	if update == nil || update.CallbackQuery == nil {
		return
	}
	query := update.CallbackQuery
	args := strings.Fields(query.Data)
	if len(args) < 1 {
		h.answer(ctx, b, query.ID, "")
		return
	}
	switch args[0] {
	case "fav":
		h.handleToggle(ctx, b, query, args)
	case "favm":
		h.handleListCallback(ctx, b, query, args)
	default:
		h.answer(ctx, b, query.ID, "")
	}
}

func (h *FavoriteCallbackHandler) handleToggle(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, args []string) {
	parsed := parseFavoriteToggleData(args)
	if parsed.expired {
		h.answer(ctx, b, query.ID, "操作已过期，请重新发送歌曲")
		return
	}
	if !parsed.ok {
		h.answer(ctx, b, query.ID, "")
		return
	}
	clicker := int64(0)
	if query.From.ID != 0 {
		clicker = query.From.ID
	}
	if clicker == 0 {
		h.answer(ctx, b, query.ID, "")
		return
	}
	scopeType := parsed.scope
	scopeID := clicker
	if scopeType == botpkg.FavoriteScopeGroup {
		scopeID = parsed.chatID
	}
	out, err := toggleFavorite(ctx, b, h.Repo, h.PlatformManager, scopeType, scopeID, clicker, callbackUserDisplayName(&query.From), parsed.platform, parsed.trackID)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Warn("favorite toggle failed", "platform", parsed.platform, "trackID", parsed.trackID, "error", err)
		}
		h.answer(ctx, b, query.ID, "操作失败，请稍后再试")
		return
	}
	h.answer(ctx, b, query.ID, favoriteToggleMessage(out, scopeType))
}

func (h *FavoriteCallbackHandler) handleListCallback(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, args []string) {
	if h.Favorites == nil {
		h.answer(ctx, b, query.ID, "")
		return
	}
	h.Favorites.handleListCallback(ctx, b, query, args)
}
