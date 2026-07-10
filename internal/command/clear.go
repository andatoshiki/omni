package command

import (
	"context"

	"github.com/go-telegram/bot/models"
)

func Clear(ctx context.Context, b BotContext, msg *models.Message) {
	if err := b.ClearConversation(msg.Chat.ID); err != nil {
		_, _ = b.Reply(ctx, msg, errorMessage(err))
		return
	}
	_, _ = b.Reply(ctx, msg, "✅ Conversation history cleared")
}
