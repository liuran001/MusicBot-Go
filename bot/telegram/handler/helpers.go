package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/XiaoMengXinX/Music163Api-Go/api"
	"github.com/XiaoMengXinX/Music163Api-Go/types"
	"github.com/XiaoMengXinX/Music163Api-Go/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

var (
	reg1   = regexp.MustCompile(`(.*)song\?id=`)
	reg2   = regexp.MustCompile("(.*)song/")
	regP1  = regexp.MustCompile(`(.*)program\?id=`)
	regP2  = regexp.MustCompile("(.*)program/")
	regP3  = regexp.MustCompile(`(.*)dj\?id=`)
	regP4  = regexp.MustCompile("(.*)dj/")
	reg5   = regexp.MustCompile("/(.*)")
	reg4   = regexp.MustCompile("&(.*)")
	reg3   = regexp.MustCompile(`\?(.*)`)
	regInt = regexp.MustCompile(`\d+`)
	regUrl = regexp.MustCompile("(http|https)://[\\w\\-_]+(\\.[\\w\\-_]+)+([\\w\\-.,@?^=%&:/~+#]*[\\w\\-@?^=%&/~+#])?")
)

func extractMusicID(message *models.Message) int {
	if message == nil {
		return 0
	}

	text := message.Text
	args := commandArguments(message.Text)
	if args != "" {
		text = args
	}

	text = getRedirectUrl(text)

	if id := parseMusicID(text); id != 0 {
		return id
	}
	if id := parseProgramID(text); id != 0 {
		return getProgramRealID(id)
	}
	return 0
}

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

	text = getRedirectUrl(text)

	if manager != nil {
		plat, id, matched := manager.MatchURL(text)
		if matched {
			return plat, id, true
		}
	}

	musicID := parseMusicID(text)
	if musicID == 0 {
		musicID = parseProgramID(text)
		if musicID != 0 {
			musicID = getProgramRealID(musicID)
		}
	}

	if musicID != 0 {
		return "netease", fmt.Sprintf("%d", musicID), true
	}

	return "", "", false
}

func parseArtist(songDetail types.SongDetailData) string {
	var artists string
	for i, ar := range songDetail.Ar {
		if i == 0 {
			artists = ar.Name
		} else {
			artists = fmt.Sprintf("%s/%s", artists, ar.Name)
		}
	}
	return artists
}

func parseMusicID(text string) int {
	var replacer = strings.NewReplacer("\n", "", " ", "")
	messageText := replacer.Replace(getRedirectUrl(text))
	musicUrl := regUrl.FindStringSubmatch(messageText)
	if len(musicUrl) != 0 {
		if strings.Contains(musicUrl[0], "song") {
			ur, _ := url.Parse(musicUrl[0])
			id := ur.Query().Get("id")
			if musicid, _ := strconv.Atoi(id); musicid != 0 {
				return musicid
			}
		}
	}
	musicid, _ := strconv.Atoi(linkTestMusic(messageText))
	return musicid
}

func parseProgramID(text string) int {
	var replacer = strings.NewReplacer("\n", "", " ", "")
	messageText := replacer.Replace(text)
	programid, _ := strconv.Atoi(linkTestProgram(messageText))
	return programid
}

func extractInt(text string) string {
	matchArr := regInt.FindStringSubmatch(text)
	if len(matchArr) == 0 {
		return ""
	}
	return matchArr[0]
}

func linkTestMusic(text string) string {
	return extractInt(reg5.ReplaceAllString(reg4.ReplaceAllString(reg3.ReplaceAllString(reg2.ReplaceAllString(reg1.ReplaceAllString(text, ""), ""), ""), ""), ""))
}

func linkTestProgram(text string) string {
	return extractInt(reg5.ReplaceAllString(reg4.ReplaceAllString(reg3.ReplaceAllString(regP4.ReplaceAllString(regP3.ReplaceAllString(regP2.ReplaceAllString(regP1.ReplaceAllString(text, ""), ""), ""), ""), ""), ""), ""))
}

func getProgramRealID(programID int) int {
	programDetail, err := api.GetProgramDetail(utils.RequestData{}, programID)
	if err != nil {
		return 0
	}
	if programDetail.Program.MainSong.ID != 0 {
		return programDetail.Program.MainSong.ID
	}
	return 0
}

func getRedirectUrl(text string) string {
	var replacer = strings.NewReplacer("\n", "", " ", "")
	messageText := replacer.Replace(text)
	musicUrl := regUrl.FindStringSubmatch(messageText)
	if len(musicUrl) != 0 {
		if strings.Contains(musicUrl[0], "163cn.tv") {
			urlStr := musicUrl[0]
			req, err := http.NewRequest("GET", urlStr, nil)
			if err != nil {
				return text
			}
			client := &http.Client{
				Timeout: 10 * time.Second,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			resp, err := client.Do(req)
			if err != nil {
				return text
			}
			defer resp.Body.Close()
			return resp.Header.Get("location")
		}
	}
	return text
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
	platformTag := "#" + platformTag(songInfo.Platform)

	if songInfo.Platform == "netease" || songInfo.Platform == "" {
		if songInfo.MusicID != 0 {
			songNameHTML = fmt.Sprintf("<a href=\"https://music.163.com/song?id=%d\">%s</a>", songInfo.MusicID, songInfo.SongName)
		}

		if songInfo.SongArtistsIDs != "" {
			artistIDs := strings.Split(songInfo.SongArtistsIDs, ",")
			artists := strings.Split(songInfo.SongArtists, "/")
			var parts []string
			for i, artist := range artists {
				artist = strings.TrimSpace(artist)
				if i < len(artistIDs) {
					if id, err := strconv.ParseInt(artistIDs[i], 10, 64); err == nil && id > 0 {
						parts = append(parts, fmt.Sprintf("<a href=\"https://music.163.com/artist?id=%d\">%s</a>", id, artist))
						continue
					}
				}
				parts = append(parts, artist)
			}
			artistsHTML = strings.Join(parts, " / ")
		}

		if songInfo.AlbumID > 0 {
			albumHTML = fmt.Sprintf("<a href=\"https://music.163.com/album?id=%d\">%s</a>", songInfo.AlbumID, songInfo.SongAlbum)
		}

	}

	return fmt.Sprintf("<b>「%s」- %s</b>\n专辑: %s\n<blockquote>%.2fMB %.2fkbps\n%s #%s\n</blockquote>via @%s",
		songNameHTML,
		artistsHTML,
		albumHTML,
		float64(songInfo.MusicSize+songInfo.EmbPicSize)/1024/1024,
		float64(songInfo.BitRate)/1000,
		platformTag,
		songInfo.FileExt,
		botName,
	)
}

func platformEmoji(platformName string) string {
	switch platformName {
	case "netease":
		return "🎵"
	case "spotify":
		return "🎧"
	case "qqmusic":
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
	default:
		return platformName
	}
}

func platformTag(platformName string) string {
	if strings.TrimSpace(platformName) == "" {
		return "music"
	}
	return platformName
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
	for _, artist := range track.Artists {
		artistNames = append(artistNames, artist.Name)
		artistIDs = append(artistIDs, artist.ID)
	}
	songInfo.SongArtists = strings.Join(artistNames, "/")
	songInfo.SongArtistsIDs = strings.Join(artistIDs, ",")

	if track.Album != nil {
		songInfo.SongAlbum = track.Album.Title
		if platformName == "netease" {
			if id, err := strconv.Atoi(track.Album.ID); err == nil {
				songInfo.AlbumID = id
			}
		}
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

		_, _ = pw.bot.EditMessageText(pw.ctx, &bot.EditMessageTextParams{
			ChatID:    pw.msg.Chat.ID,
			MessageID: pw.msg.ID,
			Text:      text,
		})

		pw.lastUpdate = now
		pw.lastWritten = pw.written
	}

	return n, nil
}
