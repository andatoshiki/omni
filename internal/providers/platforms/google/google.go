// Package google contains Google AI (Gemini) specific API operations.
package google

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"google.golang.org/genai"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

// Adapter uses Google's official genai SDK to access Gemini directly,
// allowing native support for audio, video, and streaming.
type Adapter struct {
	HTTPClient *http.Client
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
		APIKey:     endpoint.APIKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: a.HTTPClient,
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
	applyThinkingConfig(config, request)
	if request.MaxTokens > 0 {
		config.MaxOutputTokens = int32(request.MaxTokens)
	}
	if len(systemInstructions) > 0 {
		config.SystemInstruction = &genai.Content{Parts: systemInstructions}
	}

	streamCtx, cancel := context.WithCancel(ctx)

	stream := client.Models.GenerateContentStream(streamCtx, request.Model, contents, config)

	bridge := &geminiStream{
		ch:     make(chan streamChunk, 100),
		cancel: cancel,
	}

	go bridge.run(streamCtx, stream)

	return bridge, nil
}

func applyThinkingConfig(config *genai.GenerateContentConfig, request *platforms.ChatCompletionStreamRequest) {
	if config == nil || request == nil || (!request.CaptureReasoning && request.Thinking == nil) {
		return
	}
	config.ThinkingConfig = &genai.ThinkingConfig{IncludeThoughts: true}
	if request.Thinking == nil {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(request.Thinking.Mode))
	if mode == "disabled" {
		config.ThinkingConfig.ThinkingBudget = genai.Ptr[int32](0)
	}
	if request.Thinking.BudgetTokens != nil {
		config.ThinkingConfig.ThinkingBudget = genai.Ptr(int32(*request.Thinking.BudgetTokens))
	}
	if effort := strings.ToUpper(strings.TrimSpace(request.Thinking.Effort)); effort != "" && effort != "NONE" {
		config.ThinkingConfig.ThinkingLevel = genai.ThinkingLevel(effort)
	}
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

		chunk := translateGeminiResponse(resp)

		select {
		case <-ctx.Done():
			return false
		case s.ch <- streamChunk{response: chunk}:
			return true
		}
	})
}

func translateGeminiResponse(resp *genai.GenerateContentResponse) *platforms.ChatCompletionStreamResponse {
	chunk := &platforms.ChatCompletionStreamResponse{}
	if resp == nil {
		return chunk
	}
	if len(resp.Candidates) > 0 && resp.Candidates[0] != nil {
		candidate := resp.Candidates[0]
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				if part == nil || part.Text == "" {
					continue
				}
				delta := platforms.StreamDelta{Content: part.Text}
				if part.Thought {
					delta = platforms.StreamDelta{ReasoningContent: part.Text}
				}
				chunk.Choices = append(chunk.Choices, platforms.StreamChoice{Delta: delta})
			}
		}
		if finishReason := string(candidate.FinishReason); finishReason != "" {
			if len(chunk.Choices) == 0 {
				chunk.Choices = append(chunk.Choices, platforms.StreamChoice{FinishReason: finishReason})
			} else {
				chunk.Choices[len(chunk.Choices)-1].FinishReason = finishReason
			}
		}
	}
	if resp.UsageMetadata != nil {
		chunk.Usage = &platforms.TokenUsage{
			PromptTokens:     int64(resp.UsageMetadata.PromptTokenCount),
			CompletionTokens: int64(resp.UsageMetadata.CandidatesTokenCount + resp.UsageMetadata.ThoughtsTokenCount),
			TotalTokens:      int64(resp.UsageMetadata.TotalTokenCount),
		}
	}
	return chunk
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
