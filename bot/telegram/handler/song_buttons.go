package handler

import (
	"context"
	"fmt"
	"strings"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

// songButtonOptions describes the context needed to render the bottom button row
// under a sent song. The same row appears in three places — a normal chat audio
// message, an inline cached-document result, and an inline/guest edit-to-audio —
// which differ mainly in whether the lyrics button can post a new message
// (private chat) or must deep-link to the bot's private chat
// (groups/channels/inline/guest), and whether a group favorite button is
// available (group chat with a known chat ID).
type songButtonOptions struct {
	platformName    string
	trackID         string
	trackURL        string
	quality         string
	requesterID     int64
	botName         string
	platformManager platform.Manager
	// lyricsAvailable is nil when unknown. A false value means the platform
	// explicitly reported that this track has no lyrics.
	lyricsAvailable *bool
	// inlineContext is true for inline-mode and guest-mode messages, where the
	// bot cannot send a new message to the chat.
	inlineContext bool
	// chatID is the originating chat (0 when unknown, e.g. inline mode). Required
	// for the group favorite button.
	chatID int64
	// isGroup reports whether the originating chat is not private.
	isGroup bool
}

// buildSongBottomKeyboard renders the bottom button row for a sent song:
//
//	[ 发送到聊天… ]
//	[ 📜 歌词 ] [ ⭐ 收藏 ] [ 👥 群收藏 (group only) ]
//
// It returns nil when no button can be built. The caller decides whether buttons
// are shown at all (the "展示歌曲底部按钮" master toggle).
func buildSongBottomKeyboard(ctx context.Context, repo botpkg.SongRepository, opts songButtonOptions) *telego.InlineKeyboardMarkup {
	var rows [][]telego.InlineKeyboardButton

	// Row 1: send-to-chat (switch inline query).
	if fwd := buildForwardKeyboard(ctx, opts.trackURL, opts.platformName, opts.trackID); fwd != nil && len(fwd.InlineKeyboard) > 0 {
		rows = append(rows, fwd.InlineKeyboard[0])
	}

	// Row 2: lyrics + favorites.
	var actionRow []telego.InlineKeyboardButton
	if songLyricsButtonAvailable(opts) {
		if songLyricsButtonUsesDeepLink(opts) {
			if link := buildLyricDeepLink(opts.botName, opts.platformName, opts.trackID); link != "" {
				actionRow = append(actionRow, telego.InlineKeyboardButton{Text: tr(ctx, "cb_lyric_btn"), URL: link})
			}
		} else {
			if data := buildLyricButtonCallbackData(opts.platformName, opts.trackID, opts.quality, opts.requesterID); data != "" {
				actionRow = append(actionRow, telego.InlineKeyboardButton{Text: tr(ctx, "cb_lyric_btn"), CallbackData: data})
			}
		}
	}
	if data := buildFavoriteToggleData(botpkg.FavoriteScopeUser, opts.platformName, opts.trackID, 0); data != "" {
		actionRow = append(actionRow, telego.InlineKeyboardButton{Text: tr(ctx, "cb_favorite_btn"), CallbackData: data})
	}
	if opts.isGroup && opts.chatID != 0 && groupFavoritesAvailable(resolveGroupFavoritesMode(ctx, repo, opts.chatID)) {
		if data := buildFavoriteToggleData(botpkg.FavoriteScopeGroup, opts.platformName, opts.trackID, opts.chatID); data != "" {
			actionRow = append(actionRow, telego.InlineKeyboardButton{Text: tr(ctx, "cb_group_favorite_btn"), CallbackData: data})
		}
	}
	if len(actionRow) > 0 {
		rows = append(rows, actionRow)
	}

	if len(rows) == 0 {
		return nil
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func songLyricsButtonAvailable(opts songButtonOptions) bool {
	if opts.platformManager != nil {
		if plat := opts.platformManager.Get(opts.platformName); plat != nil && !plat.SupportsLyrics() {
			return false
		}
	}
	return opts.lyricsAvailable == nil || *opts.lyricsAvailable
}

func songLyricsButtonUsesDeepLink(opts songButtonOptions) bool {
	return opts.inlineContext || opts.isGroup
}

// buildLyricButtonCallbackData builds the callback data for the in-chat lyrics
// button, reusing the search-result lyric callback format
// ("lyric <platform> <trackID> <quality> <requesterID>"). Returns "" when the
// fields are unsafe or the data would exceed Telegram's 64-byte limit (the
// search-result lyric path has no token fallback, so the button is omitted).
func buildLyricButtonCallbackData(platformName, trackID, quality string, requesterID int64) string {
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	quality = strings.TrimSpace(quality)
	if quality == "" {
		quality = "hires"
	}
	if platformName == "" || trackID == "" {
		return ""
	}
	if !isInlineStartToken(platformName) || !isInlineStartToken(trackID) || !isInlineStartToken(quality) {
		return ""
	}
	data := fmt.Sprintf("lyric %s %s %s %d", platformName, trackID, quality, requesterID)
	if len(data) <= 64 {
		return data
	}
	return ""
}

// buildLyricDeepLink builds a "https://t.me/<bot>?start=lyric_<platform>_<trackID>"
// URL used by the inline/guest lyrics button to jump to the bot's private chat
// and download lyrics there. Returns "" when fields are unsafe or too long.
func buildLyricDeepLink(botName, platformName, trackID string) string {
	name := strings.TrimPrefix(strings.TrimSpace(botName), "@")
	platformName = strings.TrimSpace(platformName)
	trackID = strings.TrimSpace(trackID)
	if name == "" || platformName == "" || trackID == "" {
		return ""
	}
	if !isInlineStartToken(platformName) || !isInlineStartToken(trackID) {
		return ""
	}
	payload := "lyric_" + platformName + "_" + trackID
	if len(payload) > 64 {
		return ""
	}
	return fmt.Sprintf("https://t.me/%s?start=%s", name, payload)
}

// parseLyricStartParameter parses a "lyric_<platform>_<trackID>" /start deep-link
// payload. trackID may itself contain underscores, so only the first two
// separators are split.
func parseLyricStartParameter(value string) (platformName, trackID string, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", false
	}
	parts := strings.SplitN(value, "_", 3)
	if len(parts) < 3 || parts[0] != "lyric" {
		return "", "", false
	}
	platformName = parts[1]
	trackID = parts[2]
	if !isInlineStartToken(platformName) || !isInlineStartToken(trackID) {
		return "", "", false
	}
	return platformName, trackID, true
}
