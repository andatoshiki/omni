// Package google contains Google AI (Gemini) specific API operations.
package google

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

// Adapter uses Google's official genai SDK to access Gemini directly,
// allowing native support for audio, video, and streaming.
type Adapter struct {
	Timeout *time.Duration
}

func (a Adapter) CreateChatCompletionStream(
	ctx context.Context,
	endpoint platforms.Endpoint,
	request *platforms.ChatCompletionStreamRequest,
) (platforms.ChatCompletionStream, error) {
	if endpoint.APIKey == "" {
		return nil, errors.New("provider auth token is not configured")
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  endpoint.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}

	contents := make([]*genai.Content, 0, len(request.Messages))
	var systemInstructions []*genai.Part

	for _, msg := range request.Messages {
		if msg.Role == platforms.RoleSystem {
			// Extract system prompts
			if str, ok := msg.Content.(string); ok && str != "" {
				systemInstructions = append(systemInstructions, genai.NewPartFromText(str))
			}
			continue
		}

		role := "user"
		if msg.Role == platforms.RoleAssistant {
			role = "model"
		}

		parts := make([]*genai.Part, 0)

		switch content := msg.Content.(type) {
		case string:
			if content != "" {
				parts = append(parts, genai.NewPartFromText(content))
			}
		case []platforms.ChatContentPart:
			for _, cp := range content {
				if cp.Type == "text" {
					if cp.Text != "" {
						parts = append(parts, genai.NewPartFromText(cp.Text))
					}
				} else if cp.Type == "image_url" && cp.ImageURL != nil {
					data, mime, err := decodeDataURI(cp.ImageURL.URL)
					if err == nil {
						parts = append(parts, genai.NewPartFromBytes(data, mime))
					}
				} else if cp.MediaData != nil {
					parts = append(parts, genai.NewPartFromBytes(cp.MediaData.Data, cp.MediaData.MIMEType))
				}
			}
		}

		if len(parts) > 0 {
			contents = append(contents, &genai.Content{
				Role:  role,
				Parts: parts,
			})
		}
	}

	config := &genai.GenerateContentConfig{
		Temperature: genai.Ptr(request.Temperature),
	}
	if request.MaxTokens > 0 {
		config.MaxOutputTokens = int32(request.MaxTokens)
	}
	if len(systemInstructions) > 0 {
		config.SystemInstruction = &genai.Content{Parts: systemInstructions}
	}

	streamCtx := ctx
	var cancel context.CancelFunc
	if a.Timeout != nil {
		streamCtx, cancel = context.WithTimeout(ctx, *a.Timeout)
	} else {
		streamCtx, cancel = context.WithCancel(ctx)
	}

	stream := client.Models.GenerateContentStream(streamCtx, request.Model, contents, config)

	bridge := &geminiStream{
		ch:     make(chan streamChunk, 100),
		cancel: cancel,
	}

	go bridge.run(streamCtx, stream)

	return bridge, nil
}

func decodeDataURI(uri string) ([]byte, string, error) {
	if !strings.HasPrefix(uri, "data:") {
		return nil, "", errors.New("invalid data URI prefix")
	}
	commaIdx := strings.Index(uri, ",")
	if commaIdx == -1 {
		return nil, "", errors.New("invalid data URI format")
	}
	header := uri[5:commaIdx]
	parts := strings.Split(header, ";")
	if len(parts) < 2 || parts[1] != "base64" {
		return nil, "", errors.New("expected base64 encoding")
	}
	mime := parts[0]
	data, err := base64.StdEncoding.DecodeString(uri[commaIdx+1:])
	if err != nil {
		return nil, "", err
	}
	return data, mime, nil
}

type geminiStream struct {
	ch     chan streamChunk
	cancel context.CancelFunc
}

type streamChunk struct {
	response *platforms.ChatCompletionStreamResponse
	err      error
}

func (s *geminiStream) run(ctx context.Context, iter func(func(*genai.GenerateContentResponse, error) bool)) {
	defer close(s.ch)
	iter(func(resp *genai.GenerateContentResponse, err error) bool {
		if err != nil {
			s.ch <- streamChunk{err: err}
			return false
		}

		chunk := &platforms.ChatCompletionStreamResponse{}

		if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
			if textPart := resp.Candidates[0].Content.Parts[0].Text; textPart != "" {
				chunk.Choices = []platforms.StreamChoice{
					{Delta: platforms.StreamDelta{Content: string(textPart)}},
				}
			}
		}

		if resp.UsageMetadata != nil {
			chunk.Usage = &platforms.TokenUsage{
				PromptTokens:     int64(resp.UsageMetadata.PromptTokenCount),
				CompletionTokens: int64(resp.UsageMetadata.CandidatesTokenCount),
				TotalTokens:      int64(resp.UsageMetadata.TotalTokenCount),
			}
		}

		select {
		case <-ctx.Done():
			return false
		case s.ch <- streamChunk{response: chunk}:
			return true
		}
	})
}

func (s *geminiStream) Recv() (*platforms.ChatCompletionStreamResponse, error) {
	chunk, ok := <-s.ch
	if !ok {
		return nil, io.EOF
	}
	if chunk.err != nil {
		return nil, chunk.err
	}
	return chunk.response, nil
}

func (s *geminiStream) Close() error {
	s.cancel()
	return nil
}
