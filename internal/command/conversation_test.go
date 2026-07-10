package command

import (
	"context"
	"log/slog"
	"testing"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/storage"
)

func TestSessionListViewFetchesSentinelForNextPage(t *testing.T) {
	store := &sessionListStore{sessions: testSessions(11)}
	bot := testBotContext{
		store:  store,
		params: &config.Params{MaxSessionsDisplayed: 10},
	}

	_, keyboard, err := sessionListView(bot, 42, 0)
	if err != nil {
		t.Fatalf("sessionListView() error = %v", err)
	}

	if store.gotLimit != 11 {
		t.Fatalf("ListSessions limit = %d, want 11", store.gotLimit)
	}
	if store.gotChatID != 42 {
		t.Fatalf("ListSessions chat ID = %d, want 42", store.gotChatID)
	}
	if got := len(keyboard.InlineKeyboard); got != 11 {
		t.Fatalf("row count = %d, want 11", got)
	}
	navigation := keyboard.InlineKeyboard[len(keyboard.InlineKeyboard)-1]
	if len(navigation) != 1 || navigation[0].CallbackData != "session:list:1" {
		t.Fatalf("navigation = %#v", navigation)
	}
}

func TestSessionListViewFetchesThroughRequestedPage(t *testing.T) {
	store := &sessionListStore{sessions: testSessions(21)}
	bot := testBotContext{
		store:  store,
		params: &config.Params{MaxSessionsDisplayed: 10},
	}

	_, keyboard, err := sessionListView(bot, 42, 1)
	if err != nil {
		t.Fatalf("sessionListView() error = %v", err)
	}

	if store.gotLimit != 21 {
		t.Fatalf("ListSessions limit = %d, want 21", store.gotLimit)
	}
	if store.gotChatID != 42 {
		t.Fatalf("ListSessions chat ID = %d, want 42", store.gotChatID)
	}
	navigation := keyboard.InlineKeyboard[len(keyboard.InlineKeyboard)-1]
	if len(navigation) != 2 {
		t.Fatalf("navigation button count = %d, want 2: %#v", len(navigation), navigation)
	}
	if navigation[0].CallbackData != "session:list:0" || navigation[1].CallbackData != "session:list:2" {
		t.Fatalf("navigation = %#v", navigation)
	}
}

func testSessions(count int) []storage.SessionMeta {
	sessions := make([]storage.SessionMeta, 0, count)
	for id := 1; id <= count; id++ {
		sessions = append(sessions, storage.SessionMeta{
			ID:        int64(id),
			ChatID:    42,
			Title:     "Session",
			UpdatedAt: "2026-07-11 00:00:00",
		})
	}
	return sessions
}

type testBotContext struct {
	store  storage.Store
	params *config.Params
}

func (b testBotContext) Store() storage.Store { return b.store }

func (b testBotContext) Config() *config.Params { return b.params }

func (b testBotContext) Providers() *providers.Registry { return nil }

func (b testBotContext) Telegram() *telegram.Bot { return nil }

func (b testBotContext) Logger() *slog.Logger { return slog.Default() }

func (b testBotContext) Reply(context.Context, *models.Message, string) (*models.Message, error) {
	return nil, nil
}

func (b testBotContext) SendMessage(context.Context, int64, string) (*models.Message, error) {
	return nil, nil
}

func (b testBotContext) SendMessageWithKeyboard(context.Context, int64, string, *models.InlineKeyboardMarkup) (*models.Message, error) {
	return nil, nil
}

func (b testBotContext) SendReplyToMessage(context.Context, *models.Message, string) (*models.Message, error) {
	return nil, nil
}

func (b testBotContext) SendReplyWithKeyboard(context.Context, *models.Message, string, *models.InlineKeyboardMarkup) (*models.Message, error) {
	return nil, nil
}

func (b testBotContext) ClearConversation(int64) error { return nil }

func (b testBotContext) CurrentModel(int64) providers.ModelID { return providers.ModelID{} }

func (b testBotContext) MessageLogAttrs(*models.Message) []any { return nil }

func (b testBotContext) DeleteSessionCache(int64) {}

func (b testBotContext) AnswerCallback(context.Context, string, string, bool) {}

type sessionListStore struct {
	storage.Store
	sessions  []storage.SessionMeta
	gotChatID int64
	gotLimit  int
}

func (s *sessionListStore) ListSessions(chatID int64, limit int) ([]storage.SessionMeta, error) {
	s.gotChatID = chatID
	s.gotLimit = limit
	if limit > len(s.sessions) {
		limit = len(s.sessions)
	}
	return s.sessions[:limit], nil
}

func (s *sessionListStore) SaveSession(int64, int64, []conversation.Message) error { return nil }

func (s *sessionListStore) LoadSession(int64, int64) ([]conversation.Message, error) { return nil, nil }

func (s *sessionListStore) GetActiveSession(int64) (storage.SessionMeta, error) {
	return storage.SessionMeta{}, nil
}

func (s *sessionListStore) SetActiveSession(int64, int64) error { return nil }

func (s *sessionListStore) CreateNewSession(int64, string) (storage.SessionMeta, error) {
	return storage.SessionMeta{}, nil
}

func (s *sessionListStore) UpdateSessionTitle(int64, string, bool) error { return nil }

func (s *sessionListStore) DeleteSession(int64, int64) error { return nil }

func (s *sessionListStore) ClearSessions(int64) error { return nil }

func (s *sessionListStore) SaveUserContext(int64, string) error { return nil }

func (s *sessionListStore) LoadUserContext(int64) (string, error) { return "", nil }

func (s *sessionListStore) GetAllChats() ([]int64, error) { return nil, nil }

func (s *sessionListStore) ExportMemory(string) error { return nil }

func (s *sessionListStore) SaveTokenUsage(int64, int64, providers.TokenUsage) error { return nil }

func (s *sessionListStore) GetTokenUsage(int64, int64) (storage.TokenUsageSummary, error) {
	return storage.TokenUsageSummary{}, nil
}

func (s *sessionListStore) SaveChatModel(int64, string, string) error { return nil }

func (s *sessionListStore) LoadChatModel(int64) (providers.ModelID, bool) {
	return providers.ModelID{}, false
}

func (s *sessionListStore) Close() error { return nil }
