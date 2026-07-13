package bot

import (
	"context"
	"log/slog"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/storage"
)

func TestTelegramMessageTextUsesTextThenCaption(t *testing.T) {
	tests := []struct {
		name string
		msg  *models.Message
		want string
	}{
		{name: "text", msg: &models.Message{Text: " hello ", Caption: "caption"}, want: "hello"},
		{name: "caption", msg: &models.Message{Caption: " video caption "}, want: "video caption"},
		{name: "media only", msg: &models.Message{Video: &models.Video{}}, want: ""},
		{name: "nil", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := telegramMessageText(test.msg); got != test.want {
				t.Fatalf("telegramMessageText() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTelegramCommandsAreExcludedFromTranscript(t *testing.T) {
	for _, text := range []string{"/summary", "/summary@bot 10", "!clear"} {
		if !isTelegramCommandText(text) {
			t.Fatalf("isTelegramCommandText(%q) = false, want true", text)
		}
	}
	for _, text := range []string{"hello", "please /summary this", ""} {
		if isTelegramCommandText(text) {
			t.Fatalf("isTelegramCommandText(%q) = true, want false", text)
		}
	}
}

func TestUnsupportedMediaCaptionIsTranscriptOnly(t *testing.T) {
	msg := &models.Message{Caption: "animated caption", Animation: &models.Animation{}}
	if got := telegramMessageText(msg); got != "animated caption" {
		t.Fatalf("telegramMessageText() = %q", got)
	}
	if isRoutableTelegramMessage(msg) {
		t.Fatal("animation unexpectedly changed live chat routing")
	}
}

func TestHandleMessageCapturesUnsupportedMediaCaptionForSummary(t *testing.T) {
	store := &transcriptCaptureStore{}
	app := &App{
		params: &config.Params{AllowedUserIDs: []int64{42}},
		store:  store,
		logger: slog.Default(),
	}
	app.handleMessage(context.Background(), &models.Update{Message: &models.Message{
		ID: 12, MessageThreadID: 3,
		Chat: models.Chat{ID: 42}, From: &models.User{ID: 42, FirstName: "Alice"},
		Caption: "animated caption", Animation: &models.Animation{},
	}})

	if len(store.messages) != 1 {
		t.Fatalf("captured messages = %#v, want one caption", store.messages)
	}
	got := store.messages[0]
	if got.ChatID != 42 || got.ThreadID != 3 || got.MessageID != 12 || got.Role != providers.RoleUser || got.Sender != "Alice" || got.Text != "animated caption" {
		t.Fatalf("captured message = %#v", got)
	}
}

func TestAssistantTranscriptUsesFinalTelegramMessage(t *testing.T) {
	store := &transcriptCaptureStore{}
	handler := &CommandHandler{app: &App{store: store, logger: slog.Default(), botUsername: "omni_bot"}}
	handler.saveAssistantTranscriptMessage(
		&models.Message{ID: 10, MessageThreadID: 4, Chat: models.Chat{ID: -100}},
		&models.Message{ID: 11, Chat: models.Chat{ID: -100}},
		"final answer",
	)

	if len(store.messages) != 1 {
		t.Fatalf("captured messages = %#v, want one assistant reply", store.messages)
	}
	got := store.messages[0]
	if got.ChatID != -100 || got.ThreadID != 4 || got.MessageID != 11 || got.Role != providers.RoleAssistant || got.Sender != "omni_bot" || got.Text != "final answer" {
		t.Fatalf("captured message = %#v", got)
	}
}

type transcriptCaptureStore struct {
	storage.Store
	messages []storage.TranscriptMessage
}

func (s *transcriptCaptureStore) SaveTranscriptMessage(message storage.TranscriptMessage) error {
	s.messages = append(s.messages, message)
	return nil
}
