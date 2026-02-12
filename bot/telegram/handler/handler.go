package handler

import (
	"context"

	"github.com/mymmrac/telego"
)

// MessageHandler handles message-based commands.
type MessageHandler interface {
	Handle(ctx context.Context, b *telego.Bot, update *telego.Update)
}

// InlineHandler handles inline queries.
type InlineHandler interface {
	Handle(ctx context.Context, b *telego.Bot, update *telego.Update)
}

// ChosenInlineHandler handles chosen inline results.
type ChosenInlineHandler interface {
	Handle(ctx context.Context, b *telego.Bot, update *telego.Update)
}

// CallbackHandler handles callback queries.
type CallbackHandler interface {
	Handle(ctx context.Context, b *telego.Bot, update *telego.Update)
}
