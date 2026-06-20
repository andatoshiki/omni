package storage

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/andatoshiki/omni/internal/providers"
)

func TestTokenUsageIsAggregatedPerUserAndChat(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	database := &Database{conn: connection}

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

func TestConversationStringContentRoundTrips(t *testing.T) {
	t.Parallel()

	connection, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	database := &Database{conn: connection}
	want := []providers.ChatMessage{
		{Role: providers.RoleUser, Content: "[User attached an image] describe this"},
		{Role: providers.RoleAssistant, Content: "A test image."},
	}
	if err := database.SaveConversation(42, want); err != nil {
		t.Fatal(err)
	}
	got, err := database.LoadConversation(42)
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
