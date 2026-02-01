package handler

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// MessageHandler handles message-based commands.
type MessageHandler interface {
	Handle(ctx context.Context, b *bot.Bot, update *models.Update)
}

// InlineHandler handles inline queries.
type InlineHandler interface {
	Handle(ctx context.Context, b *bot.Bot, update *models.Update)
}

// CallbackHandler handles callback queries.
type CallbackHandler interface {
	Handle(ctx context.Context, b *bot.Bot, update *models.Update)
}
