package command

import (
	"context"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/storage"
)

func TestParseSummaryMessageCount(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
		ok    bool
	}{
		{name: "default", input: "", want: 20, ok: true},
		{name: "whitespace default", input: "  ", want: 20, ok: true},
		{name: "explicit", input: "5", want: 5, ok: true},
		{name: "maximum", input: "100", want: 100, ok: true},
		{name: "capped", input: "101", want: 100, ok: true},
		{name: "non-integer", input: "abc", ok: false},
		{name: "zero", input: "0", ok: false},
		{name: "negative", input: "-1", ok: false},
		{name: "extra argument", input: "5 now", ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseSummaryMessageCount(test.input)
			if got != test.want || ok != test.ok {
				t.Fatalf("parseSummaryMessageCount(%q) = (%d, %t), want (%d, %t)", test.input, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestBuildSummaryPromptIncludesTranscriptSpeakerAndOrder(t *testing.T) {
	messages := []storage.TranscriptMessage{
		{Role: providers.RoleUser, Sender: "Alice\nSmith", Text: "first"},
		{Role: providers.RoleAssistant, Text: "second"},
	}

	got := buildSummaryPrompt("Summarize this.", messages)
	if !strings.Contains(got, "User (Alice Smith): first\nAssistant: second") {
		t.Fatalf("buildSummaryPrompt() = %q, want chronological speaker labels", got)
	}
	if !strings.HasPrefix(got, "Summarize this.\n\nConversation:\n") {
		t.Fatalf("buildSummaryPrompt() = %q, want instruction before transcript", got)
	}
}

func TestSummaryQueriesTranscriptWithoutActiveSession(t *testing.T) {
	store := &summaryTranscriptStore{}
	bot := &summaryBotContext{
		testBotContext: testBotContext{
			store:  store,
			params: &config.Params{Temperature: 1, MaxReplyTokens: 128},
		},
	}

	Summary(context.Background(), bot, &models.Message{
		ID: 100, MessageThreadID: 7, Chat: models.Chat{ID: 42},
	})

	if store.gotChatID != 42 || store.gotThreadID != 7 || store.gotBeforeID != 100 || store.gotLimit != 20 {
		t.Fatalf("transcript query = chat %d thread %d before %d limit %d", store.gotChatID, store.gotThreadID, store.gotBeforeID, store.gotLimit)
	}
	if len(bot.replies) != 1 || bot.replies[0] != "No recent text messages to summarize." {
		t.Fatalf("replies = %#v, want empty transcript reply", bot.replies)
	}
}

type summaryTranscriptStore struct {
	storage.Store
	messages    []storage.TranscriptMessage
	gotChatID   int64
	gotThreadID int
	gotBeforeID int
	gotLimit    int
}

func (s *summaryTranscriptStore) RecentTranscriptMessages(chatID int64, threadID, beforeMessageID, limit int) ([]storage.TranscriptMessage, error) {
	s.gotChatID = chatID
	s.gotThreadID = threadID
	s.gotBeforeID = beforeMessageID
	s.gotLimit = limit
	return s.messages, nil
}

type summaryBotContext struct {
	testBotContext
	replies []string
}

func (b *summaryBotContext) Reply(_ context.Context, _ *models.Message, text string) (*models.Message, error) {
	b.replies = append(b.replies, text)
	return nil, nil
}
