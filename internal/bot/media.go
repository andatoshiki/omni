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

func (c *CommandHandler) userMessage(ctx context.Context, msgs ...*models.Message) (providers.ChatMessage, string, error) {
	if len(msgs) == 0 {
		return providers.ChatMessage{}, "", fmt.Errorf("no messages to process")
	}

	prompt := msgs[0].Text
	for _, m := range msgs {
		if m.Caption != "" {
			prompt = m.Caption
			break
		}
	}

	var parts []providers.ChatContentPart
	var storedSummary string
	mediaCount := 0

	for _, msg := range msgs {
		mediaMsg := msg
		if len(mediaMsg.Photo) == 0 && mediaMsg.Voice == nil && mediaMsg.Audio == nil && mediaMsg.Video == nil && mediaMsg.VideoNote == nil {
			if msg.ReplyToMessage != nil {
				mediaMsg = msg.ReplyToMessage
			}
		}

		if len(mediaMsg.Photo) > 0 {
			if photo, ok := largestPhoto(mediaMsg.Photo); ok {
				part, err := c.processMediaItem(ctx, photo.FileID, "", "image")
				if err != nil {
					return providers.ChatMessage{}, "", err
				}
				parts = append(parts, part)
				mediaCount++
				storedSummary = updateStoredSummary(storedSummary, "image")
			}
			continue
		}

		if mediaMsg.Voice != nil {
			if mediaMsg.Voice.FileSize > maxTelegramImageBytes {
				return providers.ChatMessage{}, "", fmt.Errorf("voice note exceeds 20 MB limit")
			}
			part, err := c.processMediaItem(ctx, mediaMsg.Voice.FileID, "audio/ogg", "audio")
			if err != nil {
				return providers.ChatMessage{}, "", err
			}
			parts = append(parts, part)
			mediaCount++
			storedSummary = updateStoredSummary(storedSummary, "voice note")
			continue
		}

		if mediaMsg.Audio != nil {
			if mediaMsg.Audio.FileSize > maxTelegramImageBytes {
				return providers.ChatMessage{}, "", fmt.Errorf("audio file exceeds 20 MB limit")
			}
			part, err := c.processMediaItem(ctx, mediaMsg.Audio.FileID, "audio/mpeg", "audio")
			if err != nil {
				return providers.ChatMessage{}, "", err
			}
			parts = append(parts, part)
			mediaCount++
			storedSummary = updateStoredSummary(storedSummary, "audio file")
			continue
		}

		if mediaMsg.Video != nil {
			if mediaMsg.Video.FileSize > maxTelegramImageBytes {
				return providers.ChatMessage{}, "", fmt.Errorf("video exceeds 20 MB limit")
			}
			part, err := c.processMediaItem(ctx, mediaMsg.Video.FileID, "video/mp4", "video")
			if err != nil {
				return providers.ChatMessage{}, "", err
			}
			parts = append(parts, part)
			mediaCount++
			storedSummary = updateStoredSummary(storedSummary, "video")
			continue
		}

		if mediaMsg.VideoNote != nil {
			if mediaMsg.VideoNote.FileSize > maxTelegramImageBytes {
				return providers.ChatMessage{}, "", fmt.Errorf("video note exceeds 20 MB limit")
			}
			part, err := c.processMediaItem(ctx, mediaMsg.VideoNote.FileID, "video/mp4", "video")
			if err != nil {
				return providers.ChatMessage{}, "", err
			}
			parts = append(parts, part)
			mediaCount++
			storedSummary = updateStoredSummary(storedSummary, "video note")
			continue
		}
	}

	if mediaCount == 0 {
		return providers.ChatMessage{Role: providers.RoleUser, Content: prompt}, prompt, nil
	}

	caption := strings.TrimSpace(prompt)
	if caption != "" {
		storedSummary += " " + caption
	} else {
		prompt = "What is in this media?"
	}

	finalParts := []providers.ChatContentPart{{Type: "text", Text: prompt}}
	finalParts = append(finalParts, parts...)

	return providers.ChatMessage{Role: providers.RoleUser, Content: finalParts}, storedSummary, nil
}

func updateStoredSummary(current, mediaType string) string {
	if current == "" {
		return fmt.Sprintf("[User attached a %s]", mediaType)
	}
	return "[User attached multiple media files]"
}

func (c *CommandHandler) processMediaItem(ctx context.Context, fileID string, fallbackMime string, typePrefix string) (providers.ChatContentPart, error) {
	data, mime, err := c.downloadFile(ctx, fileID)
	if err != nil {
		return providers.ChatContentPart{}, err
	}
	if mime == "application/octet-stream" || mime == "" {
		mime = fallbackMime
	}

	if typePrefix == "image" {
		if !strings.HasPrefix(mime, "image/") {
			return providers.ChatContentPart{}, fmt.Errorf("downloaded photo has unsupported media type %q", mime)
		}
		return providers.ChatContentPart{
			Type: "image_url",
			ImageURL: &providers.ChatImageURL{
				URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data),
			},
		}, nil
	}

	return providers.ChatContentPart{
		Type:      typePrefix,
		MediaData: &providers.MediaData{MIMEType: mime, Data: data},
	}, nil
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
