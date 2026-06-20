package bot

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/providers"
)

const maxTelegramImageBytes = 20 << 20

var telegramFileHTTPClient = &http.Client{Timeout: time.Minute}

func (c *CommandHandler) userMessage(ctx context.Context, msg *models.Message) (providers.ChatMessage, string, error) {
	prompt := msg.Text
	if len(msg.Photo) == 0 {
		return providers.ChatMessage{Role: providers.RoleUser, Content: prompt}, prompt, nil
	}
	if msg.Caption != "" {
		prompt = msg.Caption
	}

	image, mediaType, err := c.downloadLargestPhoto(ctx, msg.Photo)
	if err != nil {
		return providers.ChatMessage{}, "", err
	}
	return imageUserMessage(prompt, mediaType, image)
}

func imageUserMessage(prompt, mediaType string, image []byte) (providers.ChatMessage, string, error) {
	if !strings.HasPrefix(mediaType, "image/") {
		return providers.ChatMessage{}, "", fmt.Errorf("downloaded Telegram photo has unsupported media type %q", mediaType)
	}

	caption := strings.TrimSpace(prompt)
	stored := "[User attached an image]"
	if caption != "" {
		stored += " " + caption
	} else {
		prompt = "What is in this image?"
	}

	parts := []providers.ChatContentPart{
		{Type: "text", Text: prompt},
		{
			Type: "image_url",
			ImageURL: &providers.ChatImageURL{
				URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(image),
			},
		},
	}
	return providers.ChatMessage{Role: providers.RoleUser, Content: parts}, stored, nil
}

func (c *CommandHandler) downloadLargestPhoto(ctx context.Context, photos []models.PhotoSize) ([]byte, string, error) {
	photo, ok := largestPhoto(photos)
	if !ok {
		return nil, "", fmt.Errorf("Telegram photo has no downloadable size")
	}
	file, err := c.app.client.GetFile(ctx, &telegram.GetFileParams{FileID: photo.FileID})
	if err != nil {
		return nil, "", fmt.Errorf("get Telegram photo file: %w", err)
	}
	if file.FilePath == "" {
		return nil, "", fmt.Errorf("Telegram photo file path is empty")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.app.client.FileDownloadLink(file), nil)
	if err != nil {
		return nil, "", fmt.Errorf("build Telegram photo download: %w", err)
	}
	response, err := telegramFileHTTPClient.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("download Telegram photo: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("download Telegram photo: unexpected HTTP status %d", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxTelegramImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read Telegram photo: %w", err)
	}
	if len(data) > maxTelegramImageBytes {
		return nil, "", fmt.Errorf("Telegram photo exceeds %d-byte download limit", maxTelegramImageBytes)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("Telegram photo download is empty")
	}
	mediaType := strings.SplitN(http.DetectContentType(data), ";", 2)[0]
	if !strings.HasPrefix(mediaType, "image/") {
		return nil, "", fmt.Errorf("downloaded Telegram photo has unsupported media type %q", mediaType)
	}
	return data, mediaType, nil
}

func largestPhoto(photos []models.PhotoSize) (models.PhotoSize, bool) {
	if len(photos) == 0 {
		return models.PhotoSize{}, false
	}
	largest := photos[0]
	for _, photo := range photos[1:] {
		largestPixels := int64(largest.Width) * int64(largest.Height)
		pixels := int64(photo.Width) * int64(photo.Height)
		if pixels > largestPixels || (pixels == largestPixels && photo.FileSize > largest.FileSize) {
			largest = photo
		}
	}
	return largest, true
}
