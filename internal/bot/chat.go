package bot

import (
	"context"
	"database/sql"
	"errors"
	"io"
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

	modelID, request, history, storedUserPrompt, sessionID, err := c.prepareChatContext(ctx, chatID, input)
	if err != nil {
		// Error logging and replying is handled inside prepareChatContext to maintain context
		return
	}

	text, usage, streamErr := c.executeChatStream(ctx, msg, modelID, request, requestStartedAt)

	c.finalizeChatTurn(chatID, sessionID, userID, msg, input, history, storedUserPrompt, text, usage, streamErr)
}

func (c *CommandHandler) prepareChatContext(ctx context.Context, chatID int64, input ChatInput) (providers.ModelID, *providers.ChatCompletionStreamRequest, []conversation.Message, string, int64, error) {
	msg := input.Messages[0]

	// Session timeout and active session resolution
	activeSession, err := c.app.store.GetActiveSession(chatID)
	var sessionID int64
	createNew := false

	if errors.Is(err, sql.ErrNoRows) {
		createNew = true
	} else if err != nil {
		c.app.logger.Error("failed to resolve active session", append(c.app.messageLogAttrs(msg), "error", err)...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return providers.ModelID{}, nil, nil, "", 0, err
	} else {
		// SQLite returns string like "2006-01-02 15:04:05" for CURRENT_TIMESTAMP
		updatedAt, parseErr := time.Parse("2006-01-02 15:04:05", activeSession.UpdatedAt)
		if parseErr == nil && time.Since(updatedAt) > c.app.params.SessionTimeout {
			createNew = true
		} else if parseErr != nil {
			// if parse fails, fallback to standard RFC3339 just in case
			if updatedAtRFC, parseErr2 := time.Parse(time.RFC3339, activeSession.UpdatedAt); parseErr2 == nil && time.Since(updatedAtRFC) > c.app.params.SessionTimeout {
				createNew = true
			}
		}
	}

	if createNew {
		activeSession, err = c.app.store.CreateNewSession(chatID, "New Session")
		if err != nil {
			c.app.logger.Error("failed to create new session", append(c.app.messageLogAttrs(msg), "error", err)...)
			_, _ = c.reply(ctx, msg, errorMessage(err))
			return providers.ModelID{}, nil, nil, "", 0, err
		}
	}
	sessionID = activeSession.ID

	// Title generation logic if needed
	if !activeSession.TitleGenerated {
		go c.generateSessionTitle(chatID, sessionID, input)
	}

	var history []conversation.Message
	if stored, exists := c.msgHistory.Load(sessionID); exists {
		history = stored.([]conversation.Message)
	} else {
		loaded, err := c.app.store.LoadSession(chatID, sessionID)
		if err != nil {
			c.app.logger.Warn("failed to load session from database", append(c.app.messageLogAttrs(msg), "error", err)...)
		} else {
			history = loaded
			c.msgHistory.Store(sessionID, history)
		}
	}

	modelID := c.currentModel(chatID)
	if _, err := c.app.providers.Resolve(modelID); err != nil {
		c.app.logger.Error("failed to resolve ai provider", append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return modelID, nil, nil, "", 0, err
	}

	userMessage, storedUserPrompt, err := c.userMessage(ctx, input)
	if err != nil {
		c.app.logger.Error("failed to prepare user message", append(c.app.messageLogAttrs(msg), "error", err)...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return modelID, nil, nil, "", 0, err
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
		return modelID, nil, nil, "", 0, err
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

	return modelID, request, history, storedUserPrompt, sessionID, nil
}

func (c *CommandHandler) generateSessionTitle(chatID int64, sessionID int64, input ChatInput) {
	msg := input.Messages[0]
	modelID := c.currentModel(chatID)

	// Create a short prompt to generate a title
	prompt := "Summarize the user's message in 3 to 5 words to use as a chat title. Do not include quotes or extra text. User message: " + msg.Text

	request := &providers.ChatCompletionStreamRequest{
		Model:       modelID.Model,
		Temperature: 0.5,
		MaxTokens:   50,
		Messages: []providers.ChatMessage{
			{Role: providers.RoleUser, Content: prompt},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := c.app.providers.CreateChatCompletionStream(ctx, modelID, request)
	if err != nil {
		c.app.logger.Warn("failed to initialize title generation stream", "error", err)
		if err := c.app.store.UpdateSessionTitle(sessionID, "Chat from "+time.Now().Format("Jan 02 15:04"), false); err != nil {
			c.app.logger.Error("failed to set fallback title", "error", err)
		}
		// We don't update TitleGenerated so it can retry later
		return
	}
	defer stream.Close()

	var title string
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if err != io.EOF {
				c.app.logger.Warn("error while receiving title stream", "error", err)
			}
			break
		}
		for _, choice := range chunk.Choices {
			title += choice.Delta.Content
		}
	}

	if title == "" {
		c.app.logger.Warn("empty title generated")
		if err := c.app.store.UpdateSessionTitle(sessionID, "Chat from "+time.Now().Format("Jan 02 15:04"), false); err != nil {
			c.app.logger.Error("failed to set fallback title", "error", err)
		}
		return
	}

	if err := c.app.store.UpdateSessionTitle(sessionID, title, true); err != nil {
		c.app.logger.Error("failed to save generated title", "error", err)
	}
}
