package bot

import (
	"context"
	"time"

	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

const minReplyIntervalPrivateChat = time.Second
const minReplyIntervalGroupChat = 3 * time.Second

// Chat handles one AI conversation turn. Turns for the same Telegram chat are
// serialized so their persisted histories cannot interleave.
func (c *CommandHandler) Chat(ctx context.Context, input ChatInput) {
	if len(input.Messages) == 0 {
		return
	}
	msg := input.Messages[0]

	requestStartedAt := time.Now()
	chatID := msg.Chat.ID
	unlock := c.lockChat(chatID)
	defer unlock()

	userID := int64(0)
	if msg.From != nil {
		userID = msg.From.ID
	}

	modelID, request, history, storedUserPrompt, err := c.prepareChatContext(ctx, chatID, input)
	if err != nil {
		// Error logging and replying is handled inside prepareChatContext to maintain context
		return
	}

	text, usage, streamErr := c.executeChatStream(ctx, msg, modelID, request, requestStartedAt)

	c.finalizeChatTurn(chatID, userID, msg, input, history, storedUserPrompt, text, usage, streamErr)
}

func (c *CommandHandler) prepareChatContext(ctx context.Context, chatID int64, input ChatInput) (providers.ModelID, *providers.ChatCompletionStreamRequest, []conversation.Message, string, error) {
	msg := input.Messages[0]
	var history []conversation.Message
	if stored, exists := c.msgHistory.Load(chatID); exists {
		history = stored.([]conversation.Message)
	} else {
		loaded, err := c.app.store.LoadConversation(chatID)
		if err != nil {
			c.app.logger.Warn("failed to load conversation from database", append(c.app.messageLogAttrs(msg), "error", err)...)
		} else {
			history = loaded
			c.msgHistory.Store(chatID, history)
		}
	}

	modelID := c.currentModel(chatID)
	if _, err := c.app.providers.Resolve(modelID); err != nil {
		c.app.logger.Error("failed to resolve ai provider", append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return modelID, nil, nil, "", err
	}

	userMessage, storedUserPrompt, err := c.userMessage(ctx, input)
	if err != nil {
		c.app.logger.Error("failed to prepare user message", append(c.app.messageLogAttrs(msg), "error", err)...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return modelID, nil, nil, "", err
	}

	currentMessages := make([]conversation.Message, 0, 1)
	currentMessages = append(currentMessages, userMessage)

	maxContextTokens := c.app.params.MaxContextTokens
	if model := c.app.providers.LookupModelConfig(modelID); model != nil && model.MaxContextTokens > 0 {
		maxContextTokens = model.MaxContextTokens
	}

	initialPrompt := c.app.params.InitialPrompt
	if customPrompt, err := c.app.store.LoadUserContext(chatID); err == nil && customPrompt != "" {
		initialPrompt = customPrompt
	}

	includeIdentity := false
	if c.app.params.SenderContext == "all" || (c.app.params.SenderContext == "groups" && chatID < 0) {
		includeIdentity = true
		initialPrompt += "\n\n" + conversation.SystemInstruction
	}

	renderedHistory := conversation.Render(history, includeIdentity)
	renderedCurrent := conversation.Render(currentMessages, includeIdentity)

	maxReplyTokens := c.app.params.MaxReplyTokens
	if model := c.app.providers.LookupModelConfig(modelID); model != nil && model.MaxReplyTokens > 0 {
		maxReplyTokens = model.MaxReplyTokens
	}

	requestMessages, promptTokens, droppedHistory, err := messagesWithinContext(
		providers.ChatMessage{Role: providers.RoleSystem, Content: initialPrompt},
		renderedHistory,
		renderedCurrent,
		maxContextTokens,
		maxReplyTokens,
	)
	if err != nil {
		c.app.logger.Error("failed to build ai context window", append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return modelID, nil, nil, "", err
	}

	request := &providers.ChatCompletionStreamRequest{
		Model:       modelID.Model,
		Temperature: float32(c.app.params.Temperature),
		MaxTokens:   maxReplyTokens,
		Messages:    requestMessages,
	}

	if modelConfig := c.app.providers.LookupModelConfig(modelID); modelConfig != nil && modelConfig.Temperature != nil {
		request.Temperature = *modelConfig.Temperature
	}

	c.app.logger.Info(
		"ai chat request started",
		append(
			c.app.messageLogAttrs(msg),
			"provider", modelID.Provider,
			"model", modelID.Model,
			"history_messages", len(history),
			"history_messages_dropped", droppedHistory,
			"request_messages", len(request.Messages),
			"estimated_prompt_tokens", promptTokens,
			"max_context_tokens", maxContextTokens,
		)...,
	)

	return modelID, request, history, storedUserPrompt, nil
}

