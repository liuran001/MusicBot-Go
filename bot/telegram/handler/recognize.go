package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// RecognizeHandler handles voice recognition.
type RecognizeHandler struct {
	CacheDir string
	Music    *MusicHandler
}

var recognizeAPI = "https://music-recognize.vercel.app/api/recognize"

func (h *RecognizeHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}
	message := update.Message
	chatID := message.Chat.ID
	replyID := message.ID

	if message.ReplyToMessage == nil || message.ReplyToMessage.Voice == nil {
		sendText(ctx, b, chatID, replyID, "请回复一条语音留言")
		return
	}
	replyID = message.ReplyToMessage.ID

	if h.CacheDir == "" {
		h.CacheDir = "./cache"
	}
	ensureDir(h.CacheDir)

	fileInfo, err := b.GetFile(ctx, &bot.GetFileParams{FileID: message.ReplyToMessage.Voice.FileID})
	if err != nil || fileInfo == nil || fileInfo.FilePath == "" {
		sendText(ctx, b, chatID, replyID, "获取语音失败，请稍后重试")
		return
	}
	if fileInfo.FileSize > 20*1024*1024 {
		sendText(ctx, b, chatID, replyID, "语音过大，无法识别")
		return
	}
	fileURL := b.FileDownloadLink(fileInfo)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		sendText(ctx, b, chatID, replyID, "下载语音失败，请稍后重试")
		return
	}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		sendText(ctx, b, chatID, replyID, "下载语音失败，请稍后重试")
		return
	}
	defer resp.Body.Close()

	fileName := fmt.Sprintf("%d-%d-%d.ogg", message.ReplyToMessage.Chat.ID, message.ReplyToMessage.ID, time.Now().Unix())
	oggPath := fmt.Sprintf("%s/%s", h.CacheDir, fileName)
	file, err := os.OpenFile(oggPath, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		sendText(ctx, b, chatID, replyID, "保存语音失败，请稍后重试")
		return
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		sendText(ctx, b, chatID, replyID, "保存语音失败，请稍后重试")
		return
	}
	_ = file.Close()
	defer os.Remove(oggPath)

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		sendText(ctx, b, chatID, replyID, "服务器未安装 ffmpeg，无法识别")
		return
	}
	ffmpegCtx, ffmpegCancel := context.WithTimeout(ctx, 30*time.Second)
	defer ffmpegCancel()
	cmd := exec.CommandContext(ffmpegCtx, "ffmpeg", "-i", oggPath, oggPath+".mp3")
	if err := cmd.Run(); err != nil {
		sendText(ctx, b, chatID, replyID, "音频转换失败，请稍后重试")
		return
	}
	if _, err := os.Stat(oggPath + ".mp3"); err != nil {
		sendText(ctx, b, chatID, replyID, "音频转换失败，请稍后重试")
		return
	}
	defer os.Remove(oggPath + ".mp3")

	newFile, err := os.Open(oggPath + ".mp3")
	if err != nil {
		sendText(ctx, b, chatID, replyID, "读取音频失败，请稍后重试")
		return
	}
	defer newFile.Close()

	respBody, err := uploadFile(ctx, client, recognizeAPI, newFile)
	if err != nil {
		sendText(ctx, b, chatID, replyID, "识别服务暂不可用，请稍后重试")
		return
	}

	var result RecognizeResultData
	if err := json.Unmarshal(respBody, &result); err != nil {
		sendText(ctx, b, chatID, replyID, "识别失败，请稍后重试")
		return
	}
	if len(result.Data.Result) == 0 {
		sendText(ctx, b, chatID, replyID, "识别失败，可能是录音时间太短")
		return
	}

	musicID := result.Data.Result[0].Song.Id
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          chatID,
		Text:            fmt.Sprintf("https://music.163.com/song/%d", musicID),
		ReplyParameters: &models.ReplyParameters{MessageID: replyID},
	})

	if h.Music != nil {
		// Recognition currently returns NetEase musicID
		h.Music.dispatch(ctx, b, message.ReplyToMessage, "netease", fmt.Sprintf("%d", musicID), "")
	}
}

func uploadFile(ctx context.Context, client *http.Client, url string, file io.Reader) ([]byte, error) {
	bodyBuf := &bytes.Buffer{}
	writer := multipart.NewWriter(bodyBuf)
	fileWriter, err := writer.CreateFormFile("file", "")
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(fileWriter, file)
	if err != nil {
		return nil, err
	}
	contentType := writer.FormDataContentType()
	_ = writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyBuf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("recognize api error")
	}
	return io.ReadAll(resp.Body)
}

type RecognizeResultData struct {
	Data struct {
		Result []struct {
			Song struct {
				Name string `json:"name"`
				Id   int    `json:"id"`
			} `json:"song"`
		} `json:"result"`
	} `json:"data"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func sendText(ctx context.Context, b *bot.Bot, chatID int64, replyID int, text string) {
	if b == nil {
		return
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:          chatID,
		Text:            text,
		ReplyParameters: &models.ReplyParameters{MessageID: replyID},
	})
}
