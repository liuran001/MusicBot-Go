package telegram

import (
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mymmrac/telego"
)

const aprilFoolsTOSMessage = "This message couldn't be displayed on your device because it violates the Telegram Terms of Service."

var (
	aprilFoolsTZ      = time.FixedZone("UTC+8", 8*60*60)
	aprilFoolsEnabled atomic.Bool
)

func SetAprilFoolsEnabled(enabled bool) {
	aprilFoolsEnabled.Store(enabled)
}

func isAprilFoolsDayNow() bool {
	now := time.Now().In(aprilFoolsTZ)
	return now.Month() == time.April && now.Day() == 1
}

func shouldApplyAprilFoolsTextPrank() bool {
	if !aprilFoolsEnabled.Load() || !isAprilFoolsDayNow() {
		return false
	}
	return rand.Float64() < 0.01
}

func buildAprilFoolsFeedbackMarkup(botUsername string, existing *telego.InlineKeyboardMarkup) *telego.InlineKeyboardMarkup {
	username := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(botUsername), "@"))
	if username == "" {
		return existing
	}
	feedbackURL := fmt.Sprintf("https://t.me/%s?start=cache_netease_18520488", username)
	feedbackRow := []telego.InlineKeyboardButton{{Text: "Submit Feedback", URL: feedbackURL}}

	return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{feedbackRow}}
}

func maybeApplyAprilFoolsTextPrank(text *string, parseMode *string) bool {
	if text == nil || parseMode == nil {
		return false
	}
	if !shouldApplyAprilFoolsTextPrank() {
		return false
	}
	*text = aprilFoolsTOSMessage
	*parseMode = telego.ModeHTML
	return true
}

func maybeApplyAprilFoolsTextPrankWithMarkup(botUsername string, text *string, parseMode *string, replyMarkup **telego.InlineKeyboardMarkup) {
	if !maybeApplyAprilFoolsTextPrank(text, parseMode) {
		return
	}
	if replyMarkup != nil {
		*replyMarkup = buildAprilFoolsFeedbackMarkup(botUsername, *replyMarkup)
	}
}

func maybeApplyAprilFoolsTextPrankToSendMessage(botUsername string, params *telego.SendMessageParams) {
	if params == nil {
		return
	}
	if !maybeApplyAprilFoolsTextPrank(&params.Text, &params.ParseMode) {
		return
	}
	var existing *telego.InlineKeyboardMarkup
	if markup, ok := params.ReplyMarkup.(*telego.InlineKeyboardMarkup); ok {
		existing = markup
	}
	params.ReplyMarkup = buildAprilFoolsFeedbackMarkup(botUsername, existing)
}
