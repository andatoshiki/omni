package command

import (
	"context"

	"github.com/go-telegram/bot/models"
)

func SetPrompt(ctx context.Context, b BotContext, msg *models.Message) {
	if msg.Text == "" {
		_, _ = b.Reply(ctx, msg, "❌ Please provide a prompt. Usage: /setprompt <your custom prompt>")
		return
	}
	if err := b.Store().SaveUserContext(msg.Chat.ID, msg.Text); err != nil {
		_, _ = b.Reply(ctx, msg, errorMessage(err))
		return
	}
	_, _ = b.Reply(ctx, msg, "✅ Custom prompt set for this chat!")
}

func ClearPrompt(ctx context.Context, b BotContext, msg *models.Message) {
	if err := b.Store().SaveUserContext(msg.Chat.ID, ""); err != nil {
		_, _ = b.Reply(ctx, msg, errorMessage(err))
		return
	}
	_, _ = b.Reply(ctx, msg, "✅ Custom prompt cleared! Falling back to the default.")
}
