package kugou

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/guohuiyuan/music-lib/model"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type KugouPlatform struct {
	client *Client
}

type kugouDownloadCandidate struct {
	Quality platform.Quality
	Song    *model.Song
	Source  string
}

func NewPlatform(client *Client) *KugouPlatform {
	return &KugouPlatform{client: client}
}

func (k *KugouPlatform) Name() string {
	return "kugou"
}

func (k *KugouPlatform) SupportsDownload() bool {
	return true
}

func (k *KugouPlatform) SupportsSearch() bool {
	return true
}

func (k *KugouPlatform) SupportsLyrics() bool {
	return true
}

func (k *KugouPlatform) SupportsRecognition() bool {
	return false
}

func (k *KugouPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Download: true,
		Search:   true,
		Lyrics:   true,
		HiRes:    true,
	}
}

func (k *KugouPlatform) Metadata() platform.Meta {
	return platform.Meta{
		Name:          "kugou",
		DisplayName:   "酷狗音乐",
		Emoji:         "🐶",
		Aliases:       []string{"kugou", "kg", "酷狗", "酷狗音乐"},
		AllowGroupURL: true,
	}
}

func (k *KugouPlatform) CheckCookie(ctx context.Context) (platform.CookieCheckResult, error) {
	if k == nil || k.client == nil {
		return platform.CookieCheckResult{OK: false, Message: "kugou client unavailable"}, nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ok, err := k.client.CheckCookie(checkCtx)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("Cookie 校验失败: %v", err)}, nil
	}
	if !ok {
		return platform.CookieCheckResult{OK: false, Message: "Cookie 未检测到 VIP 能力或未配置"}, nil
	}
	tracks, err := k.Search(checkCtx, "周杰伦", 1)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("VIP 已识别，但测试搜索失败: %v", err)}, nil
	}
	if len(tracks) == 0 || strings.TrimSpace(tracks[0].ID) == "" {
		return platform.CookieCheckResult{OK: false, Message: "VIP 已识别，但测试曲目获取失败"}, nil
	}
	info, err := k.GetDownloadInfo(checkCtx, tracks[0].ID, platform.QualityHiRes)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("VIP 已识别，但下载链路校验失败: %v", err)}, nil
	}
	if info == nil || strings.TrimSpace(info.URL) == "" {
		return platform.CookieCheckResult{OK: false, Message: "VIP 已识别，但下载链接为空"}, nil
	}
	message := fmt.Sprintf("Cookie 可用，下载链路已验证（实际音质：%s）", info.Quality.String())
	if info.Size > 0 {
		message = fmt.Sprintf("Cookie 可用，下载链路已验证（%s / %.2fMB）", info.Quality.String(), float64(info.Size)/1024/1024)
	}
	return platform.CookieCheckResult{OK: true, Message: message}, nil
}

func (k *KugouPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if k == nil || k.client == nil {
		return nil, platform.NewUnavailableError("kugou", "track", trackID)
	}
	song, err := k.client.GetTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	resolvedSong, err := k.client.ResolveDownloadByQuality(ctx, song, normalizeRequestedQuality(quality))
	if err != nil {
		return nil, err
	}
	song = resolvedSong
	if song == nil || strings.TrimSpace(song.URL) == "" {
		return nil, platform.NewUnavailableError("kugou", "track", trackID)
	}
	resolvedQuality := qualityFromSong(song.Bitrate, firstNonEmpty(song.Ext, detectExtFromURL(song.URL)))
	if q := strings.TrimSpace(song.Extra["resolved_quality"]); q != "" {
		if parsed, err := platform.ParseQuality(q); err == nil {
			resolvedQuality = parsed
		}
	}
	urls := collectCandidateURLs(song.URL, song.Extra)
	return &platform.DownloadInfo{
		URL:           strings.TrimSpace(song.URL),
		CandidateURLs: urls,
		Size:          song.Size,
		Format:        firstNonEmpty(song.Ext, detectExtFromURL(song.URL), "mp3"),
		Bitrate:       song.Bitrate,
		Quality:       resolvedQuality,
	}, nil
}

func (k *KugouPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if k == nil || k.client == nil {
		return nil, platform.NewUnavailableError("kugou", "search", "")
	}
	songs, err := k.client.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	tracks := make([]platform.Track, 0, len(songs))
	for _, song := range songs {
		track := convertSong(song)
		if strings.TrimSpace(track.ID) == "" {
			continue
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}

func (k *KugouPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	if k == nil || k.client == nil {
		return nil, platform.NewUnavailableError("kugou", "lyrics", trackID)
	}
	lyric, err := k.client.GetLyrics(ctx, trackID)
	if err != nil {
		return nil, err
	}
	result := &platform.Lyrics{Plain: lyric}
	if parsed := platform.ParseLRCTimestampedLines(lyric); len(parsed) > 0 {
		result.Timestamped = parsed
	}
	return result, nil
}

func (k *KugouPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.NewUnsupportedError("kugou", "audio recognition")
}

func (k *KugouPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	if k == nil || k.client == nil {
		return nil, platform.NewUnavailableError("kugou", "track", trackID)
	}
	song, err := k.client.GetTrack(ctx, trackID)
	if err != nil {
		return nil, err
	}
	track := convertSong(*song)
	if strings.TrimSpace(track.ID) == "" {
		return nil, platform.NewNotFoundError("kugou", "track", trackID)
	}
	return &track, nil
}

func (k *KugouPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return nil, platform.NewUnsupportedError("kugou", "get artist")
}

func (k *KugouPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	return nil, platform.NewUnsupportedError("kugou", "get album")
}

func (k *KugouPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	if k == nil || k.client == nil {
		return nil, platform.NewUnavailableError("kugou", "playlist", playlistID)
	}
	playlistData, songs, err := k.client.GetPlaylist(ctx, playlistID)
	if err != nil {
		return nil, err
	}
	tracks := make([]platform.Track, 0, len(songs))
	for _, song := range songs {
		tracks = append(tracks, convertSong(song))
	}
	offset := platform.PlaylistOffsetFromContext(ctx)
	if offset < 0 {
		offset = 0
	}
	if offset > 0 {
		if offset >= len(tracks) {
			tracks = nil
		} else {
			tracks = tracks[offset:]
		}
	}
	limit := platform.PlaylistLimitFromContext(ctx)
	if limit > 0 && len(tracks) > limit {
		tracks = tracks[:limit]
	}
	trackCount := playlistData.TrackCount
	if trackCount <= 0 {
		trackCount = len(songs)
	}
	return &platform.Playlist{
		ID:          strings.TrimSpace(playlistData.ID),
		Platform:    "kugou",
		Title:       strings.TrimSpace(playlistData.Name),
		Description: strings.TrimSpace(playlistData.Description),
		CoverURL:    strings.TrimSpace(playlistData.Cover),
		Creator:     strings.TrimSpace(playlistData.Creator),
		TrackCount:  trackCount,
		Tracks:      tracks,
		URL:         strings.TrimSpace(playlistData.Link),
	}, nil
}

func (k *KugouPlatform) MatchURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchURL(rawURL)
}

func (k *KugouPlatform) MatchPlaylistURL(rawURL string) (string, bool) {
	return NewURLMatcher().MatchPlaylistURL(rawURL)
}

func (k *KugouPlatform) MatchText(text string) (string, bool) {
	return NewTextMatcher().MatchText(text)
}

func convertSongModel(song songModelLike) platform.Track {
	artists := splitArtists(song.Artist, song.Extra)
	var album *platform.Album
	if strings.TrimSpace(song.Album) != "" || strings.TrimSpace(song.AlbumID) != "" {
		album = &platform.Album{
			ID:       strings.TrimSpace(song.AlbumID),
			Platform: "kugou",
			Title:    strings.TrimSpace(song.Album),
			Artists:  artists,
			CoverURL: strings.TrimSpace(song.Cover),
			URL:      buildAlbumURL(song.AlbumID),
		}
	}
	trackURL := buildTrackURL(song.ID, song.AlbumID, song.Link, song.Extra)
	return platform.Track{
		ID:       inferTrackID(song.ID, song.Link, song.Extra),
		Platform: "kugou",
		Title:    strings.TrimSpace(song.Name),
		Artists:  artists,
		Album:    album,
		Duration: time.Duration(song.Duration) * time.Second,
		CoverURL: strings.TrimSpace(song.Cover),
		URL:      trackURL,
	}
}

type songModelLike struct {
	ID       string
	Name     string
	Artist   string
	Album    string
	AlbumID  string
	Duration int
	Cover    string
	Link     string
	Extra    map[string]string
	Bitrate  int
	Ext      string
	Size     int64
}

func convertSong(song model.Song) platform.Track {
	return convertSongModel(songModelLike{
		ID:       song.ID,
		Name:     song.Name,
		Artist:   song.Artist,
		Album:    song.Album,
		AlbumID:  song.AlbumID,
		Duration: song.Duration,
		Cover:    song.Cover,
		Link:     song.Link,
		Extra:    song.Extra,
		Bitrate:  song.Bitrate,
		Ext:      song.Ext,
		Size:     song.Size,
	})
}

func inferTrackID(id, link string, extra map[string]string) string {
	if extra != nil {
		for _, candidate := range []string{
			extra["hash"],
			extra["sq_hash"],
			extra["hq_hash"],
			extra["res_hash"],
			extra["ogg_320_hash"],
			extra["file_hash"],
			extra["ogg_128_hash"],
		} {
			if normalized := normalizeHash(candidate); normalized != "" {
				return normalized
			}
		}
	}
	if normalized := normalizeHash(link); normalized != "" {
		return normalized
	}
	if normalized := normalizeHash(id); normalized != "" {
		return normalized
	}
	return strings.TrimSpace(id)
}

func mergeDownloadSong(song *model.Song, resolvedURL string) *model.Song {
	if song == nil {
		return nil
	}
	copySong := *song
	copySong.URL = strings.TrimSpace(resolvedURL)
	ensureSongExtra(&copySong)["play_url"] = copySong.URL
	if strings.TrimSpace(copySong.Ext) == "" {
		copySong.Ext = detectExtFromURL(copySong.URL)
	}
	if copySong.Extra != nil {
		clone := make(map[string]string, len(copySong.Extra))
		for key, value := range copySong.Extra {
			clone[key] = value
		}
		copySong.Extra = clone
	}
	return &copySong
}

func collectCandidateURLs(primary string, extra map[string]string) []string {
	urls := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)
	appendURL := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		urls = append(urls, value)
	}
	appendURL(primary)
	if extra != nil {
		appendURL(extra["play_backup_url"])
		appendURL(extra["play_url"])
	}
	return urls
}

func normalizeRequestedQuality(requested platform.Quality) platform.Quality {
	switch requested {
	case platform.QualityStandard, platform.QualityHigh, platform.QualityLossless, platform.QualityHiRes:
		return requested
	default:
		return platform.QualityHigh
	}
}

func splitArtists(value string, extra map[string]string) []platform.Artist {
	fields := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		switch r {
		case '/', '&', '、', ',', '，':
			return true
		default:
			return false
		}
	})
	artistIDs := splitArtistIDs(extra)
	artists := make([]platform.Artist, 0, len(fields))
	for idx, field := range fields {
		name := strings.TrimSpace(field)
		if name == "" {
			continue
		}
		artist := platform.Artist{Platform: "kugou", Name: name}
		if idx < len(artistIDs) {
			artist.ID = artistIDs[idx]
			artist.URL = buildArtistURL(artist.ID)
		}
		artists = append(artists, artist)
	}
	if len(artists) == 0 && strings.TrimSpace(value) != "" {
		artist := platform.Artist{Platform: "kugou", Name: strings.TrimSpace(value)}
		if len(artistIDs) > 0 {
			artist.ID = artistIDs[0]
			artist.URL = buildArtistURL(artist.ID)
		}
		artists = append(artists, artist)
	}
	return artists
}

func buildTrackLink(hash string) string {
	return buildTrackLinkWithAlbum(hash, "")
}

func buildTrackLinkWithAlbum(hash, albumID string) string {
	hash = normalizeHash(hash)
	if hash == "" {
		return ""
	}
	albumID = strings.TrimSpace(albumID)
	if albumID != "" {
		return "https://www.kugou.com/song/#hash=" + hash + "&album_id=" + albumID
	}
	return "https://www.kugou.com/song/#hash=" + hash
}

func buildTrackURL(id, albumID, link string, extra map[string]string) string {
	if strings.TrimSpace(link) != "" {
		return strings.TrimSpace(link)
	}
	return buildTrackLinkWithAlbum(id, firstNonEmpty(albumID, mapValue(extra, "album_id")))
}

func buildArtistURL(artistID string) string {
	artistID = strings.TrimSpace(artistID)
	if artistID == "" {
		return ""
	}
	if _, err := strconv.ParseInt(artistID, 10, 64); err != nil {
		return ""
	}
	return "https://www.kugou.com/singer/" + artistID + ".html"
}

func normalizeHash(value string) string {
	value = strings.TrimSpace(value)
	if matches := kugouHashPattern.FindStringSubmatch(value); len(matches) == 2 {
		return strings.ToLower(matches[1])
	}
	if kugouHashOnlyPattern.MatchString(value) {
		return strings.ToLower(value)
	}
	return ""
}

func qualityFromSong(bitrate int, ext string) platform.Quality {
	ext = strings.ToLower(strings.TrimSpace(ext))
	switch {
	case ext == "flac" || ext == "ape" || ext == "wav":
		if bitrate >= 2000 {
			return platform.QualityHiRes
		}
		return platform.QualityLossless
	case bitrate >= 1000:
		return platform.QualityHiRes
	case bitrate >= 700:
		return platform.QualityLossless
	case bitrate >= 320:
		return platform.QualityHigh
	default:
		return platform.QualityStandard
	}
}

func detectExtFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	pathValue := strings.ToLower(parsed.Path)
	switch {
	case strings.HasSuffix(pathValue, ".flac"):
		return "flac"
	case strings.HasSuffix(pathValue, ".ape"):
		return "ape"
	case strings.HasSuffix(pathValue, ".wav"):
		return "wav"
	case strings.HasSuffix(pathValue, ".m4a"):
		return "m4a"
	case strings.HasSuffix(pathValue, ".aac"):
		return "aac"
	case strings.HasSuffix(pathValue, ".mp3"):
		return "mp3"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func buildAlbumURL(albumID string) string {
	albumID = strings.TrimSpace(albumID)
	if albumID == "" {
		return ""
	}
	if _, err := strconv.Atoi(albumID); err != nil {
		return ""
	}
	return "https://www.kugou.com/album/" + albumID + ".html"
}

func splitArtistIDs(extra map[string]string) []string {
	value := mapValue(extra, "singer_ids")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ids = append(ids, part)
	}
	return ids
}

func mapValue(values map[string]string, key string) string {
	if values == nil {
		return ""
	}
	return strings.TrimSpace(values[key])
}
