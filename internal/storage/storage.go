package storage

import (
	"fmt"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

// Store defines the interface for all database backends.
type Store interface {
	SaveSession(chatID int64, sessionID int64, messages []conversation.Message) error
	LoadSession(chatID int64, sessionID int64) ([]conversation.Message, error)
	SaveTranscriptMessage(message TranscriptMessage) error
	RecentTranscriptMessages(chatID int64, threadID, beforeMessageID, limit int) ([]TranscriptMessage, error)
	GetActiveSession(chatID int64) (SessionMeta, error)
	SetActiveSession(chatID int64, sessionID int64) error
	CreateNewSession(chatID int64, title string) (SessionMeta, error)
	// ListSessions returns every session when limit is zero or negative.
	ListSessions(chatID int64, limit int) ([]SessionMeta, error)
	UpdateSessionTitle(sessionID int64, title string, generated bool) error
	DeleteSession(chatID int64, sessionID int64) error
	ClearSessions(chatID int64) error

	SaveUserContext(chatID int64, context string) error
	LoadUserContext(chatID int64) (string, error)
	GetAllChats() ([]int64, error)
	ExportMemory(filename string) error
	SaveTokenUsage(chatID, userID int64, usage providers.TokenUsage) error
	GetTokenUsage(chatID, userID int64) (TokenUsageSummary, error)
	SaveChatModel(chatID int64, provider, model string) error
	LoadChatModel(chatID int64) (providers.ModelID, bool)
	Close() error
}

type TokenUsageSummary struct {
	Requests         int64
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

type SessionMeta struct {
	ID             int64
	ChatID         int64
	Title          string
	TitleGenerated bool
	UpdatedAt      string
}

// TranscriptMessage is one text-only Telegram message available to /summary.
// It is deliberately independent from the bounded AI conversation context.
type TranscriptMessage struct {
	ChatID    int64  `json:"chat_id"`
	ThreadID  int    `json:"thread_id"`
	MessageID int    `json:"message_id"`
	Role      string `json:"role"`
	Sender    string `json:"sender,omitempty"`
	Text      string `json:"text"`
}

// Open initializes the appropriate database backend based on the provided configuration.
func Open(cfg config.DatabaseConfig) (Store, error) {
	switch cfg.Backend {
	case "sqlite":
		return newSQLiteStore(cfg.SQLite)
	case "mysql":
		return newMySQLStore(cfg.MySQL)
	case "postgres":
		return newPostgresStore(cfg.Postgres)
	case "mongodb":
		return newMongoDBStore(cfg.MongoDB)
	default:
		return nil, fmt.Errorf("unsupported database backend: %s", cfg.Backend)
	}
}
