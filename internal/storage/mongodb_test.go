package storage

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

func TestMongoMessagesRoundTrip(t *testing.T) {
	t.Parallel()

	want := []conversation.Message{
		{
			Role:    providers.RoleUser,
			Content: "hello",
			Speaker: &conversation.Speaker{UserID: 42, DisplayName: "Test User", Username: "test"},
			ReplyTo: &conversation.ReplyContext{
				Speaker: &conversation.Speaker{UserID: 99, DisplayName: "Other User"},
				Text:    "earlier message",
			},
		},
		{
			Role: providers.RoleUser,
			Content: []providers.ChatContentPart{
				{Type: "text", Text: "describe this"},
				{Type: "image_url", ImageURL: &providers.ChatImageURL{URL: "data:image/png;base64,test"}},
			},
		},
		{Role: providers.RoleAssistant, Content: "an image"},
	}

	documents, err := toMongoMessages(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fromMongoMessages(documents)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v, want %#v", got, want)
	}
}

func TestMongoSessionMessagesUseNativeBSONArray(t *testing.T) {
	t.Parallel()

	messages, err := toMongoMessages([]conversation.Message{{
		Role:    providers.RoleUser,
		Content: []providers.ChatContentPart{{Type: "text", Text: "hello"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := bson.Marshal(mongoSessionDocument{
		ID: 1, ChatID: 42, Title: "test", Messages: messages,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	messagesValue := bson.Raw(encoded).Lookup("messages")
	if got := messagesValue.Type; got != bson.TypeArray {
		t.Fatalf("messages BSON type = %v, want array", got)
	}
	messageValues, err := messagesValue.Array().Values()
	if err != nil {
		t.Fatal(err)
	}
	if len(messageValues) != 1 || messageValues[0].Type != bson.TypeEmbeddedDocument {
		t.Fatalf("message values = %#v, want one embedded document", messageValues)
	}
	content := messageValues[0].Document().Lookup("content")
	if content.Type != bson.TypeEmbeddedDocument {
		t.Fatalf("content BSON type = %v, want embedded document", content.Type)
	}
	contentDocument := content.Document()
	if got := contentDocument.Lookup("kind").StringValue(); got != "parts" {
		t.Fatalf("content kind = %q, want parts", got)
	}
	if got := contentDocument.Lookup("parts").Type; got != bson.TypeArray {
		t.Fatalf("content parts BSON type = %v, want array", got)
	}
}

func TestMongoMessagesRejectUnsupportedContent(t *testing.T) {
	t.Parallel()

	_, err := toMongoMessages([]conversation.Message{{Role: providers.RoleUser, Content: 42}})
	if err == nil || !strings.Contains(err.Error(), "unsupported content type") {
		t.Fatalf("toMongoMessages() error = %v", err)
	}
}
