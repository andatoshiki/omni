package command

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/version"
)

func Help(ctx context.Context, b BotContext, msg *models.Message) {
	helpText := `🤖 **Omni**

A versatile, Go-based Telegram bot supporting multiple AI platforms; with persistent memory, model switching, and token tracking. Developed with ❤️ by [Anda Toshiki](https://t.me/toshikidev), open-sourced on [GitHub](https://github.com/andatoshiki/omni) under GPL-v3. Get your instance of Omni bot under minutes with minimal configuration!

**Available Commands:**
/model - Switch between AI models on the fly
/ping - Check the bot's network latency
/clear - Clear AI chat context and start fresh
/usage - View your current token usage and estimated costs
/setprompt - Assign a custom personality or system prompt to the bot
/clearprompt - Revert to the default system prompt
/export - Download your entire chat history as a JSON file
/summary - Summarize the most recent text messages in this chat or topic
/version - View build metadata and active Go environment
/help - Show this comprehensive help message`

	_, _ = b.SendReplyToMessage(ctx, msg, helpText)
}

func Start(ctx context.Context, b BotContext, msg *models.Message) {
	if msg.Chat.ID >= 0 {
		_, _ = b.SendReplyToMessage(ctx, msg, "🤖 Welcome! Send me a message or use /help to see available commands.")
	}
}

func Version(ctx context.Context, b BotContext, msg *models.Message) {
	text := fmt.Sprintf("Omni\nVersion: <code>%s</code>\nCommit: <code>%s</code>\nBuild time: <code>%s</code>\nGo: <code>%s</code>",
		version.Version, version.Commit, version.BuildTime, version.GoVersion())
	_, _ = b.Reply(ctx, msg, text)
}
