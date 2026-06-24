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
	songName        string
	songArtists     string
	songAlbum       string
	trackURL        string
	songArtistsURLs string
}

// findSongMetaForFavorite resolves display metadata for a track, preferring the
// local song cache (no network). It falls back to the platform's GetTrack when
// the cache lacks the song name OR the web URL, so the favorites list can
// hyperlink the song/artists to their pages using each platform's own URL logic
// (the same source buildMusicCaption uses). Favoriting is infrequent, so the
// extra call is acceptable.
func findSongMetaForFavorite(ctx context.Context, repo botpkg.SongRepository, mgr platform.Manager, platformName, trackID string) favoriteMeta {
	var meta favoriteMeta
	if repo != nil {
		if s, err := repo.FindCachedSongMeta(ctx, platformName, trackID); err == nil && s != nil {
			meta.songName = s.SongName
			meta.songArtists = s.SongArtists
			meta.songAlbum = s.SongAlbum
			meta.trackURL = s.TrackURL
			meta.songArtistsURLs = s.SongArtistsURLs
		}
	}
	if (meta.songName == "" || meta.trackURL == "") && mgr != nil {
		if plat := mgr.Get(platformName); plat != nil {
			if track, err := plat.GetTrack(ctx, trackID); err == nil && track != nil {
				var si botpkg.SongInfo
				fillSongInfoFromTrack(&si, track, platformName, trackID, nil)
				if meta.songName == "" {
					meta.songName = si.SongName
					meta.songArtists = si.SongArtists
					meta.songAlbum = si.SongAlbum
				}
				if meta.trackURL == "" {
					meta.trackURL = si.TrackURL
				}
				if meta.songArtistsURLs == "" {
					meta.songArtistsURLs = si.SongArtistsURLs
				}
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

// resolveFavoriteToggleDeny enforces group-favorites gating (enabled +
// admin-only), returning a non-empty user-facing reason when the action is
// blocked. The admin check degrades to "blocked" when it cannot be verified
// (e.g. guest mode, where the bot is not a group member).
func resolveFavoriteToggleDeny(ctx context.Context, b *telego.Bot, repo botpkg.SongRepository, scopeType string, scopeID, clickerID int64) string {
	if scopeType != botpkg.FavoriteScopeGroup {
		return ""
	}
	mode := resolveGroupFavoritesMode(ctx, repo, scopeID)
	if !groupFavoritesAvailable(mode) {
		return tr(ctx, "fav_group_disabled")
	}
	if mode == GroupFavAdmin && !isRequesterOrAdmin(ctx, b, scopeID, clickerID, 0) {
		return tr(ctx, "fav_group_admin_only")
	}
	return ""
}

// addFavoriteWithMeta resolves song metadata and inserts the favorite.
func addFavoriteWithMeta(ctx context.Context, repo botpkg.SongRepository, mgr platform.Manager, scopeType string, scopeID, clickerID int64, clickerName, platformName, trackID string) (string, error) {
	meta := findSongMetaForFavorite(ctx, repo, mgr, platformName, trackID)
	fav := &botpkg.Favorite{
		ScopeType:       scopeType,
		ScopeID:         scopeID,
		Platform:        platformName,
		TrackID:         trackID,
		AddedByUserID:   clickerID,
		AddedByName:     strings.TrimSpace(clickerName),
		SongName:        meta.songName,
		SongArtists:     meta.songArtists,
		SongAlbum:       meta.songAlbum,
		TrackURL:        meta.trackURL,
		SongArtistsURLs: meta.songArtistsURLs,
	}
	return meta.songName, repo.AddFavorite(ctx, fav)
}

// toggleFavorite is the immediate add/remove core behind the /fav command. The
// favorite button uses a two-step removal (see handleToggle), but the command is
// explicit so it toggles in one step.
func toggleFavorite(ctx context.Context, b *telego.Bot, repo botpkg.SongRepository, mgr platform.Manager, scopeType string, scopeID, clickerID int64, clickerName, platformName, trackID string) (favoriteToggleOutcome, error) {
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	if repo == nil || scopeID == 0 || clickerID == 0 || platformName == "" || trackID == "" {
		return favoriteToggleOutcome{}, fmt.Errorf("invalid favorite toggle request")
	}

	if deny := resolveFavoriteToggleDeny(ctx, b, repo, scopeType, scopeID, clickerID); deny != "" {
		return favoriteToggleOutcome{deny: deny}, nil
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

	songName, err := addFavoriteWithMeta(ctx, repo, mgr, scopeType, scopeID, clickerID, clickerName, platformName, trackID)
	if err != nil {
		return favoriteToggleOutcome{}, err
	}
	return favoriteToggleOutcome{added: true, songName: songName}, nil
}

// favoriteToggleMessage renders the user-facing toast for a toggle outcome.
func favoriteToggleMessage(ctx context.Context, out favoriteToggleOutcome, scopeType string) string {
	if out.deny != "" {
		return out.deny
	}
	group := scopeType == botpkg.FavoriteScopeGroup
	if out.added {
		if group {
			return tr(ctx, "fav_added_group")
		}
		return tr(ctx, "fav_added")
	}
	if out.removed {
		if group {
			return tr(ctx, "fav_removed_group")
		}
		return tr(ctx, "fav_removed")
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

func (h *FavoriteCallbackHandler) answerAlert(ctx context.Context, b *telego.Bot, callbackQueryID, text string) {
	_ = b.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{CallbackQueryID: callbackQueryID, Text: text, ShowAlert: true})
}

// favoriteUnfavoritePending tracks a pending "tap again to unfavorite"
// confirmation, keyed by (clicker, scope, track). The 10s TTL is the confirm
// window: a tap while already favorited only arms this and prompts; a second tap
// within the window actually removes. Adding a favorite needs no confirmation.
var favoriteUnfavoritePending = newTTLStore[struct{}](10 * time.Second)

func favoriteUnfavoriteKey(clickerID int64, scopeType string, scopeID int64, platformName, trackID string) string {
	return fmt.Sprintf("%d|%s|%d|%s|%s", clickerID, scopeType, scopeID, platformName, trackID)
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
		h.answer(ctx, b, query.ID, tr(ctx, "fav_expired"))
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
	group := scopeType == botpkg.FavoriteScopeGroup

	if deny := resolveFavoriteToggleDeny(ctx, b, h.Repo, scopeType, scopeID, clicker); deny != "" {
		h.answer(ctx, b, query.ID, deny)
		return
	}

	favorited, err := h.Repo.IsFavorited(ctx, scopeType, scopeID, parsed.platform, parsed.trackID)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Warn("favorite toggle failed", "platform", parsed.platform, "trackID", parsed.trackID, "error", err)
		}
		h.answer(ctx, b, query.ID, tr(ctx, "fav_action_failed"))
		return
	}

	// Already favorited: require a two-step confirmation to remove, so a single
	// mis-tap on the (visually identical) button can't silently unfavorite.
	if favorited {
		// Group favorites: only the collector or an admin may remove (matching the
		// list's permission). In guest mode the admin check fails, so it degrades
		// to collector-only.
		if group {
			fav, _ := h.Repo.GetFavorite(ctx, scopeType, scopeID, parsed.platform, parsed.trackID)
			if (fav == nil || fav.AddedByUserID != clicker) && !isRequesterOrAdmin(ctx, b, scopeID, clicker, 0) {
				h.answer(ctx, b, query.ID, tr(ctx, "fav_group_remove_denied"))
				return
			}
		}
		key := favoriteUnfavoriteKey(clicker, scopeType, scopeID, parsed.platform, parsed.trackID)
		if _, pending := favoriteUnfavoritePending.Load(key); pending {
			favoriteUnfavoritePending.Delete(key)
			if err := h.Repo.RemoveFavorite(ctx, scopeType, scopeID, parsed.platform, parsed.trackID); err != nil {
				h.answer(ctx, b, query.ID, tr(ctx, "fav_action_failed"))
				return
			}
			if group {
				h.answer(ctx, b, query.ID, tr(ctx, "fav_removed_group"))
			} else {
				h.answer(ctx, b, query.ID, tr(ctx, "fav_removed"))
			}
			return
		}
		favoriteUnfavoritePending.Store(key, struct{}{})
		if group {
			h.answerAlert(ctx, b, query.ID, tr(ctx, "fav_added_group_confirm"))
		} else {
			h.answerAlert(ctx, b, query.ID, tr(ctx, "fav_added_confirm"))
		}
		return
	}

	// Not favorited: add immediately, no confirmation.
	if _, err := addFavoriteWithMeta(ctx, h.Repo, h.PlatformManager, scopeType, scopeID, clicker, callbackUserDisplayName(&query.From), parsed.platform, parsed.trackID); err != nil {
		if h.Logger != nil {
			h.Logger.Warn("favorite add failed", "platform", parsed.platform, "trackID", parsed.trackID, "error", err)
		}
		h.answer(ctx, b, query.ID, tr(ctx, "fav_action_failed"))
		return
	}
	if group {
		h.answer(ctx, b, query.ID, tr(ctx, "fav_added_group"))
	} else {
		h.answer(ctx, b, query.ID, tr(ctx, "fav_added"))
	}
}

func (h *FavoriteCallbackHandler) handleListCallback(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery, args []string) {
	if h.Favorites == nil {
		h.answer(ctx, b, query.ID, "")
		return
	}
	h.Favorites.handleListCallback(ctx, b, query, args)
}
