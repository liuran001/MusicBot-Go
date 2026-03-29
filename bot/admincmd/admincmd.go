package admincmd

import (
	"context"

	"github.com/mymmrac/telego"
)

type chatIDContextKey struct{}

type Response struct {
	Text        string
	Photo       []byte
	PhotoName   string
	ReplyMarkup *telego.InlineKeyboardMarkup
	AfterSend   func(ctx context.Context, b *telego.Bot, sent *telego.Message)
}

type Command struct {
	Name            string
	Description     string
	Handler         func(ctx context.Context, args string) (string, error)
	RichHandler     func(ctx context.Context, args string) (*Response, error)
	CallbackPrefix  string
	CallbackHandler func(ctx context.Context, b *telego.Bot, query *telego.CallbackQuery) error
}

func WithChatID(ctx context.Context, chatID int64) context.Context {
	return context.WithValue(ctx, chatIDContextKey{}, chatID)
}

func ChatIDFromContext(ctx context.Context) (int64, bool) {
	if ctx == nil {
		return 0, false
	}
	chatID, ok := ctx.Value(chatIDContextKey{}).(int64)
	return chatID, ok
}
