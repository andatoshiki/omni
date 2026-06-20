package bot

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

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
