package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/providers"
)

const minReplyIntervalPrivateChat = time.Second
const minReplyIntervalGroupChat = 3 * time.Second

// Chat handles one AI conversation turn. Turns for the same Telegram chat are
// serialized so their persisted histories cannot interleave.
func (c *CommandHandler) Chat(ctx context.Context, msgs ...*models.Message) {
	if len(msgs) == 0 {
		return
	}
	msg := msgs[0]

	requestStartedAt := time.Now()
	chatID := msg.Chat.ID
	unlock := c.lockChat(chatID)
	defer unlock()

	userID := int64(0)
	if msg.From != nil {
		userID = msg.From.ID
	}

	modelID, request, history, storedUserPrompt, err := c.prepareChatContext(ctx, chatID, msgs...)
	if err != nil {
		// Error logging and replying is handled inside prepareChatContext to maintain context
		return
	}

	text, usage, streamErr := c.executeChatStream(ctx, msg, modelID, request, requestStartedAt)

	c.finalizeChatTurn(chatID, userID, msg, history, storedUserPrompt, text, usage, streamErr)
}

func (c *CommandHandler) prepareChatContext(ctx context.Context, chatID int64, msgs ...*models.Message) (providers.ModelID, *providers.ChatCompletionStreamRequest, []providers.ChatMessage, string, error) {
	msg := msgs[0]
	var history []providers.ChatMessage
	if stored, exists := c.msgHistory.Load(chatID); exists {
		history = stored.([]providers.ChatMessage)
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

	userMessage, storedUserPrompt, err := c.userMessage(ctx, msgs...)
	if err != nil {
		c.app.logger.Error("failed to prepare user message", append(c.app.messageLogAttrs(msg), "error", err)...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return modelID, nil, nil, "", err
	}

	currentMessages := make([]providers.ChatMessage, 0, 2)
	if msg.ReplyToMessage != nil {
		currentMessages = append(currentMessages, providers.ChatMessage{
			Role:    providers.RoleAssistant,
			Content: msg.ReplyToMessage.Text,
		})
	}
	currentMessages = append(currentMessages, userMessage)

	maxContextTokens := c.app.params.MaxContextTokens
	if model := c.app.providers.LookupModelConfig(modelID); model != nil && model.MaxContextTokens > 0 {
		maxContextTokens = model.MaxContextTokens
	}

	initialPrompt := c.app.params.InitialPrompt
	if customPrompt, err := c.app.store.LoadUserContext(chatID); err == nil && customPrompt != "" {
		initialPrompt = customPrompt
	}

	requestMessages, promptTokens, droppedHistory, err := messagesWithinContext(
		providers.ChatMessage{Role: providers.RoleSystem, Content: initialPrompt},
		history,
		currentMessages,
		maxContextTokens,
		c.app.params.MaxReplyTokens,
	)
	if err != nil {
		c.app.logger.Error("failed to build ai context window", append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return modelID, nil, nil, "", err
	}

	request := &providers.ChatCompletionStreamRequest{
		Model:       modelID.Model,
		Temperature: float32(c.app.params.Temperature),
		MaxTokens:   c.app.params.MaxReplyTokens,
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

func (c *CommandHandler) executeChatStream(
	ctx context.Context,
	msg *models.Message,
	modelID providers.ModelID,
	request *providers.ChatCompletionStreamRequest,
	requestStartedAt time.Time,
) (string, *providers.TokenUsage, error) {
	stream, err := c.app.providers.CreateChatCompletionStream(ctx, modelID, request)
	if err != nil {
		c.app.logger.Error("failed to create ai chat stream", append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return "", nil, err
	}
	defer stream.Close()

	c.app.sendChatActionTyping(ctx, msg)
	text, usage, replyMsg, previewCapped, streamErr := c.receiveChatStream(ctx, msg, modelID, stream)
	c.logChatCompletion(msg, modelID, requestStartedAt, text, usage, previewCapped)

	if text != "" {
		if streamErr != nil {
			text += fmt.Sprintf("\n\n⚠️ _[Stream interrupted: %v]_", streamErr)
		}
		c.sendFinalChatReply(ctx, msg, replyMsg, text)
	} else {
		if replyMsg != nil {
			_, _ = c.app.deleteMessage(ctx, replyMsg)
		}
		if streamErr != nil {
			_, _ = c.reply(ctx, msg, errorMessage(streamErr))
		}
	}

	return text, usage, streamErr
}

func (c *CommandHandler) finalizeChatTurn(
	chatID int64,
	userID int64,
	msg *models.Message,
	history []providers.ChatMessage,
	storedUserPrompt string,
	text string,
	usage *providers.TokenUsage,
	streamErr error,
) {
	if text == "" && streamErr != nil {
		return // Do not save history if it completely failed
	}

	history = appendTurnToHistory(history, msg, storedUserPrompt, text, c.app.params.HistorySize)
	c.msgHistory.Store(chatID, history)

	if err := c.app.store.SaveConversation(chatID, history); err != nil {
		c.app.logger.Warn("failed to save conversation to database", append(c.app.messageLogAttrs(msg), "history_messages", len(history), "error", err)...)
	}

	if usage != nil {
		if err := c.app.store.SaveTokenUsage(chatID, userID, *usage); err != nil {
			c.app.logger.Warn("failed to save token usage", append(c.app.messageLogAttrs(msg), "error", err)...)
		}
	}
}

func (c *CommandHandler) receiveChatStream(
	ctx context.Context,
	msg *models.Message,
	modelID providers.ModelID,
	stream providers.ChatCompletionStream,
) (text string, usage *providers.TokenUsage, replyMsg *models.Message, previewCapped bool, streamErr error) {
	lastReplyEditAt := time.Now()
	minReplyInterval := minReplyIntervalPrivateChat
	if msg.Chat.ID < 0 {
		minReplyInterval = minReplyIntervalGroupChat
	}
	lastSentText := ""

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			attrs := append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)
			c.app.logger.Error("ai chat stream receive failed", attrs...)
			streamErr = err
			break
		}
		if response.Usage != nil {
			usage = response.Usage
		}
		for _, choice := range response.Choices {
			text += choice.Delta.Content
			if time.Since(lastReplyEditAt) <= minReplyInterval || text == "" || text == lastSentText {
				continue
			}
			if len(text) <= streamPreviewLimit {
				replyMsg, _ = c.editReply(ctx, msg, replyMsg, text)
				lastSentText = text
			} else if !previewCapped {
				replyMsg, _ = c.editReply(ctx, msg, replyMsg, truncateUTF8(text, streamPreviewLimit)+"\n\n⏳ _Response is long, sending in full when complete…_")
				previewCapped = true
			}
			lastReplyEditAt = time.Now()
		}
	}

	if time.Since(lastReplyEditAt) < minReplyInterval && text != lastSentText {
		time.Sleep(minReplyInterval - time.Since(lastReplyEditAt))
	}
	return text, usage, replyMsg, previewCapped, streamErr
}

func (c *CommandHandler) logChatCompletion(
	msg *models.Message,
	modelID providers.ModelID,
	startedAt time.Time,
	text string,
	usage *providers.TokenUsage,
	previewCapped bool,
) {
	attrs := append(
		c.app.messageLogAttrs(msg),
		"provider", modelID.Provider,
		"model", modelID.Model,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"preview_capped", previewCapped,
		"response_chunks", len(splitText(text, streamPreviewLimit)),
	)
	attrs = append(attrs, c.app.textMetricAttrs("response", text)...)
	if usage != nil {
		attrs = append(
			attrs,
			"prompt_tokens", usage.PromptTokens,
			"completion_tokens", usage.CompletionTokens,
			"total_tokens", usage.TotalTokens,
		)
	}
	c.app.logger.Info("ai chat response completed", attrs...)
}

func (c *CommandHandler) sendFinalChatReply(ctx context.Context, msg, replyMsg *models.Message, text string) {
	if len(text) <= streamPreviewLimit {
		finalReplyMsg, err := c.editReply(ctx, msg, replyMsg, text)
		if err != nil {
			_, _ = c.app.deleteMessage(ctx, finalReplyMsg)
			_, _ = c.app.sendMessageInThread(ctx, msg.Chat.ID, msg.MessageThreadID, text)
		}
		return
	}

	_, _ = c.app.deleteMessage(ctx, replyMsg)
	if msg.Chat.ID >= 0 {
		c.app.sendLongMessage(ctx, msg.Chat.ID, text)
		return
	}
	c.app.sendLongReply(ctx, msg, text)
}

func appendTurnToHistory(history []providers.ChatMessage, msg *models.Message, userPrompt, text string, maxMessages int) []providers.ChatMessage {
	if msg.ReplyToMessage != nil {
		history = append(history, providers.ChatMessage{
			Role:    providers.RoleAssistant,
			Content: msg.ReplyToMessage.Text,
		})
	}
	history = append(history,
		providers.ChatMessage{Role: providers.RoleUser, Content: userPrompt},
		providers.ChatMessage{Role: providers.RoleAssistant, Content: text},
	)
	if len(history) > maxMessages {
		return history[len(history)-maxMessages:]
	}
	return history
}
