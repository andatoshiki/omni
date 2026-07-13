package storage

import (
	"fmt"
	"strings"
)

const transcriptRetentionMessages = 100

func validateTranscriptMessage(message TranscriptMessage) (TranscriptMessage, error) {
	message.Text = strings.TrimSpace(message.Text)
	message.Sender = strings.TrimSpace(message.Sender)
	if message.ChatID == 0 {
		return TranscriptMessage{}, fmt.Errorf("transcript chat ID must not be zero")
	}
	if message.MessageID <= 0 {
		return TranscriptMessage{}, fmt.Errorf("transcript message ID must be greater than zero")
	}
	if message.Role == "" {
		return TranscriptMessage{}, fmt.Errorf("transcript role must not be empty")
	}
	if message.Text == "" {
		return TranscriptMessage{}, fmt.Errorf("transcript text must not be empty")
	}
	return message, nil
}

func normalizeTranscriptLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit > transcriptRetentionMessages {
		return transcriptRetentionMessages
	}
	return limit
}

func reverseTranscriptMessages(messages []TranscriptMessage) {
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
}
