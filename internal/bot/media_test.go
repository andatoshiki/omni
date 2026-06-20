package bot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/providers"
)

func TestImageUserMessageBuildsMultipartAndSanitizedHistory(t *testing.T) {
	message, stored, err := imageUserMessage("What is shown?", "image/jpeg", []byte("secret-image-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	parts, ok := message.Content.([]providers.ChatContentPart)
	if !ok || len(parts) != 2 {
		t.Fatalf("Content = %#v, want two multipart items", message.Content)
	}
	if parts[0].Type != "text" || parts[0].Text != "What is shown?" {
		t.Fatalf("text part = %#v", parts[0])
	}
	if parts[1].ImageURL == nil || !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/jpeg;base64,") {
		t.Fatalf("image part = %#v", parts[1])
	}
	if stored != "[User attached an image] What is shown?" || strings.Contains(stored, "base64") {
		t.Fatalf("stored prompt = %q", stored)
	}

	history := appendTurnToHistory(nil, &models.Message{}, stored, "It is a test.", 4)
	encoded, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-image-bytes") || strings.Contains(string(encoded), "base64") {
		t.Fatalf("persistable history contains image payload: %s", encoded)
	}
	if _, ok := history[0].Content.(string); !ok {
		t.Fatalf("persisted user content type = %T, want string", history[0].Content)
	}
}

func TestImageUserMessageAddsDefaultPrompt(t *testing.T) {
	message, stored, err := imageUserMessage("", "image/png", []byte("png"))
	if err != nil {
		t.Fatal(err)
	}
	parts := message.Content.([]providers.ChatContentPart)
	if parts[0].Text != "What is in this image?" || stored != "[User attached an image]" {
		t.Fatalf("parts=%#v stored=%q", parts, stored)
	}
}

func TestLargestPhotoUsesResolutionThenFileSize(t *testing.T) {
	got, ok := largestPhoto([]models.PhotoSize{
		{FileID: "small", Width: 100, Height: 100, FileSize: 500},
		{FileID: "large-old", Width: 200, Height: 200, FileSize: 1000},
		{FileID: "large", Width: 200, Height: 200, FileSize: 1200},
	})
	if !ok || got.FileID != "large" {
		t.Fatalf("largestPhoto() = %#v, %v", got, ok)
	}
}
