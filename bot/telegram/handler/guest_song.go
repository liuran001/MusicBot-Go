package handler

import (
	"context"
	"strings"
	"time"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

func (h *GuestModeHandler) handleGuestSong(ctx context.Context, b *telego.Bot, message *telego.Message, content, guestQueryID string) {
	if h == nil || b == nil || message == nil {
		return
	}
	if h.PlatformManager == nil {
		h.answerGuest(ctx, b, guestQueryID, tr(ctx, "guest_service_unavailable"))
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		h.answerGuest(ctx, b, guestQueryID, tr(ctx, "guest_input_song_or_link"))
		return
	}
	userID, userName := guestRequester(message)
	if userID == 0 {
		h.answerGuest(ctx, b, guestQueryID, tr(ctx, "guest_requester_unknown"))
		return
	}

	baseText, requestedPlatform, qualityOverride := parseTrailingOptions(content, h.PlatformManager)
	if strings.TrimSpace(baseText) == "" {
		h.answerGuest(ctx, b, guestQueryID, tr(ctx, "guest_input_song_or_link"))
		return
	}
	resolvedText := resolveShortLinkText(ctx, h.PlatformManager, baseText)
	if platformName, trackID, matched := h.PlatformManager.MatchURL(resolvedText); matched {
		h.answerAndRunGuestTrack(ctx, b, message, guestQueryID, userID, userName, platformName, trackID, qualityOverride)
		return
	}
	if platformName, trackID, matched := matchTextTrack(h.PlatformManager, resolvedText); matched {
		h.answerAndRunGuestTrack(ctx, b, message, guestQueryID, userID, userName, platformName, trackID, qualityOverride)
		return
	}
	// Playlist / album URL (including short links that resolve to playlists).
	if platformName, playlistID, matched := matchPlaylistURL(ctx, h.PlatformManager, resolvedText); matched {
		h.handleGuestPlaylist(ctx, b, message, guestQueryID, platformName, playlistID, userID, qualityOverride)
		return
	}
	if strings.TrimSpace(requestedPlatform) != "" && !isBareNumericText(baseText) {
		if trackID, matched := matchPlatformTrack(ctx, h.PlatformManager, requestedPlatform, baseText); matched {
			h.answerAndRunGuestTrack(ctx, b, message, guestQueryID, userID, userName, requestedPlatform, trackID, qualityOverride)
			return
		}
	}

	h.answerAndRenderGuestSearch(ctx, b, message, guestQueryID, baseText, requestedPlatform, qualityOverride)
}

func (h *GuestModeHandler) answerAndRunGuestTrack(ctx context.Context, b *telego.Bot, message *telego.Message, guestQueryID string, userID int64, userName, platformName, trackID, qualityOverride string) {
	inlineMessageID := h.answerGuestPlaceholder(ctx, b, guestQueryID, tr(ctx, "wait_for_down"))
	if inlineMessageID == "" {
		return
	}
	chatID, isGroup := guestChatContext(message)
	go runInlineMediaFlow(detachContext(ctx), b, inlineMediaFlowDeps{Music: h.Music, RateLimiter: h.RateLimiter}, inlineMessageID, userID, userName, platformName, trackID, qualityOverride, chatID, isGroup)
}

// guestChatContext returns the originating group chat ID and whether it is a
// group, for guest messages (where Message.Chat.ID is available — unlike inline
// mode). Used to enable group favorites on the guest path.
func guestChatContext(message *telego.Message) (int64, bool) {
	if message == nil {
		return 0, false
	}
	return message.Chat.ID, message.Chat.Type != "private"
}

// handleGuestPlaylist fetches a playlist's tracks and presents them as a
// selectable search-result list in guest mode, reusing the same pagination
// infrastructure as keyword search.
func (h *GuestModeHandler) handleGuestPlaylist(ctx context.Context, b *telego.Bot, message *telego.Message, guestQueryID, platformName, playlistID string, userID int64, qualityOverride string) {
	inlineMessageID := h.answerGuestPlaceholder(ctx, b, guestQueryID, tr(ctx, "guest_loading_playlist"))
	if inlineMessageID == "" {
		return
	}
	go h.fetchAndRenderGuestPlaylist(detachContext(ctx), b, message, inlineMessageID, platformName, playlistID, userID, qualityOverride)
}

func (h *GuestModeHandler) fetchAndRenderGuestPlaylist(ctx context.Context, b *telego.Bot, message *telego.Message, inlineMessageID, platformName, playlistID string, userID int64, qualityOverride string) {
	if h == nil || h.PlatformManager == nil {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_service_unavailable"), nil, "")
		return
	}
	plat := h.PlatformManager.Get(platformName)
	if plat == nil {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_unsupported_platform"), nil, "")
		return
	}
	if !h.ResourceLimiter.AllowFor(ActionPlaylist, userID, message.Chat.ID, platformName) {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "err_rate_limited"), nil, "")
		return
	}
	playlist, err := plat.GetPlaylist(ctx, playlistID)
	if err != nil || playlist == nil || len(playlist.Tracks) == 0 {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_playlist_failed_or_empty"), nil, "")
		return
	}

	qualityValue := h.guestDefaultQuality(ctx, userID)
	if strings.TrimSpace(qualityOverride) != "" {
		qualityValue = qualityOverride
	}
	qualityValue = resolvePlatformQualityValue(ctx, h.repo(), botpkg.PluginScopeUser, userID, platformName, qualityValue, strings.TrimSpace(qualityOverride) != "")

	title := strings.TrimSpace(playlist.Title)
	if title == "" {
		title = tr(ctx, "guest_playlist_default_title")
	}

	collectionType := detectCollectionType(playlistID, playlist.URL)
	collectionLabel := collectionTypeLabel(ctx, collectionType)

	state := &searchState{
		keyword:          title,
		platform:         platformName,
		quality:          qualityValue,
		requesterID:      userID,
		limit:            len(playlist.Tracks),
		currentPage:      1,
		updatedAt:        time.Now(),
		tracksByPlatform: make(map[string][]platform.Track),
		hasMoreByPlat:    make(map[string]bool),
		unavailable:      make(map[string]bool),
		action:           "music",
		playlist:         playlist,
		collectionLabel:  collectionLabel,
	}
	state.setTracks(platformName, playlist.Tracks)

	token := h.guestSearchStore().store(state)
	text, keyboard := h.renderGuestSearchPage(ctx, state, token, 1)
	_ = h.editGuestInlineText(ctx, b, inlineMessageID, text, keyboard, telego.ModeMarkdownV2)
}

func (h *GuestModeHandler) answerAndRenderGuestSearch(ctx context.Context, b *telego.Bot, message *telego.Message, guestQueryID, keyword, requestedPlatform, qualityOverride string) {
	inlineMessageID := h.answerGuestPlaceholder(ctx, b, guestQueryID, tr(ctx, "searching"))
	if inlineMessageID == "" {
		return
	}
	// Remember the group chat so a later search-result tap (which goes through the
	// generic inline "music i" callback, carrying no chat context) can still
	// attach the group-favorite button to the sent song.
	chatID, isGroup := guestChatContext(message)
	rememberInlineChat(inlineMessageID, chatID, isGroup)
	go h.renderGuestSearch(detachContext(ctx), b, message, inlineMessageID, keyword, requestedPlatform, qualityOverride)
}

func (h *GuestModeHandler) renderGuestSearch(ctx context.Context, b *telego.Bot, message *telego.Message, inlineMessageID, keyword, requestedPlatform, qualityOverride string) {
	if h == nil || b == nil || message == nil || h.PlatformManager == nil || h.SearchHandler == nil {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_search_service_unavailable"), nil, "")
		return
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_input_keyword"), nil, "")
		return
	}
	userID, _ := guestRequester(message)
	if userID == 0 {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_requester_unknown"), nil, "")
		return
	}

	platformName := h.guestDefaultPlatform(ctx, userID)
	fallbackPlatform := strings.TrimSpace(h.FallbackPlatform)
	if fallbackPlatform == "" && h.SearchHandler != nil {
		fallbackPlatform = strings.TrimSpace(h.SearchHandler.FallbackPlatform)
	}
	if fallbackPlatform == "" {
		fallbackPlatform = "netease"
	}
	if strings.TrimSpace(requestedPlatform) != "" {
		platformName = requestedPlatform
		fallbackPlatform = ""
	}

	qualityValue := h.guestDefaultQuality(ctx, userID)
	if strings.TrimSpace(qualityOverride) != "" {
		qualityValue = strings.TrimSpace(qualityOverride)
	}

	biliFilter := true
	filterLabel := ""
	if enabled, supported, label := resolveSearchFilterEnabled(ctx, h.PlatformManager, h.repo(), platformName, botpkg.PluginScopeUser, userID); supported {
		biliFilter = enabled
		filterLabel = label
	}
	searchCtx := withSearchFilterContext(ctx, h.PlatformManager, platformName, biliFilter)
	primaryPlatform := platformName
	tracks, platformName, usedFallback, err := searchTracksWithFallbackLimitedFor(searchCtx, h.PlatformManager, h.ResourceLimiter, userID, message.Chat.ID, platformName, fallbackPlatform, keyword, h.guestSearchLimit, true)
	if err != nil {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, userVisibleSearchError(ctx, err), nil, "")
		return
	}
	qualityValue = resolvePlatformQualityValue(ctx, h.repo(), botpkg.PluginScopeUser, userID, platformName, qualityValue, strings.TrimSpace(qualityOverride) != "")
	limit := h.guestSearchLimit(platformName)
	unavailable := make(map[string]bool)
	if usedFallback && strings.TrimSpace(primaryPlatform) != "" {
		unavailable[primaryPlatform] = true
	}

	state := &searchState{
		keyword:          keyword,
		platform:         platformName,
		quality:          qualityValue,
		requesterID:      userID,
		limit:            limit,
		currentPage:      1,
		updatedAt:        time.Now(),
		tracksByPlatform: make(map[string][]platform.Track),
		hasMoreByPlat:    make(map[string]bool),
		unavailable:      unavailable,
		biliFilter:       biliFilter,
		searchFilterText: filterLabel,
		action:           "music",
	}
	if len(tracks) == 0 {
		if strings.TrimSpace(requestedPlatform) != "" {
			state.setUnavailable(platformName, true)
		}
		token := h.guestSearchStore().store(state)
		text := tr(ctx, "no_results")
		if state.platform != "" {
			text = tr(ctx, "guest_no_results_platform", map[string]any{"Platform": platformDisplayName(ctx, h.PlatformManager, state.platform)})
		}
		_, keyboard := h.renderGuestSearchPage(ctx, state, token, 1)
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, text, keyboard, "")
		return
	}
	state.setTracks(platformName, tracks)
	initialLimit := h.guestPageSize()
	state.setHasMore(platformName, len(tracks) >= initialLimit && initialLimit < limit)
	token := h.guestSearchStore().store(state)
	text, keyboard := h.renderGuestSearchPage(ctx, state, token, 1)
	_ = h.editGuestInlineText(ctx, b, inlineMessageID, text, keyboard, telego.ModeMarkdownV2)
}

func (h *GuestModeHandler) handleGuestLyric(ctx context.Context, b *telego.Bot, message *telego.Message, content, guestQueryID string) {
	if h == nil || b == nil || message == nil {
		return
	}
	keyword := strings.TrimSpace(strings.Replace(content, "歌词", "", 1))
	if keyword == "" {
		keyword = repliedMessageQuery(message.ReplyToMessage)
	}
	if keyword == "" {
		h.answerGuest(ctx, b, guestQueryID, tr(ctx, "guest_input_song_or_link"))
		return
	}
	if h.LyricHandler == nil || h.LyricHandler.PlatformManager == nil {
		h.answerGuest(ctx, b, guestQueryID, tr(ctx, "guest_lyric_service_unavailable"))
		return
	}
	inlineMessageID := h.answerGuestPlaceholder(ctx, b, guestQueryID, tr(ctx, "guest_fetching_lyric"))
	if inlineMessageID == "" {
		return
	}
	go h.fetchAndEditGuestLyric(detachContext(ctx), b, message, inlineMessageID, keyword)
}

func (h *GuestModeHandler) fetchAndEditGuestLyric(ctx context.Context, b *telego.Bot, message *telego.Message, inlineMessageID, keyword string) {
	requesterID, _ := guestRequester(message)
	platformName, trackID, ok := h.resolveGuestLyricTrack(ctx, message, keyword)
	if !ok {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_song_not_found"), nil, "")
		return
	}
	lh := h.LyricHandler
	plat := lh.PlatformManager.Get(platformName)
	if plat == nil || !plat.SupportsLyrics() {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_platform_no_lyrics"), nil, "")
		return
	}
	lyrics, err := getLyricsLimitedFor(ctx, h.ResourceLimiter, requesterID, message.Chat.ID, plat, platformName, trackID)
	if err != nil || lyrics == nil {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_get_lyric_failed"), nil, "")
		return
	}

	baseName := lh.buildLyricBaseName(ctx, plat, trackID)
	// Resolve the lyric-format default from the requester's OWN settings: model
	// the synthetic message as a private chat so resolveDefaultLyricFormat reads
	// user settings (From.ID) rather than group settings for chat 0.
	defaultFormat := lh.resolveDefaultLyricFormat(ctx, &telego.Message{Chat: telego.Chat{ID: requesterID, Type: telego.ChatTypePrivate}, From: &telego.User{ID: requesterID}})
	state := lyricRenderState{
		format:             defaultFormat,
		defaultFormat:      defaultFormat,
		includeTranslation: lyricFormatDefaultTranslation(defaultFormat),
		includeRoma:        false,
	}
	lh.editLyricDocumentInlineState(ctx, b, inlineMessageID, lyrics, baseName, platformName, trackID, state, requesterID)
}

func (h *GuestModeHandler) resolveGuestLyricTrack(ctx context.Context, message *telego.Message, keyword string) (platformName, trackID string, ok bool) {
	if h == nil || h.LyricHandler == nil || h.LyricHandler.PlatformManager == nil {
		return "", "", false
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "", "", false
	}
	resolvedText := resolveShortLinkText(ctx, h.LyricHandler.PlatformManager, keyword)
	if platformName, trackID, matched := h.LyricHandler.PlatformManager.MatchURL(resolvedText); matched {
		return platformName, trackID, true
	}
	if platformName, trackID, matched := matchTextTrack(h.LyricHandler.PlatformManager, resolvedText); matched {
		return platformName, trackID, true
	}
	if h.SearchHandler != nil && h.SearchHandler.PlatformManager != nil {
		base, requestedPlatform, _ := parseTrailingOptions(keyword, h.SearchHandler.PlatformManager)
		if strings.TrimSpace(base) == "" {
			return "", "", false
		}
		platformName := h.guestDefaultPlatform(ctx, guestUserID(message))
		fallbackPlatform := strings.TrimSpace(h.LyricHandler.FallbackPlatform)
		if fallbackPlatform == "" {
			fallbackPlatform = "netease"
		}
		if strings.TrimSpace(requestedPlatform) != "" {
			platformName = requestedPlatform
			fallbackPlatform = ""
		}
		tracks, matchedPlatform, _, err := searchTracksWithFallbackLimitedFor(ctx, h.LyricHandler.PlatformManager, h.ResourceLimiter, guestUserID(message), message.Chat.ID, platformName, fallbackPlatform, base, h.guestSearchLimit, true)
		if err != nil || len(tracks) == 0 {
			return "", "", false
		}
		first := tracks[0]
		resolvedPlatform := strings.TrimSpace(first.Platform)
		if resolvedPlatform == "" {
			resolvedPlatform = matchedPlatform
		}
		if resolvedPlatform == "" || strings.TrimSpace(first.ID) == "" {
			return "", "", false
		}
		return resolvedPlatform, first.ID, true
	}
	return h.LyricHandler.searchFirstTrackForLyric(ctx, message, keyword)
}

func (h *GuestModeHandler) handleGuestRecognize(ctx context.Context, b *telego.Bot, message *telego.Message, guestQueryID string) {
	if h == nil || b == nil || message == nil {
		return
	}
	inlineMessageID := h.answerGuestPlaceholder(ctx, b, guestQueryID, tr(ctx, "guest_recognizing"))
	if inlineMessageID == "" {
		return
	}
	go h.runGuestRecognize(detachContext(ctx), b, message, inlineMessageID)
}

func (h *GuestModeHandler) runGuestRecognize(ctx context.Context, b *telego.Bot, message *telego.Message, inlineMessageID string) {
	if h.RecognizeService == nil {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_recognize_service_unavailable"), nil, "")
		return
	}
	voiceMessage := message.ReplyToMessage
	if voiceMessage == nil || voiceMessage.Voice == nil {
		if message.Voice != nil {
			voiceMessage = message
		} else {
			_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_reply_voice_message"), nil, "")
			return
		}
	}
	if requesterID, _ := guestRequester(message); !h.ResourceLimiter.AllowFor(ActionRecognize, requesterID, message.Chat.ID, "") {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "err_rate_limited"), nil, "")
		return
	}
	fileBot := b
	if h.DownloadBot != nil {
		fileBot = h.DownloadBot
	}
	fileInfo, err := fileBot.GetFile(ctx, &telego.GetFileParams{FileID: voiceMessage.Voice.FileID})
	if err != nil || fileInfo == nil || fileInfo.FilePath == "" {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_get_voice_failed"), nil, "")
		return
	}
	if fileInfo.FileSize > 20*1024*1024 {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_voice_too_large"), nil, "")
		return
	}
	_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_downloading_voice"), nil, "")
	audioData, err := downloadTelegramFile(ctx, fileBot, fileInfo.FilePath)
	if err != nil {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_download_voice_failed"), nil, "")
		return
	}
	cacheDir := strings.TrimSpace(h.CacheDir)
	if cacheDir == "" {
		cacheDir = "./cache"
	}
	ensureDir(cacheDir)
	_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_converting_audio"), nil, "")
	mp3Data, err := convertToMP3(ctx, audioData, cacheDir)
	if err != nil {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_audio_convert_failed"), nil, "")
		return
	}
	_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_recognizing"), nil, "")
	result, err := h.RecognizeService.Recognize(ctx, mp3Data)
	if err != nil || result == nil || strings.TrimSpace(result.TrackID) == "" || strings.TrimSpace(result.Platform) == "" {
		_ = h.editGuestInlineText(ctx, b, inlineMessageID, tr(ctx, "guest_recognize_failed_short"), nil, "")
		return
	}
	userID, userName := guestRequester(message)
	chatID, isGroup := guestChatContext(message)
	runInlineMediaFlow(ctx, b, inlineMediaFlowDeps{Music: h.Music, RateLimiter: h.RateLimiter}, inlineMessageID, userID, userName, result.Platform, result.TrackID, "", chatID, isGroup)
}

func (h *GuestModeHandler) guestDefaultPlatform(ctx context.Context, userID int64) string {
	platformName := strings.TrimSpace(h.DefaultPlatform)
	if platformName == "" && h.SearchHandler != nil {
		platformName = strings.TrimSpace(h.SearchHandler.DefaultPlatform)
	}
	if platformName == "" {
		platformName = "netease"
	}
	if repo := h.repo(); repo != nil && userID != 0 {
		if settings, err := repo.GetUserSettings(ctx, userID); err == nil && settings != nil && strings.TrimSpace(settings.DefaultPlatform) != "" {
			platformName = settings.DefaultPlatform
		}
	}
	return platformName
}

func (h *GuestModeHandler) guestDefaultQuality(ctx context.Context, userID int64) string {
	qualityValue := strings.TrimSpace(h.DefaultQuality)
	if qualityValue == "" && h.Music != nil {
		qualityValue = strings.TrimSpace(h.Music.DefaultQuality)
	}
	if qualityValue == "" {
		qualityValue = "hires"
	}
	if repo := h.repo(); repo != nil && userID != 0 {
		if settings, err := repo.GetUserSettings(ctx, userID); err == nil && settings != nil && strings.TrimSpace(settings.DefaultQuality) != "" {
			qualityValue = settings.DefaultQuality
		}
	}
	return qualityValue
}

func guestRequester(message *telego.Message) (int64, string) {
	if message == nil || message.From == nil {
		return 0, ""
	}
	return message.From.ID, message.From.Username
}

func guestUserID(message *telego.Message) int64 {
	id, _ := guestRequester(message)
	return id
}
