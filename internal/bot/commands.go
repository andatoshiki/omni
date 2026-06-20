package bot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/version"
)

type CommandHandlerFunc func(ctx context.Context, msg *models.Message)

type Route struct {
	Handler     CommandHandlerFunc
	Description string
	Hidden      bool // True if we shouldn't show it in the Telegram menu
}

type CommandHandler struct {
	app        *App
	msgHistory sync.Map
	chatLocks  sync.Map
	routes     map[string]Route
}

func NewCommandHandler(app *App) *CommandHandler {
	c := &CommandHandler{
		app:    app,
		routes: make(map[string]Route),
	}

	c.routes["ping"] = Route{Handler: c.Ping, Description: "Check bot latency"}
	c.routes["version"] = Route{Handler: c.Version, Description: "Show bot version"}
	c.routes["model"] = Route{Handler: c.Model, Description: "Select AI model"}
	c.routes["clear"] = Route{Handler: c.Clear, Description: "Clear conversation history"}
	c.routes["usage"] = Route{Handler: c.Usage, Description: "Show token usage"}
	c.routes["dsusage"] = Route{Handler: c.Usage, Hidden: true}
	c.routes["setprompt"] = Route{Handler: c.SetPrompt, Description: "Set a custom system prompt"}
	c.routes["clearprompt"] = Route{Handler: c.ClearPrompt, Description: "Clear the custom prompt"}
	c.routes["export"] = Route{Handler: c.Export, Description: "Export conversation data"}
	c.routes["help"] = Route{Handler: c.Help, Description: "Show help message"}
	c.routes["dshelp"] = Route{Handler: c.Help, Hidden: true}
	c.routes["dsclear"] = Route{Handler: c.Clear, Hidden: true}
	c.routes["dsexport"] = Route{Handler: c.Export, Hidden: true}
	c.routes["dssetprompt"] = Route{Handler: c.SetPrompt, Hidden: true}
	c.routes["dsclearprompt"] = Route{Handler: c.ClearPrompt, Hidden: true}
	c.routes["start"] = Route{Handler: c.Start, Hidden: true}

	return c
}

func (c *CommandHandler) lockChat(chatID int64) func() {
	value, _ := c.chatLocks.LoadOrStore(chatID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

func (c *CommandHandler) ClearConversation(chatID int64) error {
	unlock := c.lockChat(chatID)
	defer unlock()
	if err := c.app.store.ClearConversation(chatID); err != nil {
		return err
	}
	c.msgHistory.Delete(chatID)
	return nil
}

func (c *CommandHandler) reply(ctx context.Context, msg *models.Message, text string) (replyMsg *models.Message, err error) {
	if msg == nil {
		return nil, errors.New("cannot reply to a nil message")
	}
	if msg.Chat.ID >= 0 {
		return c.app.sendMessage(ctx, msg.Chat.ID, text)
	}
	return c.app.sendReplyToMessage(ctx, msg, text)
}

func (c *CommandHandler) editReply(ctx context.Context, msg *models.Message, replyMsg *models.Message, text string) (replyMessage *models.Message, err error) {
	if replyMsg == nil || msg == nil {
		return c.reply(ctx, msg, text)
	}

	return c.app.editReplyToMessage(ctx, replyMsg, text)
}

func (c *CommandHandler) currentModel(chatID int64) providers.ModelID {
	if selected, ok := c.app.store.LoadChatModel(chatID); ok {
		if _, err := c.app.providers.Resolve(selected); err == nil {
			return selected
		}
	}
	return c.app.providers.DefaultModelID()
}

func (c *CommandHandler) Usage(ctx context.Context, msg *models.Message) {
	userID := int64(0)
	if msg.From != nil {
		userID = msg.From.ID
	}
	summary, err := c.app.store.GetTokenUsage(msg.Chat.ID, userID)
	if err != nil {
		attrs := append(c.app.messageLogAttrs(msg), "error", err)
		c.app.logger.Error("token usage lookup failed", attrs...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return
	}

	replyText := fmt.Sprintf(
		"📊 Token usage in this chat\n\nRequests: %d\nPrompt tokens: %d\nCompletion tokens: %d\nTotal tokens: %d",
		summary.Requests,
		summary.PromptTokens,
		summary.CompletionTokens,
		summary.TotalTokens,
	)

	// Show cost estimate if current model has pricing configured.
	modelID := c.currentModel(msg.Chat.ID)
	mc := c.app.providers.LookupModelConfig(modelID)
	if mc != nil && (mc.InputPrice > 0 || mc.OutputPrice > 0) {
		inputCost := float64(summary.PromptTokens) * mc.InputPrice / 1_000_000
		outputCost := float64(summary.CompletionTokens) * mc.OutputPrice / 1_000_000
		totalCost := inputCost + outputCost
		replyText += fmt.Sprintf(
			"\n\n💰 Estimated cost (%s pricing)\nInput:  $%.6f\nOutput: $%.6f\nTotal:  $%.6f",
			modelID.Model, inputCost, outputCost, totalCost,
		)
	}

	_, _ = c.reply(ctx, msg, replyText)
}

func (c *CommandHandler) Ping(ctx context.Context, msg *models.Message) {
	_, _ = c.reply(ctx, msg, pingReplyText(msg, time.Now()))
}

func pingReplyText(msg *models.Message, now time.Time) string {
	if msg == nil || msg.Date <= 0 {
		return "Pong! 0ms"
	}

	latency := now.Sub(time.Unix(int64(msg.Date), 0))
	if latency < 0 {
		latency = 0
	}
	return fmt.Sprintf("Pong! %dms", latency.Milliseconds())
}

func (c *CommandHandler) Model(ctx context.Context, msg *models.Message) {
	current := c.currentModel(msg.Chat.ID)
	allModels := c.app.providers.AllModelIDs()

	if len(allModels) == 0 {
		_, _ = c.reply(ctx, msg, errorMessage(errors.New("no models configured")))
		return
	}

	// Build inline keyboard
	var rows [][]models.InlineKeyboardButton
	for _, m := range allModels {
		label := m.String()
		if m.Provider == current.Provider && m.Model == current.Model {
			label = "✅ " + label
		}
		rows = append(rows, []models.InlineKeyboardButton{
			{Text: label, CallbackData: m.CallbackData()},
		})
	}

	keyboard := models.InlineKeyboardMarkup{InlineKeyboard: rows}

	if msg.Chat.ID >= 0 {
		_, _ = c.app.sendMessageWithKeyboard(ctx, msg.Chat.ID, "🤖 Select a model:", &keyboard)
	} else {
		_, _ = c.app.sendReplyWithKeyboard(ctx, msg, "🤖 Select a model:", &keyboard)
	}
}

func (c *CommandHandler) Help(ctx context.Context, msg *models.Message) {
	helpText := `🤖 **Omni**

A versatile, Go-based Telegram bot supporting multiple AI platforms; with persistent memory, model switching, and token tracking. Developed with ❤️ by [Anda Toshiki](https://t.me/toshikidev), open-sourced on [GitHub](https://github.com/andatoshiki/omni) under GPL-v3. Get your instance of Omni bot under minutes with minimal configuration!

**Available Commands:**
/model - Switch between AI models on the fly
/ping - Check the bot's network latency
/clear - Wipe your conversation history and start fresh
/usage - View your current token usage and estimated costs
/setprompt - Assign a custom personality or system prompt to the bot
/clearprompt - Revert to the default system prompt
/export - Download your entire chat history as a JSON file
/version - View build metadata and active Go environment
/help - Show this comprehensive help message`

	_, _ = c.app.sendReplyToMessage(ctx, msg, helpText)
}

func (c *CommandHandler) Clear(ctx context.Context, msg *models.Message) {
	if err := c.ClearConversation(msg.Chat.ID); err != nil {
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return
	}
	_, _ = c.reply(ctx, msg, "✅ Conversation history cleared")
}

func (c *CommandHandler) Export(ctx context.Context, msg *models.Message) {
	filename := time.Now().Format("2006-01-02-15-04-05") + "-memory-export.json"
	if err := c.app.store.ExportMemory(filename); err != nil {
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return
	}
	_, _ = c.reply(ctx, msg, "✅ Memory exported successfully.")

	file, err := os.Open(filename)
	if err != nil {
		c.app.logger.Error("failed to open export file", "error", err)
		return
	}
	defer file.Close()
	defer os.Remove(filename)

	_, err = c.app.client.SendDocument(ctx, &telegram.SendDocumentParams{
		ChatID: msg.Chat.ID,
		Document: &models.InputFileUpload{
			Filename: filename,
			Data:     file,
		},
	})
	if err != nil {
		c.app.logger.Error("failed to send export document", "error", err)
	}
}

func (c *CommandHandler) Start(ctx context.Context, msg *models.Message) {
	if msg.Chat.ID >= 0 {
		_, _ = c.app.sendReplyToMessage(ctx, msg, "🤖 Welcome! Send me a message or use /help to see available commands.")
	}
}

func (c *CommandHandler) SetPrompt(ctx context.Context, msg *models.Message) {
	if msg.Text == "" {
		_, _ = c.reply(ctx, msg, "❌ Please provide a prompt. Usage: /setprompt <your custom prompt>")
		return
	}
	if err := c.app.store.SaveUserContext(msg.Chat.ID, msg.Text); err != nil {
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return
	}
	_, _ = c.reply(ctx, msg, "✅ Custom prompt set for this chat!")
}

func (c *CommandHandler) ClearPrompt(ctx context.Context, msg *models.Message) {
	if err := c.app.store.SaveUserContext(msg.Chat.ID, ""); err != nil {
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return
	}
	_, _ = c.reply(ctx, msg, "✅ Custom prompt cleared! Falling back to the default.")
}

func (c *CommandHandler) Version(ctx context.Context, msg *models.Message) {
	text := fmt.Sprintf("Omni\nVersion: <code>%s</code>\nCommit: <code>%s</code>\nBuild time: <code>%s</code>\nGo: <code>%s</code>",
		version.Version, version.Commit, version.BuildTime, version.GoVersion())
	_, _ = c.reply(ctx, msg, text)
}
