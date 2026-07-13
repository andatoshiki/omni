package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/storage"
)

const (
	defaultSummaryMessageCount = 20
	maxSummaryMessageCount     = 100
	summaryUsage               = "❌ Usage: /summary [1-100]"
)

// Summary summarizes the most recent text messages in the Telegram transcript.
func Summary(ctx context.Context, b BotContext, msg *models.Message) {
	count, ok := parseSummaryMessageCount(msg.Text)
	if !ok {
		_, _ = b.Reply(ctx, msg, summaryUsage)
		return
	}

	messages, err := b.Store().RecentTranscriptMessages(msg.Chat.ID, msg.MessageThreadID, msg.ID, count)
	if err != nil {
		b.Logger().Error("failed to load summary transcript", append(b.MessageLogAttrs(msg), "message_count", count, "error", err)...)
		_, _ = b.Reply(ctx, msg, errorMessage(err))
		return
	}

	if len(messages) == 0 {
		_, _ = b.Reply(ctx, msg, "No recent text messages to summarize.")
		return
	}

	modelID := b.CurrentModel(msg.Chat.ID)
	params := b.Config()
	summaryPrompt := config.DefaultSummaryPrompt
	temperature := float32(1)
	maxTokens := 2048
	if params != nil {
		if strings.TrimSpace(params.SummaryPrompt) != "" {
			summaryPrompt = params.SummaryPrompt
		}
		temperature = float32(params.Temperature)
		maxTokens = params.MaxReplyTokens
	}
	if modelConfig := b.Providers().LookupModelConfig(modelID); modelConfig != nil {
		if modelConfig.Temperature != nil {
			temperature = *modelConfig.Temperature
		}
		if modelConfig.MaxReplyTokens > 0 {
			maxTokens = modelConfig.MaxReplyTokens
		}
	}

	request := &providers.ChatCompletionStreamRequest{
		Model:       modelID.Model,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Messages: []providers.ChatMessage{{
			Role:    providers.RoleUser,
			Content: buildSummaryPrompt(summaryPrompt, messages),
		}},
	}

	stream, err := b.Providers().CreateChatCompletionStream(ctx, modelID, request)
	if err != nil {
		b.Logger().Error("failed to create summary stream", append(b.MessageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)...)
		_, _ = b.Reply(ctx, msg, errorMessage(err))
		return
	}
	defer stream.Close()

	var summary strings.Builder
	for {
		response, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			b.Logger().Error("summary stream receive failed", append(b.MessageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", recvErr)...)
			_, _ = b.Reply(ctx, msg, errorMessage(recvErr))
			return
		}
		for _, choice := range response.Choices {
			summary.WriteString(choice.Delta.Content)
		}
	}

	summaryText := strings.TrimSpace(summary.String())
	if summaryText == "" {
		_, _ = b.Reply(ctx, msg, errorMessage(errors.New("AI provider returned an empty summary")))
		return
	}
	_, _ = b.Reply(ctx, msg, summaryText)
}

func parseSummaryMessageCount(argument string) (int, bool) {
	fields := strings.Fields(argument)
	if len(fields) == 0 {
		return defaultSummaryMessageCount, true
	}
	if len(fields) != 1 {
		return 0, false
	}

	count, err := strconv.Atoi(fields[0])
	if err != nil || count <= 0 {
		return 0, false
	}
	if count > maxSummaryMessageCount {
		count = maxSummaryMessageCount
	}
	return count, true
}

func buildSummaryPrompt(instruction string, messages []storage.TranscriptMessage) string {
	var prompt strings.Builder
	prompt.WriteString(strings.TrimSpace(instruction))
	prompt.WriteString("\n\nConversation:\n")
	for _, message := range messages {
		label := summaryRoleLabel(message.Role)
		if message.Role == providers.RoleUser && strings.TrimSpace(message.Sender) != "" {
			sender := strings.Join(strings.Fields(message.Sender), " ")
			label = fmt.Sprintf("User (%s)", sender)
		}
		fmt.Fprintf(&prompt, "%s: %s\n", label, strings.TrimSpace(message.Text))
	}
	return strings.TrimSpace(prompt.String())
}

func summaryRoleLabel(role string) string {
	switch role {
	case providers.RoleUser:
		return "User"
	case providers.RoleAssistant:
		return "Assistant"
	default:
		return "Message"
	}
}
