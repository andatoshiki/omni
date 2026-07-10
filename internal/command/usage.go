package command

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot/models"
)

func Usage(ctx context.Context, b BotContext, msg *models.Message) {
	userID := int64(0)
	if msg.From != nil {
		userID = msg.From.ID
	}
	summary, err := b.Store().GetTokenUsage(msg.Chat.ID, userID)
	if err != nil {
		attrs := append(b.MessageLogAttrs(msg), "error", err)
		b.Logger().Error("token usage lookup failed", attrs...)
		_, _ = b.Reply(ctx, msg, errorMessage(err))
		return
	}

	replyText := fmt.Sprintf(
		"📊 Token usage in this chat\n\nRequests: %d\nPrompt tokens: %d\nCompletion tokens: %d\nTotal tokens: %d",
		summary.Requests,
		summary.PromptTokens,
		summary.CompletionTokens,
		summary.TotalTokens,
	)

	modelID := b.CurrentModel(msg.Chat.ID)
	mc := b.Providers().LookupModelConfig(modelID)
	if mc != nil && (mc.InputPrice > 0 || mc.OutputPrice > 0) {
		inputCost := float64(summary.PromptTokens) * mc.InputPrice / 1_000_000
		outputCost := float64(summary.CompletionTokens) * mc.OutputPrice / 1_000_000
		totalCost := inputCost + outputCost
		replyText += fmt.Sprintf(
			"\n\n💰 Estimated cost (%s pricing)\nInput:  $%.6f\nOutput: $%.6f\nTotal:  $%.6f",
			modelID.Model, inputCost, outputCost, totalCost,
		)
	}

	_, _ = b.Reply(ctx, msg, replyText)
}
