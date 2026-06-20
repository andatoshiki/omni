package bot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/providers"
)

type CommandHandler struct {
	app        *App
	msgHistory sync.Map
	chatLocks  sync.Map
}

func NewCommandHandler(app *App) *CommandHandler {
	return &CommandHandler{
		app: app,
	}
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

func (c *CommandHandler) Help(ctx context.Context, msg *models.Message, cmdChar string) {
	_, _ = c.app.sendReplyToMessage(ctx, msg, "🤖 AI Telegram Bot\n\n"+
		"Available commands:\n\n"+
		cmdChar+"model - select AI model\n"+
		cmdChar+"ping - check bot latency\n"+
		cmdChar+"clear - clear conversation history\n"+
		cmdChar+"usage - show your token usage in this chat\n"+
		cmdChar+"setprompt - set a custom system prompt\n"+
		cmdChar+"clearprompt - clear the custom prompt\n"+
		cmdChar+"help - show this help\n"+
		cmdChar+"export - export all memories")
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
