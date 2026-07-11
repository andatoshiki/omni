package cohere

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	cohere "github.com/cohere-ai/cohere-go/v2"
	cohereclient "github.com/cohere-ai/cohere-go/v2/client"
	coherecore "github.com/cohere-ai/cohere-go/v2/core"
	cohereoption "github.com/cohere-ai/cohere-go/v2/option"
	"github.com/andatoshiki/omni/internal/providers/platforms"
)

type Adapter struct {
	HTTPClient *http.Client
}

func (a Adapter) CreateChatCompletionStream(
	ctx context.Context,
	endpoint platforms.Endpoint,
	request *platforms.ChatCompletionStreamRequest,
) (platforms.ChatCompletionStream, error) {
	if strings.TrimSpace(endpoint.APIKey) == "" {
		return nil, errors.New("provider auth token is not configured")
	}
	if request == nil {
		return nil, errors.New("request cannot be nil")
	}

	opts := []cohereoption.RequestOption{
		cohereoption.WithToken(endpoint.APIKey),
	}
	if endpoint.BaseURL != "" {
		opts = append(opts, cohereoption.WithBaseURL(endpoint.BaseURL))
	}
	if a.HTTPClient != nil {
		opts = append(opts, cohereoption.WithHTTPClient(a.HTTPClient))
	}

	client := cohereclient.NewClient(opts...)

	var messages []*cohere.ChatMessageV2
	for _, msg := range request.Messages {
		strContent, _ := msg.Content.(string)
		
		switch msg.Role {
		case platforms.RoleSystem:
			messages = append(messages, &cohere.ChatMessageV2{
				Role: "system",
				System: &cohere.SystemMessageV2{
					Content: &cohere.SystemMessageV2Content{
						String: strContent,
					},
				},
			})
		case platforms.RoleAssistant:
			messages = append(messages, &cohere.ChatMessageV2{
				Role: "assistant",
				Assistant: &cohere.AssistantMessage{
					Content: &cohere.AssistantMessageV2Content{
						String: strContent,
					},
				},
			})
		default:
			messages = append(messages, &cohere.ChatMessageV2{
				Role: "user",
				User: &cohere.UserMessageV2{
					Content: &cohere.UserMessageV2Content{
						String: strContent,
					},
				},
			})
		}
	}

	req := &cohere.V2ChatStreamRequest{
		Model:    request.Model,
		Messages: messages,
	}
	
	if request.MaxTokens > 0 {
		req.MaxTokens = cohere.Int(request.MaxTokens)
	}
	if request.Temperature > 0 {
		req.Temperature = cohere.Float64(float64(request.Temperature))
	}

	stream, err := client.V2.ChatStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("cohere chat stream: %w", err)
	}

	return &messageStream{stream: stream}, nil
}

type messageStream struct {
	stream *coherecore.Stream[cohere.V2ChatStreamResponse]
}

func (s *messageStream) Recv() (*platforms.ChatCompletionStreamResponse, error) {
	for {
		event, err := s.stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("read cohere stream: %w", err)
		}

		if event.ContentDelta != nil && event.ContentDelta.Delta != nil && event.ContentDelta.Delta.Message != nil && event.ContentDelta.Delta.Message.Content != nil && event.ContentDelta.Delta.Message.Content.Text != nil {
			return &platforms.ChatCompletionStreamResponse{
				Choices: []platforms.StreamChoice{
					{Delta: platforms.StreamDelta{Content: *event.ContentDelta.Delta.Message.Content.Text}},
				},
			}, nil
		}
		
		if event.MessageEnd != nil && event.MessageEnd.Delta != nil && event.MessageEnd.Delta.Usage != nil && event.MessageEnd.Delta.Usage.BilledUnits != nil {
			return &platforms.ChatCompletionStreamResponse{
				Usage: &platforms.TokenUsage{
					PromptTokens:     int64(*event.MessageEnd.Delta.Usage.BilledUnits.InputTokens),
					CompletionTokens: int64(*event.MessageEnd.Delta.Usage.BilledUnits.OutputTokens),
					TotalTokens:      int64(*event.MessageEnd.Delta.Usage.BilledUnits.InputTokens + *event.MessageEnd.Delta.Usage.BilledUnits.OutputTokens),
				},
			}, nil
		}
	}
}

func (s *messageStream) Close() error {
	return s.stream.Close()
}
