package storage

import (
	"fmt"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

// Store defines the interface for all database backends.
type Store interface {
	SaveConversation(chatID int64, messages []conversation.Message) error
	LoadConversation(chatID int64) ([]conversation.Message, error)
	SaveUserContext(chatID int64, context string) error
	LoadUserContext(chatID int64) (string, error)
	ClearConversation(chatID int64) error
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

// Open initializes the appropriate database backend based on the provided configuration.
func Open(cfg config.DatabaseConfig) (Store, error) {
	switch cfg.Backend {
	case "sqlite":
		return newSQLiteStore(cfg.SQLite)
	case "mysql":
		return newMySQLStore(cfg.MySQL)
	default:
		return nil, fmt.Errorf("unsupported database backend: %s", cfg.Backend)
	}
}
