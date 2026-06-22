package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	_ "github.com/lib/pq"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

const postgresSchema = `
	CREATE TABLE IF NOT EXISTS conversations (
		id SERIAL PRIMARY KEY,
		chat_id BIGINT UNIQUE NOT NULL,
		messages TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS user_context (
		id SERIAL PRIMARY KEY,
		chat_id BIGINT UNIQUE NOT NULL,
		context_data TEXT NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS token_usage (
		id SERIAL PRIMARY KEY,
		chat_id BIGINT NOT NULL,
		user_id BIGINT NOT NULL,
		prompt_tokens INTEGER NOT NULL,
		completion_tokens INTEGER NOT NULL,
		total_tokens INTEGER NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_token_usage_chat_user
	ON token_usage(chat_id, user_id);

	CREATE TABLE IF NOT EXISTS chat_models (
		chat_id BIGINT PRIMARY KEY,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
`

type postgresStore struct {
	conn *sql.DB
}

func newPostgresStore(cfg config.PostgresConfig) (Store, error) {
	sslmode := cfg.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, sslmode)

	postgresDatabase, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db := &postgresStore{conn: postgresDatabase}

	_, err = postgresDatabase.Exec(postgresSchema)
	if err != nil {
		_ = postgresDatabase.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	slog.Default().Info("postgres database initialized", "host", cfg.Host, "db", cfg.Database)
	return db, nil
}

// SaveConversation saves the message history for a chat
func (db *postgresStore) SaveConversation(chatID int64, messages []conversation.Message) error {
	jsonData, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("failed to marshal messages: %w", err)
	}

	query := `
	INSERT INTO conversations (chat_id, messages, updated_at)
	VALUES ($1, $2, CURRENT_TIMESTAMP)
	ON CONFLICT (chat_id) DO UPDATE SET
		messages = EXCLUDED.messages,
		updated_at = CURRENT_TIMESTAMP
	`

	_, err = db.conn.Exec(query, chatID, string(jsonData))
	if err != nil {
		return fmt.Errorf("failed to save conversation: %w", err)
	}

	return nil
}

// LoadConversation loads the message history for a chat
func (db *postgresStore) LoadConversation(chatID int64) ([]conversation.Message, error) {
	var jsonData string
	query := "SELECT messages FROM conversations WHERE chat_id = $1"

	err := db.conn.QueryRow(query, chatID).Scan(&jsonData)
	if err != nil {
		if err == sql.ErrNoRows {
			return []conversation.Message{}, nil
		}
		return nil, fmt.Errorf("failed to load conversation: %w", err)
	}

	var messages []conversation.Message
	err = json.Unmarshal([]byte(jsonData), &messages)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal messages: %w", err)
	}

	return messages, nil
}

// SaveUserContext saves personalized context for a user
func (db *postgresStore) SaveUserContext(chatID int64, context string) error {
	query := `
	INSERT INTO user_context (chat_id, context_data, updated_at)
	VALUES ($1, $2, CURRENT_TIMESTAMP)
	ON CONFLICT (chat_id) DO UPDATE SET
		context_data = EXCLUDED.context_data,
		updated_at = CURRENT_TIMESTAMP
	`

	_, err := db.conn.Exec(query, chatID, context)
	if err != nil {
		return fmt.Errorf("failed to save user context: %w", err)
	}

	return nil
}

// LoadUserContext loads personalized context for a user
func (db *postgresStore) LoadUserContext(chatID int64) (string, error) {
	var context string
	query := "SELECT context_data FROM user_context WHERE chat_id = $1"

	err := db.conn.QueryRow(query, chatID).Scan(&context)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("failed to load user context: %w", err)
	}

	return context, nil
}

// ClearConversation deletes a chat's conversation history
func (db *postgresStore) ClearConversation(chatID int64) error {
	query := "DELETE FROM conversations WHERE chat_id = $1"
	_, err := db.conn.Exec(query, chatID)
	if err != nil {
		return fmt.Errorf("failed to clear conversation: %w", err)
	}
	return nil
}

// GetAllChats returns all chat IDs in the database
func (db *postgresStore) GetAllChats() ([]int64, error) {
	query := "SELECT DISTINCT chat_id FROM conversations"
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get chats: %w", err)
	}
	defer rows.Close()

	var chatIDs []int64
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return nil, fmt.Errorf("failed to scan chat ID: %w", err)
		}
		chatIDs = append(chatIDs, chatID)
	}

	return chatIDs, nil
}

// Close closes the database connection.
func (db *postgresStore) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}

// ExportMemory exports all conversations to a JSON file (for backup)
func (db *postgresStore) ExportMemory(filename string) error {
	type ConversationExport struct {
		ChatID   int64                  `json:"chat_id"`
		Messages []conversation.Message `json:"messages"`
		Context  string                 `json:"context,omitempty"`
	}

	chatIDs, err := db.GetAllChats()
	if err != nil {
		return err
	}

	var exports []ConversationExport

	for _, chatID := range chatIDs {
		messages, err := db.LoadConversation(chatID)
		if err != nil {
			continue
		}

		context, _ := db.LoadUserContext(chatID)

		exports = append(exports, ConversationExport{
			ChatID:   chatID,
			Messages: messages,
			Context:  context,
		})
	}

	jsonData, err := json.MarshalIndent(exports, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal exports: %w", err)
	}

	err = os.WriteFile(filename, jsonData, 0600)
	if err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	slog.Default().Info("memory exported", "file", filename, "chats", len(exports))
	return nil
}

// SaveTokenUsage records token counts for a request.
func (db *postgresStore) SaveTokenUsage(chatID, userID int64, usage providers.TokenUsage) error {
	_, err := db.conn.Exec(`
		INSERT INTO token_usage (
			chat_id, user_id, prompt_tokens, completion_tokens, total_tokens
		) VALUES ($1, $2, $3, $4, $5)
	`, chatID, userID, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	if err != nil {
		return fmt.Errorf("failed to save token usage: %w", err)
	}
	return nil
}

// GetTokenUsage returns totals for one user in one chat.
func (db *postgresStore) GetTokenUsage(chatID, userID int64) (TokenUsageSummary, error) {
	var summary TokenUsageSummary
	err := db.conn.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(prompt_tokens), 0),
			COALESCE(SUM(completion_tokens), 0),
			COALESCE(SUM(total_tokens), 0)
		FROM token_usage
		WHERE chat_id = $1 AND user_id = $2
	`, chatID, userID).Scan(
		&summary.Requests,
		&summary.PromptTokens,
		&summary.CompletionTokens,
		&summary.TotalTokens,
	)
	if err != nil {
		return TokenUsageSummary{}, fmt.Errorf("failed to load token usage: %w", err)
	}
	return summary, nil
}

// SaveChatModel persists the selected model for a chat.
func (db *postgresStore) SaveChatModel(chatID int64, provider, model string) error {
	_, err := db.conn.Exec(`
		INSERT INTO chat_models (chat_id, provider, model, updated_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (chat_id) DO UPDATE SET
			provider = EXCLUDED.provider,
			model = EXCLUDED.model,
			updated_at = CURRENT_TIMESTAMP
	`, chatID, provider, model)
	if err != nil {
		return fmt.Errorf("failed to save chat model: %w", err)
	}
	return nil
}

// LoadChatModel loads the selected model for a chat.
func (db *postgresStore) LoadChatModel(chatID int64) (providers.ModelID, bool) {
	var provider, model string
	err := db.conn.QueryRow(
		"SELECT provider, model FROM chat_models WHERE chat_id = $1", chatID,
	).Scan(&provider, &model)
	if err != nil {
		return providers.ModelID{}, false
	}
	return providers.ModelID{Provider: provider, Model: model}, true
}
