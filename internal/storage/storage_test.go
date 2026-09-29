package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

func TestTokenUsageIsAggregatedPerUserAndChat(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(sqliteSchema); err != nil {
		t.Fatal(err)
	}
	database := &sqliteStore{conn: connection}

	for _, usage := range []providers.TokenUsage{
		{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		{PromptTokens: 20, CompletionTokens: 8, TotalTokens: 28},
	} {
		if err := database.SaveTokenUsage(-100, 42, usage); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.SaveTokenUsage(-100, 99, providers.TokenUsage{TotalTokens: 500}); err != nil {
		t.Fatal(err)
	}

	summary, err := database.GetTokenUsage(-100, 42)
	if err != nil {
		t.Fatal(err)
	}
	want := (TokenUsageSummary{Requests: 2, PromptTokens: 30, CompletionTokens: 13, TotalTokens: 43})
	if summary != want {
		t.Fatalf("summary = %#v, want %#v", summary, want)
	}
}

func TestSQLiteTelegramUserAccessPersistsAndMovesUsernames(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := migrateSQLiteSchema(connection); err != nil {
		t.Fatal(err)
	}
	database := &sqliteStore{conn: connection}

	if err := database.ObserveTelegramUser(10, "Alice", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := database.SetTelegramUserAllowed(10, true); err != nil {
		t.Fatal(err)
	}
	if err := database.ObserveTelegramUser(20, "Alice", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := database.SetTelegramUserAllowed(20, true); err != nil {
		t.Fatal(err)
	}
	if err := database.SetTelegramUserAllowed(20, true); err != nil {
		t.Fatal(err)
	}

	users, err := database.LoadTelegramUsers()
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[int64]TelegramUser, len(users))
	for _, user := range users {
		byID[user.UserID] = user
	}
	if !byID[10].Allowed || byID[10].NormalizedUsername != "" {
		t.Fatalf("original username owner = %#v", byID[10])
	}
	if !byID[20].Allowed || byID[20].NormalizedUsername != "alice" {
		t.Fatalf("new username owner = %#v", byID[20])
	}
	if err := database.SetTelegramUserAllowed(20, false); err != nil {
		t.Fatal(err)
	}
	users, err = database.LoadTelegramUsers()
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		if user.UserID == 20 && user.Allowed {
			t.Fatal("deleted access remained allowed")
		}
	}
}

func TestSQLSchemasIncludeTelegramUserAccess(t *testing.T) {
	for name, schema := range map[string]string{
		"sqlite":   sqliteSchema,
		"mysql":    mysqlSchema,
		"postgres": postgresSchema,
	} {
		t.Run(name, func(t *testing.T) {
			for _, required := range []string{"CREATE TABLE IF NOT EXISTS telegram_users", "normalized_username", "allowed BOOLEAN"} {
				if !strings.Contains(schema, required) {
					t.Fatalf("schema does not contain %q", required)
				}
			}
		})
	}
}

func TestSQLiteTelegramUserAccessSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.db")
	firstStore, err := newSQLiteStore(config.SQLiteConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstStore.ObserveTelegramUser(42, "Alice", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := firstStore.SetTelegramUserAllowed(42, true); err != nil {
		t.Fatal(err)
	}
	if err := firstStore.Close(); err != nil {
		t.Fatal(err)
	}

	secondStore, err := newSQLiteStore(config.SQLiteConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	users, err := secondStore.LoadTelegramUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].UserID != 42 || users[0].NormalizedUsername != "alice" || !users[0].Allowed {
		t.Fatalf("reopened users = %#v", users)
	}
}

func TestConversationStringContentRoundTrips(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(sqliteSchema); err != nil {
		t.Fatal(err)
	}
	database := &sqliteStore{conn: connection}
	want := []conversation.Message{
		{Role: providers.RoleUser, Content: "[User attached an image] describe this"},
		{Role: providers.RoleAssistant, Content: "A test image."},
	}

	activeSession, err := database.CreateNewSession(42, "Test Session")
	if err != nil {
		t.Fatal(err)
	}

	if err := database.SaveSession(42, activeSession.ID, want); err != nil {
		t.Fatal(err)
	}

	got, err := database.LoadSession(42, activeSession.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
	for index := range want {
		content, ok := got[index].Content.(string)
		if !ok || content != want[index].Content {
			t.Fatalf("message %d content = %#v (%T), want %q", index, got[index].Content, got[index].Content, want[index].Content)
		}
	}
}

func TestSummaryTranscriptKeepsRecentMessagesAcrossClear(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(sqliteSchema); err != nil {
		t.Fatal(err)
	}
	database := &sqliteStore{conn: connection}

	for messageID := 1; messageID <= 105; messageID++ {
		if err := database.SaveTranscriptMessage(TranscriptMessage{
			ChatID: 42, ThreadID: 7, MessageID: messageID,
			Role: providers.RoleUser, Sender: "Alice", Text: fmt.Sprintf("message %d", messageID),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.SaveTranscriptMessage(TranscriptMessage{
		ChatID: 42, ThreadID: 8, MessageID: 200,
		Role: providers.RoleUser, Text: "other topic",
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveTranscriptMessage(TranscriptMessage{
		ChatID: 42, ThreadID: 7, MessageID: 105,
		Role: providers.RoleAssistant, Text: "updated final reply",
	}); err != nil {
		t.Fatal(err)
	}

	if err := database.ClearSessions(42); err != nil {
		t.Fatal(err)
	}
	messages, err := database.RecentTranscriptMessages(42, 7, 106, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 5 {
		t.Fatalf("message count = %d, want 5", len(messages))
	}
	for index, message := range messages {
		wantID := 101 + index
		if message.MessageID != wantID {
			t.Fatalf("message %d ID = %d, want %d", index, message.MessageID, wantID)
		}
	}
	if messages[4].Text != "updated final reply" || messages[4].Role != providers.RoleAssistant {
		t.Fatalf("upserted message = %#v", messages[4])
	}

	retained, err := database.RecentTranscriptMessages(42, 7, 1000, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(retained) != 100 || retained[0].MessageID != 6 || retained[99].MessageID != 105 {
		t.Fatalf("retained transcript range = %d..%d (%d messages), want 6..105 (100)", retained[0].MessageID, retained[len(retained)-1].MessageID, len(retained))
	}
}

func TestExportMemoryIncludesMoreThanThousandSessions(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(sqliteSchema); err != nil {
		t.Fatal(err)
	}
	database := &sqliteStore{conn: connection}

	for id := 0; id < 1001; id++ {
		session, err := database.CreateNewSession(42, "Session")
		if err != nil {
			t.Fatal(err)
		}
		if err := database.SaveSession(42, session.ID, []conversation.Message{{Role: providers.RoleUser, Content: "hello"}}); err != nil {
			t.Fatal(err)
		}
	}

	filename := t.TempDir() + "/memory-export.json"
	if err := database.ExportMemory(filename); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}

	var exports []struct {
		ChatID   int64 `json:"chat_id"`
		Sessions []struct {
			ID int64 `json:"id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(data, &exports); err != nil {
		t.Fatal(err)
	}
	if len(exports) != 1 {
		t.Fatalf("export count = %d, want 1", len(exports))
	}
	if exports[0].ChatID != 42 {
		t.Fatalf("chat id = %d, want 42", exports[0].ChatID)
	}
	if got := len(exports[0].Sessions); got != 1001 {
		t.Fatalf("session count = %d, want 1001", got)
	}
}

func TestSQLiteMigrationPreservesLegacyConversation(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(`
		CREATE TABLE conversations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER UNIQUE,
			messages TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatal(err)
	}

	want := []conversation.Message{{Role: providers.RoleUser, Content: "legacy hello"}}
	jsonData, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec("INSERT INTO conversations (chat_id, messages) VALUES (?, ?)", int64(42), string(jsonData)); err != nil {
		t.Fatal(err)
	}
	if err := migrateSQLiteSchema(connection); err != nil {
		t.Fatal(err)
	}

	database := &sqliteStore{conn: connection}
	activeSession, err := database.GetActiveSession(42)
	if err != nil {
		t.Fatal(err)
	}
	got, err := database.LoadSession(42, activeSession.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "legacy hello" {
		t.Fatalf("migrated messages = %#v", got)
	}
	hasConversations, err := sqliteTableExists(connection, "conversations")
	if err != nil {
		t.Fatal(err)
	}
	if hasConversations {
		t.Fatal("legacy conversations table was not archived")
	}
	hasLegacy, err := sqliteTableExists(connection, "conversations_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if !hasLegacy {
		t.Fatal("conversations_legacy table missing after migration")
	}
}

func TestSQLiteSessionOwnershipIsEnforced(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(sqliteSchema); err != nil {
		t.Fatal(err)
	}
	database := &sqliteStore{conn: connection}

	chatSession, err := database.CreateNewSession(42, "chat session")
	if err != nil {
		t.Fatal(err)
	}
	otherSession, err := database.CreateNewSession(99, "other session")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetActiveSession(42, otherSession.ID); err == nil {
		t.Fatal("SetActiveSession allowed a session from another chat")
	}
	if err := database.DeleteSession(42, otherSession.ID); err == nil {
		t.Fatal("DeleteSession allowed a session from another chat")
	}

	otherSessions, err := database.ListSessions(99, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherSessions) != 1 || otherSessions[0].ID != otherSession.ID {
		t.Fatalf("other chat sessions = %#v", otherSessions)
	}
	if err := database.DeleteSession(42, chatSession.ID); err != nil {
		t.Fatal(err)
	}
	chatSessions, err := database.ListSessions(42, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(chatSessions) != 0 {
		t.Fatalf("chat sessions after delete = %#v", chatSessions)
	}
}
