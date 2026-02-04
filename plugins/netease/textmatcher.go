package netease

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/XiaoMengXinX/Music163Api-Go/api"
	"github.com/XiaoMengXinX/Music163Api-Go/utils"
)

var (
	regProgramQ = regexp.MustCompile(`(.*)program\?id=`)
	regProgramP = regexp.MustCompile(`(.*)program/`)
	regDjQuery  = regexp.MustCompile(`(.*)dj\?id=`)
	regDjPath   = regexp.MustCompile(`(.*)dj/`)
	regSlash    = regexp.MustCompile(`/(.*)`)
	regAmp      = regexp.MustCompile(`&(.*)`)
	regQuestion = regexp.MustCompile(`\?(.*)`)
	regInt      = regexp.MustCompile(`\d+`)
	regURL      = regexp.MustCompile(`(http|https)://[\w\-_]+(\.[\w\-_]+)+([\w\-.,@?^=%&:/~+#]*[\w\-@?^=%&/~+#])?`)
)

// MatchText attempts to extract a track ID from arbitrary text input.
// It supports direct URLs, program IDs, and plain numeric IDs. Short links are resolved upstream.
func (n *NeteasePlatform) MatchText(text string) (trackID string, matched bool) {
	cleaned := normalizeText(text)
	if cleaned == "" {
		return "", false
	}

	if urlStr := extractURL(cleaned); urlStr != "" {
		if id, ok := n.MatchURL(urlStr); ok {
			return id, true
		}
	}

	if programID := parseProgramID(cleaned); programID != 0 {
		if realID := getProgramRealID(programID); realID != 0 {
			return strconv.Itoa(realID), true
		}
	}

	if musicID := parseMusicID(cleaned); musicID != 0 {
		return strconv.Itoa(musicID), true
	}

	return "", false
}

func normalizeText(text string) string {
	replacer := strings.NewReplacer("\n", "", " ", "")
	return strings.TrimSpace(replacer.Replace(text))
}

func extractURL(text string) string {
	match := regURL.FindStringSubmatch(text)
	if len(match) == 0 {
		return ""
	}
	return match[0]
}

func parseMusicID(text string) int {
	messageText := normalizeText(text)
	if messageText == "" {
		return 0
	}
	urlStr := extractURL(messageText)
	if urlStr != "" && strings.Contains(urlStr, "song") {
		parsed, err := url.Parse(urlStr)
		if err == nil {
			id := parsed.Query().Get("id")
			if len(id) >= 5 {
				if musicID, _ := strconv.Atoi(id); musicID != 0 {
					return musicID
				}
			}
		}
	}
	if !isDigits(messageText) {
		return 0
	}
	if len(messageText) < 5 {
		return 0
	}
	musicID, _ := strconv.Atoi(messageText)
	return musicID
}

func parseProgramID(text string) int {
	messageText := normalizeText(text)
	programID, _ := strconv.Atoi(linkTestProgram(messageText))
	return programID
}

func linkTestProgram(text string) string {
	return extractInt(regSlash.ReplaceAllString(regAmp.ReplaceAllString(regQuestion.ReplaceAllString(regDjPath.ReplaceAllString(regDjQuery.ReplaceAllString(regProgramP.ReplaceAllString(regProgramQ.ReplaceAllString(text, ""), ""), ""), ""), ""), ""), ""))
}

func extractInt(text string) string {
	matchArr := regInt.FindStringSubmatch(text)
	if len(matchArr) == 0 {
		return ""
	}
	return matchArr[0]
}

func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
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
