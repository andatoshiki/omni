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
	CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTO_INCREMENT,
		chat_id INTEGER NOT NULL,
		title TEXT NOT NULL,
		title_generated BOOLEAN NOT NULL DEFAULT FALSE,
		messages LONGTEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS active_sessions (
		chat_id INTEGER PRIMARY KEY,
		session_id INTEGER NOT NULL
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
	mysqlConfig.DBName = cfg.DBName
	mysqlConfig.ParseTime = true

	dsn := mysqlConfig.FormatDSN()
	mysqlDatabase, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db := &mysqlStore{conn: mysqlDatabase}

	mysqlConfig.MultiStatements = true
	dsn = mysqlConfig.FormatDSN()
	mysqlDatabase, err = sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	db.conn = mysqlDatabase

	if err := migrateMySQLSchema(mysqlDatabase); err != nil {
		_ = mysqlDatabase.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	slog.Default().Info("mysql database initialized", "host", cfg.Host, "db", cfg.DBName)
	return db, nil
}

func migrateMySQLSchema(db *sql.DB) error {
	if _, err := db.Exec(mysqlSchema); err != nil {
		return err
	}
	if err := ensureMySQLIndexes(db); err != nil {
		return err
	}
	hasLegacy, err := mysqlTableExists(db, "conversations")
	if err != nil || !hasLegacy {
		return err
	}

	slog.Default().Info("running database migration to v2 (sessions)")
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := migrateMySQLLegacyConversations(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := db.Exec("RENAME TABLE conversations TO conversations_legacy"); err != nil {
		slog.Default().Warn("failed to archive legacy conversations", "error", err)
	}
	return nil
}

func mysqlTableExists(db *sql.DB, tableName string) (bool, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
			AND table_name = ?
	`, tableName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func migrateMySQLLegacyConversations(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		INSERT INTO sessions (chat_id, title, title_generated, messages, created_at, updated_at)
		SELECT c.chat_id, 'Initial Session', 1, c.messages, c.created_at, c.updated_at
		FROM conversations c
		WHERE NOT EXISTS (
			SELECT 1 FROM sessions s WHERE s.chat_id = c.chat_id
		)
	`); err != nil {
		return fmt.Errorf("failed to migrate legacy conversations: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO active_sessions (chat_id, session_id)
		SELECT c.chat_id, MIN(s.id)
		FROM conversations c
		JOIN sessions s ON s.chat_id = c.chat_id
			AND s.title = 'Initial Session'
			AND s.title_generated = TRUE
			AND s.messages = c.messages
		GROUP BY c.chat_id
		ON DUPLICATE KEY UPDATE
			session_id = VALUES(session_id)
	`); err != nil {
		return fmt.Errorf("failed to set active sessions from legacy: %w", err)
	}
	return nil
}

func ensureMySQLIndexes(db *sql.DB) error {
	if err := ensureMySQLIndex(db, "sessions", "idx_sessions_chat_id", "CREATE INDEX idx_sessions_chat_id ON sessions(chat_id)"); err != nil {
		return err
	}
	return ensureMySQLIndex(db, "token_usage", "idx_token_usage_chat_user", "CREATE INDEX idx_token_usage_chat_user ON token_usage(chat_id, user_id)")
}

func ensureMySQLIndex(db *sql.DB, tableName, indexName, createStatement string) error {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM information_schema.statistics
		WHERE table_schema = DATABASE()
			AND table_name = ?
			AND index_name = ?
	`, tableName, indexName).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to inspect mysql index %s: %w", indexName, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := db.Exec(createStatement); err != nil {
		return fmt.Errorf("failed to create mysql index %s: %w", indexName, err)
	}
	return nil
}

func (db *mysqlStore) SaveSession(chatID int64, sessionID int64, messages []conversation.Message) error {
	jsonData, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("failed to marshal messages: %w", err)
	}

	query := `
	UPDATE sessions SET
		messages = ?,
		updated_at = CURRENT_TIMESTAMP
	WHERE id = ? AND chat_id = ?
	`
	_, err = db.conn.Exec(query, string(jsonData), sessionID, chatID)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

func (db *mysqlStore) LoadSession(chatID int64, sessionID int64) ([]conversation.Message, error) {
	var jsonData string
	query := "SELECT messages FROM sessions WHERE id = ? AND chat_id = ?"

	err := db.conn.QueryRow(query, sessionID, chatID).Scan(&jsonData)
	if err != nil {
		if err == sql.ErrNoRows {
			return []conversation.Message{}, nil
		}
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	var messages []conversation.Message
	err = json.Unmarshal([]byte(jsonData), &messages)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal messages: %w", err)
	}

	return messages, nil
}

func (db *mysqlStore) GetActiveSession(chatID int64) (SessionMeta, error) {
	query := `
		SELECT s.id, s.chat_id, s.title, s.title_generated, s.updated_at
		FROM active_sessions a
		JOIN sessions s ON a.session_id = s.id
		WHERE a.chat_id = ?
	`
	var meta SessionMeta
	err := db.conn.QueryRow(query, chatID).Scan(&meta.ID, &meta.ChatID, &meta.Title, &meta.TitleGenerated, &meta.UpdatedAt)
	if err != nil {
		return SessionMeta{}, err
	}
	return meta, nil
}

func (db *mysqlStore) SetActiveSession(chatID int64, sessionID int64) error {
	if err := db.ensureSessionBelongsToChat(chatID, sessionID); err != nil {
		return err
	}
	query := `
		INSERT INTO active_sessions (chat_id, session_id)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE
			session_id = VALUES(session_id)
	`
	_, err := db.conn.Exec(query, chatID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to set active session: %w", err)
	}
	return nil
}

func (db *mysqlStore) ensureSessionBelongsToChat(chatID int64, sessionID int64) error {
	var id int64
	err := db.conn.QueryRow("SELECT id FROM sessions WHERE id = ? AND chat_id = ?", sessionID, chatID).Scan(&id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("session %d not found for chat %d", sessionID, chatID)
	}
	if err != nil {
		return fmt.Errorf("failed to verify session ownership: %w", err)
	}
	return nil
}

func (db *mysqlStore) CreateNewSession(chatID int64, title string) (SessionMeta, error) {
	query := `
		INSERT INTO sessions (chat_id, title, title_generated, messages, created_at, updated_at)
		VALUES (?, ?, 0, '[]', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`
	res, err := db.conn.Exec(query, chatID, title)
	if err != nil {
		return SessionMeta{}, fmt.Errorf("failed to create new session: %w", err)
	}

	sessionID, err := res.LastInsertId()
	if err != nil {
		return SessionMeta{}, fmt.Errorf("failed to get last insert id: %w", err)
	}

	err = db.SetActiveSession(chatID, sessionID)
	if err != nil {
		return SessionMeta{}, err
	}

	return db.GetActiveSession(chatID)
}

func (db *mysqlStore) ListSessions(chatID int64, limit int) ([]SessionMeta, error) {
	query := `
		SELECT id, chat_id, title, title_generated, updated_at
		FROM sessions
		WHERE chat_id = ?
		ORDER BY updated_at DESC
	`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.conn.Query(query+" LIMIT ?", chatID, limit)
	} else {
		rows, err = db.conn.Query(query, chatID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []SessionMeta
	for rows.Next() {
		var meta SessionMeta
		if err := rows.Scan(&meta.ID, &meta.ChatID, &meta.Title, &meta.TitleGenerated, &meta.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan session meta: %w", err)
		}
		sessions = append(sessions, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate sessions: %w", err)
	}
	return sessions, nil
}

func (db *mysqlStore) UpdateSessionTitle(sessionID int64, title string, generated bool) error {
	genInt := 0
	if generated {
		genInt = 1
	}
	query := `
		UPDATE sessions SET
			title = ?,
			title_generated = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`
	_, err := db.conn.Exec(query, title, genInt, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session title: %w", err)
	}
	return nil
}

func (db *mysqlStore) DeleteSession(chatID int64, sessionID int64) error {
	result, err := db.conn.Exec("DELETE FROM sessions WHERE id = ? AND chat_id = ?", sessionID, chatID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect deleted session count: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("session %d not found for chat %d", sessionID, chatID)
	}
	_, err = db.conn.Exec("DELETE FROM active_sessions WHERE chat_id = ? AND session_id = ?", chatID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to clear active session: %w", err)
	}
	return nil
}

func (db *mysqlStore) ClearSessions(chatID int64) error {
	_, err := db.conn.Exec("DELETE FROM sessions WHERE chat_id = ?", chatID)
	if err != nil {
		return fmt.Errorf("failed to delete sessions: %w", err)
	}
	_, err = db.conn.Exec("DELETE FROM active_sessions WHERE chat_id = ?", chatID)
	if err != nil {
		return fmt.Errorf("failed to clear active session: %w", err)
	}
	return nil
}

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

// GetAllChats returns all chat IDs in the database
func (db *mysqlStore) GetAllChats() ([]int64, error) {
	query := "SELECT DISTINCT chat_id FROM sessions"
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate chats: %w", err)
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
	type SessionExport struct {
		ID        int64                  `json:"id"`
		Title     string                 `json:"title"`
		Messages  []conversation.Message `json:"messages"`
		UpdatedAt string                 `json:"updated_at"`
	}

	type ConversationExport struct {
		ChatID   int64           `json:"chat_id"`
		Context  string          `json:"context,omitempty"`
		Sessions []SessionExport `json:"sessions"`
	}

	chatIDs, err := db.GetAllChats()
	if err != nil {
		return err
	}

	var exports []ConversationExport

	for _, chatID := range chatIDs {
		context, _ := db.LoadUserContext(chatID)

		sessionsMeta, err := db.ListSessions(chatID, 0)
		if err != nil {
			continue
		}

		var sessions []SessionExport
		for _, sm := range sessionsMeta {
			msgs, _ := db.LoadSession(chatID, sm.ID)
			sessions = append(sessions, SessionExport{
				ID:        sm.ID,
				Title:     sm.Title,
				Messages:  msgs,
				UpdatedAt: sm.UpdatedAt,
			})
		}

		exports = append(exports, ConversationExport{
			ChatID:   chatID,
			Context:  context,
			Sessions: sessions,
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
