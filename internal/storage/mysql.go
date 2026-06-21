package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/go-sql-driver/mysql"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

const mysqlSchema = `
	CREATE TABLE IF NOT EXISTS conversations (
		id INTEGER PRIMARY KEY AUTO_INCREMENT,
		chat_id INTEGER UNIQUE,
		messages TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS user_context (
		id INTEGER PRIMARY KEY AUTO_INCREMENT,
		chat_id INTEGER UNIQUE,
		context_data TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS token_usage (
		id INTEGER PRIMARY KEY AUTO_INCREMENT,
		chat_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		prompt_tokens INTEGER NOT NULL,
		completion_tokens INTEGER NOT NULL,
		total_tokens INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX idx_token_usage_chat_user
	ON token_usage(chat_id, user_id);

	CREATE TABLE IF NOT EXISTS chat_models (
		chat_id INTEGER PRIMARY KEY,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

type mysqlStore struct {
	conn *sql.DB
}

func newMySQLStore(cfg config.MySQLConfig) (Store, error) {
	mysqlConfig := mysql.NewConfig()
	mysqlConfig.User = cfg.User
	mysqlConfig.Passwd = cfg.Password
	mysqlConfig.Net = "tcp"
	mysqlConfig.Addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	mysqlConfig.DBName = cfg.Database
	mysqlConfig.ParseTime = true

	dsn := mysqlConfig.FormatDSN()
	mysqlDatabase, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db := &mysqlStore{conn: mysqlDatabase}

	// We split the schema by ';' because go-sql-driver/mysql does not support multi-statements easily without multiStatements=true DSN parameter.
	// But it's safer to execute them one by one.
	// Wait, let's just enable multiStatements=true in the DSN to keep it simple.
	mysqlConfig.MultiStatements = true
	dsn = mysqlConfig.FormatDSN()
	mysqlDatabase, err = sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	db.conn = mysqlDatabase

	_, err = mysqlDatabase.Exec(mysqlSchema)
	if err != nil {
		_ = mysqlDatabase.Close()
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	slog.Default().Info("mysql database initialized", "host", cfg.Host, "db", cfg.Database)
	return db, nil
}

// SaveConversation saves the message history for a chat
func (db *mysqlStore) SaveConversation(chatID int64, messages []conversation.Message) error {
	jsonData, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("failed to marshal messages: %w", err)
	}

	query := `
	INSERT INTO conversations (chat_id, messages, updated_at)
	VALUES (?, ?, CURRENT_TIMESTAMP)
	ON DUPLICATE KEY UPDATE
		messages = VALUES(messages),
		updated_at = CURRENT_TIMESTAMP
	`

	_, err = db.conn.Exec(query, chatID, string(jsonData))
	if err != nil {
		return fmt.Errorf("failed to save conversation: %w", err)
	}

	return nil
}

// LoadConversation loads the message history for a chat
func (db *mysqlStore) LoadConversation(chatID int64) ([]conversation.Message, error) {
	var jsonData string
	query := "SELECT messages FROM conversations WHERE chat_id = ?"

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
func (db *mysqlStore) SaveUserContext(chatID int64, context string) error {
	query := `
	INSERT INTO user_context (chat_id, context_data, updated_at)
	VALUES (?, ?, CURRENT_TIMESTAMP)
	ON DUPLICATE KEY UPDATE
		context_data = VALUES(context_data),
		updated_at = CURRENT_TIMESTAMP
	`

	_, err := db.conn.Exec(query, chatID, context)
	if err != nil {
		return fmt.Errorf("failed to save user context: %w", err)
	}

	return nil
}

// LoadUserContext loads personalized context for a user
func (db *mysqlStore) LoadUserContext(chatID int64) (string, error) {
	var context string
	query := "SELECT context_data FROM user_context WHERE chat_id = ?"

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
func (db *mysqlStore) ClearConversation(chatID int64) error {
	query := "DELETE FROM conversations WHERE chat_id = ?"
	_, err := db.conn.Exec(query, chatID)
	if err != nil {
		return fmt.Errorf("failed to clear conversation: %w", err)
	}
	return nil
}

// GetAllChats returns all chat IDs in the database
func (db *mysqlStore) GetAllChats() ([]int64, error) {
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
func (db *mysqlStore) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}

// ExportMemory exports all conversations to a JSON file (for backup)
func (db *mysqlStore) ExportMemory(filename string) error {
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
func (db *mysqlStore) SaveTokenUsage(chatID, userID int64, usage providers.TokenUsage) error {
	_, err := db.conn.Exec(`
		INSERT INTO token_usage (
			chat_id, user_id, prompt_tokens, completion_tokens, total_tokens
		) VALUES (?, ?, ?, ?, ?)
	`, chatID, userID, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	if err != nil {
		return fmt.Errorf("failed to save token usage: %w", err)
	}
	return nil
}

// GetTokenUsage returns totals for one user in one chat.
func (db *mysqlStore) GetTokenUsage(chatID, userID int64) (TokenUsageSummary, error) {
	var summary TokenUsageSummary
	err := db.conn.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(prompt_tokens), 0),
			COALESCE(SUM(completion_tokens), 0),
			COALESCE(SUM(total_tokens), 0)
		FROM token_usage
		WHERE chat_id = ? AND user_id = ?
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
func (db *mysqlStore) SaveChatModel(chatID int64, provider, model string) error {
	_, err := db.conn.Exec(`
		INSERT INTO chat_models (chat_id, provider, model, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON DUPLICATE KEY UPDATE
			provider = VALUES(provider),
			model = VALUES(model),
			updated_at = CURRENT_TIMESTAMP
	`, chatID, provider, model)
	if err != nil {
		return fmt.Errorf("failed to save chat model: %w", err)
	}
	return nil
}

// LoadChatModel loads the selected model for a chat.
func (db *mysqlStore) LoadChatModel(chatID int64) (providers.ModelID, bool) {
	var provider, model string
	err := db.conn.QueryRow(
		"SELECT provider, model FROM chat_models WHERE chat_id = ?", chatID,
	).Scan(&provider, &model)
	if err != nil {
		return providers.ModelID{}, false
	}
	return providers.ModelID{Provider: provider, Model: model}, true
}
