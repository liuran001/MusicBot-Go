package qqmusic

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type QQMusicPlatform struct {
	client *Client
}

func NewPlatform(client *Client) *QQMusicPlatform {
	return &QQMusicPlatform{client: client}
}

func (q *QQMusicPlatform) Name() string {
	return "qqmusic"
}

func (q *QQMusicPlatform) SupportsDownload() bool {
	return true
}

func (q *QQMusicPlatform) SupportsSearch() bool {
	return true
}

func (q *QQMusicPlatform) SupportsLyrics() bool {
	return true
}

func (q *QQMusicPlatform) SupportsRecognition() bool {
	return false
}

func (q *QQMusicPlatform) CheckCookie(ctx context.Context) (platform.CookieCheckResult, error) {
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := q.GetDownloadInfo(checkCtx, "0013WPvt4fQH2b", platform.QualityHiRes)
	if err != nil {
		return platform.CookieCheckResult{OK: false, Message: fmt.Sprintf("Hi-Res 校验失败: %v", err)}, nil
	}
	if info == nil || strings.TrimSpace(info.URL) == "" || info.Size <= 0 {
		return platform.CookieCheckResult{OK: false, Message: "Hi-Res 下载链接为空或文件大小为 0"}, nil
	}
	return platform.CookieCheckResult{OK: true, Message: "Hi-Res 可用"}, nil
}

func (q *QQMusicPlatform) ManualRenew(ctx context.Context) (string, error) {
	if q == nil || q.client == nil {
		return "", fmt.Errorf("qqmusic client unavailable")
	}
	return q.client.ManualRenew(ctx)
}

func (q *QQMusicPlatform) Capabilities() platform.Capabilities {
	return platform.Capabilities{
		Download:    true,
		Search:      true,
		Lyrics:      true,
		Recognition: false,
		HiRes:       true,
	}
}

func (q *QQMusicPlatform) Metadata() platform.Meta {
	return platform.Meta{
		Name:          "qqmusic",
		DisplayName:   "QQ音乐",
		Emoji:         "🎶",
		Aliases:       []string{"qqmusic", "qq", "tencent", "QQ音乐", "qq音乐"},
		AllowGroupURL: true,
	}
}

func (q *QQMusicPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if q.client == nil {
		return nil, platform.NewUnavailableError("qqmusic", "track", trackID)
	}
	detail, err := q.client.GetSongDetail(ctx, trackID)
	if err != nil {
		return nil, err
	}
	songMid := strings.TrimSpace(detail.Mid)
	if songMid == "" {
		return nil, platform.NewNotFoundError("qqmusic", "track", trackID)
	}
	fileInfo, err := q.client.GetSongFileInfo(ctx, songMid)
	if err != nil {
		return nil, err
	}
	mediaMid := strings.TrimSpace(fileInfo.MediaMid)
	if mediaMid == "" {
		mediaMid = songMid
	}
	profiles := qualityProfiles()
	qualityIdx := qualityIndex(quality)
	selected := selectQualityProfile(profiles, qualityIdx, fileInfo)
	if selected == nil {
		return nil, platform.NewUnavailableError("qqmusic", "track", trackID)
	}
	uin, authst := parseQQAuth(q.client.Cookie())
	purl, err := q.client.GetVKey(ctx, songMid, mediaMid, selected.Code, selected.Ext, uin, authst)
	if err != nil {
		return nil, err
	}
	url := buildStreamURL(purl)
	if strings.TrimSpace(url) == "" {
		return nil, platform.NewUnavailableError("qqmusic", "track", trackID)
	}
	return &platform.DownloadInfo{
		URL:     url,
		Size:    selected.Size(fileInfo),
		Format:  selected.Ext,
		Bitrate: selected.Quality.Bitrate(),
		Quality: selected.Quality,
	}, nil
}

func (q *QQMusicPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
	if q.client == nil {
		return nil, platform.NewUnavailableError("qqmusic", "search", "")
	}
	songs, err := q.client.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	tracks := make([]platform.Track, 0, len(songs))
	for _, song := range songs {
		track := convertSearchSong(song)
		if track.ID == "" {
			continue
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}

func (q *QQMusicPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
	if q.client == nil {
		return nil, platform.NewUnavailableError("qqmusic", "lyrics", trackID)
	}
	detail, err := q.client.GetSongDetail(ctx, trackID)
	if err != nil {
		return nil, err
	}
	songMid := strings.TrimSpace(detail.Mid)
	if songMid == "" {
		return nil, platform.NewNotFoundError("qqmusic", "lyrics", trackID)
	}
	lyric, trans, err := q.client.GetLyrics(ctx, songMid)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(lyric) == "" {
		return nil, platform.NewUnavailableError("qqmusic", "lyrics", trackID)
	}
	result := &platform.Lyrics{
		Plain:       lyric,
		Translation: strings.TrimSpace(trans),
	}
	if parsed := parseLyricLines(lyric); len(parsed) > 0 {
		result.Timestamped = parsed
	}
	return result, nil
}

func (q *QQMusicPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
	return nil, platform.NewUnsupportedError("qqmusic", "audio recognition")
}

func (q *QQMusicPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
	if q.client == nil {
		return nil, platform.NewUnavailableError("qqmusic", "track", trackID)
	}
	detail, err := q.client.GetSongDetail(ctx, trackID)
	if err != nil {
		return nil, err
	}
	track := convertSongDetail(detail)
	if strings.TrimSpace(track.CoverURL) == "" {
		songMid := strings.TrimSpace(detail.Mid)
		if songMid != "" {
			if fileInfo, fileErr := q.client.GetSongFileInfo(ctx, songMid); fileErr == nil && fileInfo != nil {
				if coverMid := strings.TrimSpace(fileInfo.CoverMid); coverMid != "" {
					track.CoverURL = buildSongCoverURL(coverMid)
				}
			}
		}
	}
	if track.ID == "" {
		return nil, platform.NewNotFoundError("qqmusic", "track", trackID)
	}
	return &track, nil
}

func (q *QQMusicPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return nil, platform.NewUnsupportedError("qqmusic", "get artist")
}

func (q *QQMusicPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
	return nil, platform.NewUnsupportedError("qqmusic", "get album")
}

func (q *QQMusicPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
	isAlbum, rawID := parseCollectionID(playlistID)
	if isAlbum {
		return q.getAlbumAsPlaylist(ctx, rawID)
	}
	playlistID = rawID

	if q.client == nil {
		return nil, platform.NewUnavailableError("qqmusic", "playlist", playlistID)
	}
	data, err := q.client.GetPlaylist(ctx, playlistID)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, platform.NewNotFoundError("qqmusic", "playlist", playlistID)
	}
	title := strings.TrimSpace(data.Name)
	description := strings.TrimSpace(data.Desc)
	creator := strings.TrimSpace(data.Creator.Name)
	if creator == "" {
		creator = strings.TrimSpace(data.CreatorName)
	}
	tracks := make([]platform.Track, 0, len(data.Songlist))
	for _, song := range data.Songlist {
		tracks = append(tracks, convertPlaylistSong(song))
	}
	trackCount := data.Total
	if trackCount <= 0 {
		trackCount = len(tracks)
	}
	id := playlistID
	if data.ID != 0 {
		id = fmt.Sprintf("%d", data.ID)
	}
	return &platform.Playlist{
		ID:          id,
		Platform:    "qqmusic",
		Title:       title,
		Description: description,
		CoverURL:    strings.TrimSpace(data.Logo),
		Creator:     creator,
		TrackCount:  trackCount,
		Tracks:      tracks,
		URL:         buildPlaylistURL(id),
	}, nil
}

func (q *QQMusicPlatform) getAlbumAsPlaylist(ctx context.Context, albumID string) (*platform.Playlist, error) {
	if q.client == nil {
		return nil, platform.NewUnavailableError("qqmusic", "album", albumID)
	}
	data, err := q.client.GetAlbum(ctx, albumID)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, platform.NewNotFoundError("qqmusic", "album", albumID)
	}

	tracks := make([]platform.Track, 0, len(data.Songlist))
	for _, song := range data.Songlist {
		tracks = append(tracks, convertPlaylistSong(song))
	}
	if len(tracks) == 0 {
		return nil, platform.NewNotFoundError("qqmusic", "album", albumID)
	}

	id := strings.TrimSpace(data.ID)
	if id == "" {
		id = strings.TrimSpace(albumID)
	}
	albumMid := strings.TrimSpace(data.Mid)
	if albumMid == "" && tracks[0].Album != nil {
		albumMid = strings.TrimSpace(tracks[0].Album.ID)
	}
	title := strings.TrimSpace(data.Name)
	if title == "" && tracks[0].Album != nil {
		title = strings.TrimSpace(tracks[0].Album.Title)
	}
	coverURL := strings.TrimSpace(data.CoverURL)
	if coverURL == "" {
		if tracks[0].Album != nil {
			coverURL = strings.TrimSpace(tracks[0].Album.CoverURL)
		}
		if coverURL == "" {
			coverURL = strings.TrimSpace(tracks[0].CoverURL)
		}
	}
	creator := strings.TrimSpace(data.Creator)
	if creator == "" {
		artistNames := make([]string, 0, len(data.Artists))
		for _, artist := range data.Artists {
			name := strings.TrimSpace(artist.Name)
			if name == "" {
				continue
			}
			artistNames = append(artistNames, name)
		}
		creator = strings.Join(artistNames, "/")
	}
	trackCount := data.Total
	if trackCount <= 0 {
		trackCount = len(tracks)
	}
	urlID := albumMid
	if urlID == "" {
		urlID = id
	}

	return &platform.Playlist{
		ID:          id,
		Platform:    "qqmusic",
		Title:       title,
		Description: strings.TrimSpace(data.Desc),
		CoverURL:    coverURL,
		Creator:     creator,
		TrackCount:  trackCount,
		Tracks:      tracks,
		URL:         buildAlbumURL(urlID),
	}, nil
}

func (q *QQMusicPlatform) MatchURL(rawURL string) (string, bool) {
	matcher := NewURLMatcher()
	return matcher.MatchURL(rawURL)
}

// MatchPlaylistURL implements platform.PlaylistURLMatcher interface.
func (q *QQMusicPlatform) MatchPlaylistURL(rawURL string) (string, bool) {
	matcher := NewURLMatcher()
	return matcher.MatchPlaylistURL(rawURL)
}

// ShortLinkHosts implements platform.ShortLinkProvider.
func (q *QQMusicPlatform) ShortLinkHosts() []string {
	return []string{"c6.y.qq.com"}
}

func (q *QQMusicPlatform) MatchText(text string) (string, bool) {
	matcher := NewTextMatcher()
	return matcher.MatchText(text)
}

type qualityProfile struct {
	Quality platform.Quality
	SizeKey string
	Code    string
	Ext     string
}

func (q qualityProfile) Size(info *qqFileInfo) int64 {
	if info == nil {
		return 0
	}
	switch q.SizeKey {
	case "size_hires":
		return info.SizeHiRes
	case "size_flac":
		return info.SizeFlac
	case "size_320mp3":
		return info.Size320
	case "size_128mp3":
		return info.Size128
	default:
		return 0
	}
}

func qualityProfiles() []qualityProfile {
	return []qualityProfile{
		{Quality: platform.QualityHiRes, SizeKey: "size_hires", Code: "RS01", Ext: "flac"},
		{Quality: platform.QualityLossless, SizeKey: "size_flac", Code: "F000", Ext: "flac"},
		{Quality: platform.QualityHigh, SizeKey: "size_320mp3", Code: "M800", Ext: "mp3"},
		{Quality: platform.QualityStandard, SizeKey: "size_128mp3", Code: "M500", Ext: "mp3"},
	}
}

func qualityIndex(q platform.Quality) int {
	profiles := qualityProfiles()
	for i, profile := range profiles {
		if profile.Quality == q {
			return i
		}
	}
	return len(profiles) - 1
}

func selectQualityProfile(profiles []qualityProfile, start int, info *qqFileInfo) *qualityProfile {
	if len(profiles) == 0 {
		return nil
	}
	if start < 0 || start >= len(profiles) {
		start = len(profiles) - 1
	}
	for i := start; i < len(profiles); i++ {
		if profiles[i].Size(info) > 0 {
			return &profiles[i]
		}
	}
	for i := start - 1; i >= 0; i-- {
		if profiles[i].Size(info) > 0 {
			return &profiles[i]
		}
	}
	return nil
}

func parseQQAuth(cookie string) (string, string) {
	uin := parseCookieValue(cookie, "uin")
	authst := parseCookieValue(cookie, "qqmusic_key")
	if uin == "" {
		uin = "0"
	}
	return uin, authst
}

func parseCookieValue(cookie, key string) string {
	if cookie == "" || key == "" {
		return ""
	}
	parts := strings.Split(cookie, ";")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		if strings.TrimSpace(kv[0]) == key {
			return strings.TrimSpace(kv[1])
		}
	}
	return ""
}

func buildStreamURL(purl string) string {
	trimmed := strings.TrimSpace(purl)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}
	return "https://ws.stream.qqmusic.qq.com/" + trimmed
}

func convertSearchSong(song qqSearchSong) platform.Track {
	trackID := strings.TrimSpace(song.SongMID)
	if trackID == "" && song.SongID != 0 {
		trackID = fmt.Sprintf("%d", song.SongID)
	}
	coverURL := buildTrackCoverURL(song.AlbumMID)
	artists := make([]platform.Artist, 0, len(song.Singer))
	for _, singer := range song.Singer {
		artists = append(artists, platform.Artist{
			ID:       strings.TrimSpace(singer.Mid),
			Platform: "qqmusic",
			Name:     singer.Name,
			URL:      buildArtistURL(singer.Mid),
		})
	}
	var album *platform.Album
	if strings.TrimSpace(song.AlbumName) != "" || strings.TrimSpace(song.AlbumMID) != "" {
		album = &platform.Album{
			ID:       strings.TrimSpace(song.AlbumMID),
			Platform: "qqmusic",
			Title:    song.AlbumName,
			Artists:  artists,
			CoverURL: buildAlbumCoverURL(song.AlbumMID),
			URL:      buildAlbumURL(song.AlbumMID),
		}
	}
	duration := time.Duration(song.Interval) * time.Second
	return platform.Track{
		ID:       trackID,
		Platform: "qqmusic",
		Title:    song.SongName,
		Artists:  artists,
		Album:    album,
		Duration: duration,
		CoverURL: coverURL,
		URL:      buildTrackURL(trackID),
	}
}

func convertPlaylistSong(song qqPlaylistSong) platform.Track {
	trackID := strings.TrimSpace(song.SongMID)
	if trackID == "" {
		trackID = strings.TrimSpace(song.Mid)
	}
	if trackID == "" && song.SongID != 0 {
		trackID = fmt.Sprintf("%d", song.SongID)
	}
	if trackID == "" && song.ID != 0 {
		trackID = fmt.Sprintf("%d", song.ID)
	}
	artists := make([]platform.Artist, 0, len(song.Singer))
	for _, singer := range song.Singer {
		artists = append(artists, platform.Artist{
			ID:       strings.TrimSpace(singer.Mid),
			Platform: "qqmusic",
			Name:     singer.Name,
			URL:      buildArtistURL(singer.Mid),
		})
	}
	title := strings.TrimSpace(song.SongName)
	if title == "" {
		title = strings.TrimSpace(song.Title)
	}
	if title == "" {
		title = strings.TrimSpace(song.Name)
	}
	albumName := strings.TrimSpace(song.AlbumName)
	albumMID := strings.TrimSpace(song.AlbumMID)
	if albumName == "" {
		albumName = strings.TrimSpace(song.Album.Name)
	}
	if albumMID == "" {
		albumMID = strings.TrimSpace(song.Album.Mid)
	}
	coverURL := buildTrackCoverURL(albumMID)
	var album *platform.Album
	if albumName != "" || albumMID != "" {
		album = &platform.Album{
			ID:       albumMID,
			Platform: "qqmusic",
			Title:    albumName,
			Artists:  artists,
			CoverURL: buildAlbumCoverURL(albumMID),
			URL:      buildAlbumURL(albumMID),
		}
	}
	duration := time.Duration(song.Interval) * time.Second
	return platform.Track{
		ID:       trackID,
		Platform: "qqmusic",
		Title:    title,
		Artists:  artists,
		Album:    album,
		Duration: duration,
		CoverURL: coverURL,
		URL:      buildTrackURL(trackID),
	}
}

func convertSongDetail(detail *qqSongDetail) platform.Track {
	trackID := strings.TrimSpace(detail.Mid)
	if trackID == "" && detail.ID != 0 {
		trackID = fmt.Sprintf("%d", detail.ID)
	}
	artists := make([]platform.Artist, 0, len(detail.Singer))
	for _, singer := range detail.Singer {
		artists = append(artists, platform.Artist{
			ID:       strings.TrimSpace(singer.Mid),
			Platform: "qqmusic",
			Name:     singer.Name,
			URL:      buildArtistURL(singer.Mid),
		})
	}
	albumTitle := strings.TrimSpace(detail.Album.Title)
	if albumTitle == "" {
		albumTitle = strings.TrimSpace(detail.Album.Name)
	}
	coverURL := buildTrackCoverURL(detail.Album.Mid)
	var album *platform.Album
	if albumTitle != "" || strings.TrimSpace(detail.Album.Mid) != "" {
		album = &platform.Album{
			ID:       strings.TrimSpace(detail.Album.Mid),
			Platform: "qqmusic",
			Title:    albumTitle,
			Artists:  artists,
			CoverURL: buildAlbumCoverURL(detail.Album.Mid),
			URL:      buildAlbumURL(detail.Album.Mid),
		}
	}
	title := strings.TrimSpace(detail.Title)
	if title == "" {
		title = strings.TrimSpace(detail.Name)
	}
	duration := time.Duration(detail.Interval) * time.Second
	return platform.Track{
		ID:       trackID,
		Platform: "qqmusic",
		Title:    title,
		Artists:  artists,
		Album:    album,
		Duration: duration,
		CoverURL: coverURL,
		URL:      buildTrackURL(trackID),
	}
}

func buildTrackURL(trackID string) string {
	if strings.TrimSpace(trackID) == "" {
		return ""
	}
	return "https://y.qq.com/n/ryqq_v2/songDetail/" + trackID
}

func buildPlaylistURL(playlistID string) string {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return ""
	}
	return "https://y.qq.com/n/ryqq_v2/playlist/" + playlistID
}

func buildAlbumURL(albumMid string) string {
	albumMid = strings.TrimSpace(albumMid)
	if albumMid == "" {
		return ""
	}
	return "https://y.qq.com/n/ryqq_v2/albumDetail/" + albumMid
}

func buildArtistURL(artistMid string) string {
	artistMid = strings.TrimSpace(artistMid)
	if artistMid == "" {
		return ""
	}
	return "https://y.qq.com/n/ryqq_v2/singer/" + artistMid
}

func buildArtistCoverURL(artistMid string) string {
	artistMid = strings.TrimSpace(artistMid)
	if artistMid == "" {
		return ""
	}
	return "https://y.gtimg.cn/music/photo_new/T001R300x300M000" + artistMid + ".jpg"
}

func buildSongCoverURL(songCoverMid string) string {
	songCoverMid = strings.TrimSpace(songCoverMid)
	if songCoverMid == "" {
		return ""
	}
	return "https://y.qq.com/music/photo_new/T062M000" + songCoverMid + ".jpg"
}

func buildAlbumCoverURL(albumMid string) string {
	albumMid = strings.TrimSpace(albumMid)
	if albumMid == "" {
		return ""
	}
	return "https://y.gtimg.cn/music/photo_new/T002M000" + albumMid + ".jpg"
}

func buildTrackCoverURL(albumMid string) string {
	return buildAlbumCoverURL(albumMid)
}

var lyricLineRe = regexp.MustCompile(`^\[(\d+):(\d+)\.(\d+)\](.*)$`)

func parseLyricLines(lrc string) []platform.LyricLine {
	lines := strings.Split(lrc, "\n")
	result := make([]platform.LyricLine, 0, len(lines))
	for _, line := range lines {
		matches := lyricLineRe.FindStringSubmatch(line)
		if len(matches) != 5 {
			continue
		}
		minutes, _ := strconv.Atoi(matches[1])
		seconds, _ := strconv.Atoi(matches[2])
		centis, _ := strconv.Atoi(matches[3])
		text := strings.TrimSpace(matches[4])
		if text == "" {
			continue
		}
		duration := time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second + time.Duration(centis)*10*time.Millisecond
		result = append(result, platform.LyricLine{Time: duration, Text: text})
	}
	return result
}
