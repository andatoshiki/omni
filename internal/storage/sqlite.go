package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	_ "modernc.org/sqlite"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

const sqliteSchema = `
	CREATE TABLE IF NOT EXISTS sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_id INTEGER NOT NULL,
		title TEXT NOT NULL,
		title_generated BOOLEAN NOT NULL DEFAULT 0,
		messages TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_chat_id ON sessions(chat_id);

	CREATE TABLE IF NOT EXISTS active_sessions (
		chat_id INTEGER PRIMARY KEY,
		session_id INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS user_context (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_id INTEGER UNIQUE,
		context_data TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS token_usage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		prompt_tokens INTEGER NOT NULL,
		completion_tokens INTEGER NOT NULL,
		total_tokens INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_token_usage_chat_user
	ON token_usage(chat_id, user_id);

	CREATE TABLE IF NOT EXISTS chat_models (
		chat_id INTEGER PRIMARY KEY,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
`

type sqliteStore struct {
	conn *sql.DB
}

func newSQLiteStore(cfg config.SQLiteConfig) (Store, error) {
	sqliteDatabase, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db := &sqliteStore{conn: sqliteDatabase}

	if err := migrateSQLiteSchema(sqliteDatabase); err != nil {
		_ = sqliteDatabase.Close()
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	slog.Default().Info("sqlite database initialized", "file", cfg.Path)
	return db, nil
}

func migrateSQLiteSchema(db *sql.DB) error {
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='sessions'").Scan(&tableName)
	if err == sql.ErrNoRows {
		slog.Default().Info("running database migration to v2 (sessions)")
		
		_, _ = db.Exec("ALTER TABLE conversations RENAME TO conversations_legacy")

		if _, err := db.Exec(sqliteSchema); err != nil {
			return err
		}

		_, err = db.Exec(`
			INSERT INTO sessions (chat_id, title, title_generated, messages, created_at, updated_at)
			SELECT chat_id, 'Initial Session', 1, messages, created_at, updated_at
			FROM conversations_legacy
		`)
		if err != nil {
			slog.Default().Warn("failed to migrate data from legacy conversations", "error", err)
		}

		_, err = db.Exec(`
			INSERT INTO active_sessions (chat_id, session_id)
			SELECT chat_id, id FROM sessions
		`)
		if err != nil {
			slog.Default().Warn("failed to set active sessions from legacy", "error", err)
		}
	} else if err != nil {
		return err
	} else {
		if _, err := db.Exec(sqliteSchema); err != nil {
			return err
		}
	}
	return nil
}

func (db *sqliteStore) SaveSession(chatID int64, sessionID int64, messages []conversation.Message) error {
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

func (db *sqliteStore) LoadSession(sessionID int64) ([]conversation.Message, error) {
	var jsonData string
	query := "SELECT messages FROM sessions WHERE id = ?"

	err := db.conn.QueryRow(query, sessionID).Scan(&jsonData)
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

func (db *sqliteStore) GetActiveSession(chatID int64) (SessionMeta, error) {
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

func (db *sqliteStore) SetActiveSession(chatID int64, sessionID int64) error {
	query := `
		INSERT INTO active_sessions (chat_id, session_id)
		VALUES (?, ?)
		ON CONFLICT(chat_id) DO UPDATE SET
			session_id = excluded.session_id
	`
	_, err := db.conn.Exec(query, chatID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to set active session: %w", err)
	}
	return nil
}

func (db *sqliteStore) CreateNewSession(chatID int64, title string) (SessionMeta, error) {
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

func (db *sqliteStore) ListSessions(chatID int64, limit int) ([]SessionMeta, error) {
	query := `
		SELECT id, chat_id, title, title_generated, updated_at
		FROM sessions
		WHERE chat_id = ?
		ORDER BY updated_at DESC
		LIMIT ?
	`
	rows, err := db.conn.Query(query, chatID, limit)
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
	return sessions, nil
}

func (db *sqliteStore) UpdateSessionTitle(sessionID int64, title string, generated bool) error {
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

func (db *sqliteStore) DeleteSession(sessionID int64) error {
	_, err := db.conn.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	_, err = db.conn.Exec("DELETE FROM active_sessions WHERE session_id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("failed to clear active session: %w", err)
	}
	return nil
}

func (db *sqliteStore) ClearSessions(chatID int64) error {
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

func (db *sqliteStore) SaveUserContext(chatID int64, context string) error {
	query := `
	INSERT INTO user_context (chat_id, context_data, updated_at)
	VALUES (?, ?, CURRENT_TIMESTAMP)
	ON CONFLICT(chat_id) DO UPDATE SET
		context_data = excluded.context_data,
		updated_at = CURRENT_TIMESTAMP
	`
	_, err := db.conn.Exec(query, chatID, context)
	if err != nil {
		return fmt.Errorf("failed to save user context: %w", err)
	}
	return nil
}

func (db *sqliteStore) LoadUserContext(chatID int64) (string, error) {
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

func (db *sqliteStore) GetAllChats() ([]int64, error) {
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
	return chatIDs, nil
}

func (db *sqliteStore) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}

func (db *sqliteStore) ExportMemory(filename string) error {
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
		
		sessionsMeta, err := db.ListSessions(chatID, 1000)
		if err != nil {
			continue
		}

		var sessions []SessionExport
		for _, sm := range sessionsMeta {
			msgs, _ := db.LoadSession(sm.ID)
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

func (db *sqliteStore) SaveTokenUsage(chatID, userID int64, usage providers.TokenUsage) error {
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

func (db *sqliteStore) GetTokenUsage(chatID, userID int64) (TokenUsageSummary, error) {
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

func (db *sqliteStore) SaveChatModel(chatID int64, provider, model string) error {
	_, err := db.conn.Exec(`
		INSERT INTO chat_models (chat_id, provider, model, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(chat_id) DO UPDATE SET
			provider = excluded.provider,
			model = excluded.model,
			updated_at = CURRENT_TIMESTAMP
	`, chatID, provider, model)
	if err != nil {
		return fmt.Errorf("failed to save chat model: %w", err)
	}
	return nil
}

func (db *sqliteStore) LoadChatModel(chatID int64) (providers.ModelID, bool) {
	var provider, model string
	err := db.conn.QueryRow(
		"SELECT provider, model FROM chat_models WHERE chat_id = ?", chatID,
	).Scan(&provider, &model)
	if err != nil {
		return providers.ModelID{}, false
	}
	return providers.ModelID{Provider: provider, Model: model}, true
}
