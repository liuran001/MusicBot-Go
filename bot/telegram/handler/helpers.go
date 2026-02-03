package handler

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

func extractPlatformTrack(message *models.Message, manager platform.Manager) (platformName, trackID string, found bool) {
	if message == nil || message.Text == "" {
		return "", "", false
	}

	text := message.Text
	args := commandArguments(message.Text)
	if args != "" {
		text = args
		fields := strings.Fields(args)
		if len(fields) >= 3 {
			if _, err := platform.ParseQuality(fields[2]); err == nil {
				return fields[0], fields[1], true
			}
		}
	}

	if manager != nil {
		if _, _, hasSuffix := parseSearchKeywordPlatform(text); hasSuffix {
			return "", "", false
		}
		if plat, id, matched := manager.MatchText(text); matched {
			return plat, id, true
		}
		if plat, id, matched := manager.MatchURL(text); matched {
			return plat, id, true
		}
	}

	return "", "", false
}

func extractQualityOverride(message *models.Message) string {
	if message == nil || message.Text == "" {
		return ""
	}
	args := commandArguments(message.Text)
	if args == "" {
		return ""
	}
	fields := strings.Fields(args)
	if len(fields) < 3 {
		return ""
	}
	if _, err := platform.ParseQuality(fields[2]); err == nil {
		return fields[2]
	}
	return ""
}

func commandArguments(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	parts := strings.SplitN(text, " ", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func commandName(text, botName string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	parts := strings.SplitN(text, " ", 2)
	command := strings.TrimPrefix(parts[0], "/")
	if command == "" {
		return ""
	}
	if strings.Contains(command, "@") {
		seg := strings.SplitN(command, "@", 2)
		command = seg[0]
		if botName != "" && len(seg) > 1 && seg[1] != "" && seg[1] != botName {
			return ""
		}
	}
	return command
}

func isNumeric(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func ensureDir(path string) {
	_, err := os.Stat(path)
	if err == nil {
		return
	}
	if os.IsNotExist(err) {
		_ = os.MkdirAll(path, os.ModePerm)
	}
}

func sanitizeFileName(name string) string {
	replacer := strings.NewReplacer("/", " ", "?", " ", "*", " ", ":", " ", "|", " ", "\\", " ", "<", " ", ">", " ", "\"", " ")
	return replacer.Replace(name)
}

func cleanupFiles(paths ...string) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		_ = os.RemoveAll(p)
	}
}

func buildMusicCaption(songInfo *botpkg.SongInfo, botName string) string {
	if songInfo == nil {
		return ""
	}

	songNameHTML := songInfo.SongName
	artistsHTML := songInfo.SongArtists
	albumHTML := songInfo.SongAlbum
	infoParts := make([]string, 0, 2)
	if sizeText := formatFileSize(songInfo.MusicSize + songInfo.EmbPicSize); sizeText != "" {
		infoParts = append(infoParts, sizeText)
	}
	if bitrateText := formatBitrate(songInfo.BitRate); bitrateText != "" {
		infoParts = append(infoParts, bitrateText)
	}
	infoLine := strings.Join(infoParts, " ")
	if infoLine != "" {
		infoLine += "\n"
	}
	tags := strings.Join(formatInfoTags(songInfo.Platform, songInfo.FileExt), " ")

	if strings.TrimSpace(songInfo.TrackURL) != "" {
		songNameHTML = fmt.Sprintf("<a href=\"%s\">%s</a>", songInfo.TrackURL, songInfo.SongName)
	}

	if strings.TrimSpace(songInfo.SongArtistsURLs) != "" {
		artistURLs := strings.Split(songInfo.SongArtistsURLs, ",")
		artists := strings.Split(songInfo.SongArtists, "/")
		var parts []string
		for i, artist := range artists {
			artist = strings.TrimSpace(artist)
			if i < len(artistURLs) && strings.TrimSpace(artistURLs[i]) != "" {
				parts = append(parts, fmt.Sprintf("<a href=\"%s\">%s</a>", strings.TrimSpace(artistURLs[i]), artist))
				continue
			}
			parts = append(parts, artist)
		}
		artistsHTML = strings.Join(parts, " / ")
	}

	if strings.TrimSpace(songInfo.AlbumURL) != "" {
		albumHTML = fmt.Sprintf("<a href=\"%s\">%s</a>", songInfo.AlbumURL, songInfo.SongAlbum)
	}

	return fmt.Sprintf("<b>「%s」- %s</b>\n专辑: %s\n<blockquote>%s%s\n</blockquote>via @%s",
		songNameHTML,
		artistsHTML,
		albumHTML,
		infoLine,
		tags,
		botName,
	)
}

func formatInfoTags(platformName, fileExt string) []string {
	tags := []string{"#" + platformTag(platformName)}
	if strings.TrimSpace(fileExt) != "" {
		tags = append(tags, "#"+fileExt)
	}
	return tags
}

func formatFileSize(musicSize int) string {
	if musicSize <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2fMB", float64(musicSize)/1024/1024)
}

func formatBitrate(bitRate int) string {
	if bitRate <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2fkbps", float64(bitRate)/1000)
}

func formatInlineInfoLine(platformName, fileExt string, musicSize int, bitRate int) string {
	parts := formatInfoTags(platformName, fileExt)
	if sizeText := formatFileSize(musicSize); sizeText != "" {
		parts = append(parts, sizeText)
	}
	if bitrateText := formatBitrate(bitRate); bitrateText != "" {
		parts = append(parts, bitrateText)
	}
	return strings.Join(parts, " ")
}

func formatFileInfo(fileExt string, musicSize int) string {
	if musicSize <= 0 || strings.TrimSpace(fileExt) == "" {
		return ""
	}
	return fmt.Sprintf("%s %.2fMB", fileExt, float64(musicSize)/1024/1024)
}

func platformEmoji(platformName string) string {
	switch platformName {
	case "netease":
		return "🎵"
	case "spotify":
		return "🎧"
	case "qqmusic":
		return "🎶"
	case "tencent":
		return "🎶"
	default:
		return "🎵"
	}
}

func platformDisplayName(platformName string) string {
	switch platformName {
	case "netease":
		return "网易云音乐"
	case "spotify":
		return "Spotify"
	case "qqmusic":
		return "QQ音乐"
	case "tencent":
		return "QQ音乐"
	default:
		return platformName
	}
}

func platformTag(platformName string) string {
	display := strings.TrimSpace(platformDisplayName(platformName))
	if display == "" {
		return "music"
	}
	return display
}

func fillSongInfoFromTrack(songInfo *botpkg.SongInfo, track *platform.Track, platformName, trackID string, message *models.Message) {
	songInfo.Platform = platformName
	songInfo.TrackID = trackID

	if platformName == "netease" {
		if id, err := strconv.Atoi(trackID); err == nil {
			songInfo.MusicID = id
		}
	} else {
		songInfo.MusicID = 0
	}

	songInfo.Duration = int(track.Duration.Seconds())
	songInfo.SongName = track.Title

	artistNames := make([]string, 0, len(track.Artists))
	artistIDs := make([]string, 0, len(track.Artists))
	artistURLs := make([]string, 0, len(track.Artists))
	for _, artist := range track.Artists {
		artistNames = append(artistNames, artist.Name)
		artistIDs = append(artistIDs, artist.ID)
		if strings.TrimSpace(artist.URL) != "" {
			artistURLs = append(artistURLs, artist.URL)
		} else {
			artistURLs = append(artistURLs, "")
		}
	}
	songInfo.SongArtists = strings.Join(artistNames, "/")
	songInfo.SongArtistsIDs = strings.Join(artistIDs, ",")
	songInfo.SongArtistsURLs = strings.Join(artistURLs, ",")

	if track.Album != nil {
		songInfo.SongAlbum = track.Album.Title
		if strings.TrimSpace(track.Album.URL) != "" {
			songInfo.AlbumURL = track.Album.URL
		}
		if id, err := strconv.Atoi(track.Album.ID); err == nil {
			songInfo.AlbumID = id
		}
	}
	if strings.TrimSpace(track.URL) != "" {
		songInfo.TrackURL = track.URL
	}

	if message != nil {
		songInfo.FromChatID = message.Chat.ID
		if message.Chat.Type == "private" {
			songInfo.FromChatName = message.Chat.Username
		} else {
			songInfo.FromChatName = message.Chat.Title
		}
		if message.From != nil {
			songInfo.FromUserID = message.From.ID
			songInfo.FromUserName = message.From.Username
		}
	}
}

type progressWriter struct {
	ctx         context.Context
	bot         *bot.Bot
	msg         *models.Message
	total       int64
	written     int64
	lastUpdate  time.Time
	lastWritten int64
	filename    string
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	pw.written += int64(n)

	now := time.Now()
	if now.Sub(pw.lastUpdate) >= 2*time.Second && pw.msg != nil {
		downloaded := float64(pw.written) / 1024 / 1024
		total := float64(pw.total) / 1024 / 1024
		progress := float64(pw.written) * 100 / float64(pw.total)

		bytesInPeriod := pw.written - pw.lastWritten
		duration := now.Sub(pw.lastUpdate).Seconds()
		speed := float64(bytesInPeriod) / duration / 1024 / 1024

		text := fmt.Sprintf("正在下载：%s\n进度：%.2f%% (%.2f MB / %.2f MB)\n速度：%.2f MB/s",
			pw.filename, progress, downloaded, total, speed)
		if pw.msg.Text == text {
			pw.lastUpdate = now
			pw.lastWritten = pw.written
			return n, nil
		}

		_, err = pw.bot.EditMessageText(pw.ctx, &bot.EditMessageTextParams{
			ChatID:    pw.msg.Chat.ID,
			MessageID: pw.msg.ID,
			Text:      text,
		})
		if err == nil {
			pw.msg.Text = text
		}

		pw.lastUpdate = now
		pw.lastWritten = pw.written
	}

	return n, nil
}
