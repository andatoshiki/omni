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
	if msg.Caption != "" {
		prompt = msg.Caption
	}

	mediaMsg := msg
	if len(mediaMsg.Photo) == 0 && mediaMsg.Voice == nil && mediaMsg.Audio == nil && mediaMsg.Video == nil && mediaMsg.VideoNote == nil {
		if msg.ReplyToMessage != nil {
			mediaMsg = msg.ReplyToMessage
		}
	}

	if len(mediaMsg.Photo) > 0 {
		photo, ok := largestPhoto(mediaMsg.Photo)
		if !ok {
			return providers.ChatMessage{}, "", fmt.Errorf("Telegram photo has no downloadable size")
		}
		data, mediaType, err := c.downloadFile(ctx, photo.FileID)
		if err != nil {
			return providers.ChatMessage{}, "", err
		}
		if !strings.HasPrefix(mediaType, "image/") {
			return providers.ChatMessage{}, "", fmt.Errorf("downloaded photo has unsupported media type %q", mediaType)
		}
		return imageUserMessage(prompt, mediaType, data)
	}

	if mediaMsg.Voice != nil {
		if mediaMsg.Voice.FileSize > maxTelegramImageBytes {
			return providers.ChatMessage{}, "", fmt.Errorf("this voice note is %d MB. The Telegram Bot API only allows bots to process files up to 20 MB", mediaMsg.Voice.FileSize/1024/1024)
		}
		data, _, err := c.downloadFile(ctx, mediaMsg.Voice.FileID)
		if err != nil {
			return providers.ChatMessage{}, "", err
		}
		return mediaUserMessage(prompt, "audio/ogg", data, "voice note", "Transcribe and summarize this voice note.")
	}

	if mediaMsg.Audio != nil {
		if mediaMsg.Audio.FileSize > maxTelegramImageBytes {
			return providers.ChatMessage{}, "", fmt.Errorf("this audio file is %d MB. The Telegram Bot API only allows bots to process files up to 20 MB", mediaMsg.Audio.FileSize/1024/1024)
		}
		data, mime, err := c.downloadFile(ctx, mediaMsg.Audio.FileID)
		if err != nil {
			return providers.ChatMessage{}, "", err
		}
		if mime == "application/octet-stream" || mime == "" {
			mime = "audio/mpeg" // fallback
		}
		return mediaUserMessage(prompt, mime, data, "audio file", "Transcribe and summarize this audio.")
	}

	if mediaMsg.Video != nil {
		if mediaMsg.Video.FileSize > maxTelegramImageBytes {
			return providers.ChatMessage{}, "", fmt.Errorf("this video is %d MB. The Telegram Bot API only allows bots to process files up to 20 MB", mediaMsg.Video.FileSize/1024/1024)
		}
		data, mime, err := c.downloadFile(ctx, mediaMsg.Video.FileID)
		if err != nil {
			return providers.ChatMessage{}, "", err
		}
		if mime == "application/octet-stream" || mime == "" {
			mime = "video/mp4" // fallback
		}
		return mediaUserMessage(prompt, mime, data, "video", "Describe what is happening in this video.")
	}

	if mediaMsg.VideoNote != nil {
		if mediaMsg.VideoNote.FileSize > maxTelegramImageBytes {
			return providers.ChatMessage{}, "", fmt.Errorf("this video note is %d MB. The Telegram Bot API only allows bots to process files up to 20 MB", mediaMsg.VideoNote.FileSize/1024/1024)
		}
		data, mime, err := c.downloadFile(ctx, mediaMsg.VideoNote.FileID)
		if err != nil {
			return providers.ChatMessage{}, "", err
		}
		if mime == "application/octet-stream" || mime == "" {
			mime = "video/mp4" // fallback
		}
		return mediaUserMessage(prompt, mime, data, "video note", "Describe what the person in this video note is saying and doing.")
	}

	return providers.ChatMessage{Role: providers.RoleUser, Content: prompt}, prompt, nil
}

func mediaUserMessage(prompt, mediaType string, data []byte, mediaName, defaultPrompt string) (providers.ChatMessage, string, error) {
	caption := strings.TrimSpace(prompt)
	stored := fmt.Sprintf("[User attached a %s]", mediaName)
	if caption != "" {
		stored += " " + caption
	} else {
		prompt = defaultPrompt
	}

	mediaTypePrefix := strings.SplitN(mediaType, "/", 2)[0]

	parts := []providers.ChatContentPart{
		{Type: "text", Text: prompt},
		{
			Type:      mediaTypePrefix, // "audio" or "video"
			MediaData: &providers.MediaData{MIMEType: mediaType, Data: data},
		},
	}
	return providers.ChatMessage{Role: providers.RoleUser, Content: parts}, stored, nil
}

func imageUserMessage(prompt, mediaType string, image []byte) (providers.ChatMessage, string, error) {
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

func (c *CommandHandler) downloadFile(ctx context.Context, fileID string) ([]byte, string, error) {
	file, err := c.app.client.GetFile(ctx, &telegram.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, "", fmt.Errorf("get Telegram file: %w", err)
	}
	if file.FilePath == "" {
		return nil, "", fmt.Errorf("Telegram file path is empty")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.app.client.FileDownloadLink(file), nil)
	if err != nil {
		return nil, "", fmt.Errorf("build Telegram download request: %w", err)
	}
	response, err := telegramFileHTTPClient.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("download Telegram file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("download Telegram file: unexpected HTTP status %d", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxTelegramImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read Telegram file: %w", err)
	}
	if len(data) > maxTelegramImageBytes {
		return nil, "", fmt.Errorf("Telegram file exceeds %d-byte download limit", maxTelegramImageBytes)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("Telegram file download is empty")
	}
	mediaType := strings.SplitN(http.DetectContentType(data), ";", 2)[0]
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
