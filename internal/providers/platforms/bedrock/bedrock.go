package bedrock

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/andatoshiki/omni/internal/providers/platforms"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type Adapter struct {
	Client *bedrockruntime.Client
}

func (a Adapter) CreateChatCompletionStream(
	ctx context.Context,
	endpoint platforms.Endpoint,
	request *platforms.ChatCompletionStreamRequest,
) (platforms.ChatCompletionStream, error) {
	if a.Client == nil {
		return nil, errors.New("bedrock client is not initialized")
	}

	var messages []types.Message
	var system []types.SystemContentBlock

	for _, msg := range request.Messages {
		if msg.Role == platforms.RoleSystem {
			contentStr, ok := msg.Content.(string)
			if ok {
				system = append(system, &types.SystemContentBlockMemberText{
					Value: contentStr,
				})
			}
			continue
		}

		var role types.ConversationRole
		if msg.Role == platforms.RoleUser {
			role = types.ConversationRoleUser
		} else if msg.Role == platforms.RoleAssistant {
			role = types.ConversationRoleAssistant
		} else {
			continue
		}

		var contentBlocks []types.ContentBlock
		switch content := msg.Content.(type) {
		case string:
			contentBlocks = append(contentBlocks, &types.ContentBlockMemberText{
				Value: content,
			})
		case []platforms.ChatContentPart:
			for _, part := range content {
				if part.Type == "text" {
					contentBlocks = append(contentBlocks, &types.ContentBlockMemberText{
						Value: part.Text,
					})
				} else if part.Type == "image_url" && part.MediaData != nil {
					var format types.ImageFormat
					switch part.MediaData.MIMEType {
					case "image/jpeg", "image/jpg":
						format = types.ImageFormatJpeg
					case "image/png":
						format = types.ImageFormatPng
					case "image/gif":
						format = types.ImageFormatGif
					case "image/webp":
						format = types.ImageFormatWebp
					default:
						return nil, &platforms.UnsupportedMediaError{Types: []string{part.MediaData.MIMEType}}
					}

					contentBlocks = append(contentBlocks, &types.ContentBlockMemberImage{
						Value: types.ImageBlock{
							Format: format,
							Source: &types.ImageSourceMemberBytes{
								Value: part.MediaData.Data,
							},
						},
					})
				}
			}
		}

		if len(contentBlocks) > 0 {
			messages = append(messages, types.Message{
				Role:    role,
				Content: contentBlocks,
			})
		}
	}

	input := &bedrockruntime.ConverseStreamInput{
		ModelId:  aws.String(request.Model),
		Messages: messages,
	}

	if len(system) > 0 {
		input.System = system
	}
	if fields := thinkingRequestFields(request.Thinking); fields != nil {
		input.AdditionalModelRequestFields = document.NewLazyDocument(fields)
	}

	inferenceConfig := &types.InferenceConfiguration{}
	hasInferenceConfig := false
	thinkingActive := request.Thinking != nil && strings.ToLower(strings.TrimSpace(request.Thinking.Mode)) != "disabled"
	if request.Temperature > 0 && !thinkingActive {
		inferenceConfig.Temperature = aws.Float32(request.Temperature)
		hasInferenceConfig = true
	}
	if request.MaxTokens > 0 {
		inferenceConfig.MaxTokens = aws.Int32(int32(request.MaxTokens))
		hasInferenceConfig = true
	}
	if hasInferenceConfig {
		input.InferenceConfig = inferenceConfig
	}

	streamCtx, cancel := context.WithCancel(ctx)

	output, err := a.Client.ConverseStream(streamCtx, input)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("bedrock converse stream: %w", err)
	}

	return newBedrockStream(output.GetStream(), cancel), nil
}

func thinkingRequestFields(thinking *platforms.ThinkingOptions) map[string]any {
	if thinking == nil {
		return nil
	}
	mode := strings.ToLower(strings.TrimSpace(thinking.Mode))
	if mode == "" {
		mode = "auto"
	}
	fields := make(map[string]any)
	switch mode {
	case "auto":
		fields["thinking"] = map[string]any{"type": "adaptive", "display": "summarized"}
	case "enabled":
		budget := 0
		if thinking.BudgetTokens != nil {
			budget = *thinking.BudgetTokens
		}
		fields["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget, "display": "summarized"}
	case "disabled":
		fields["thinking"] = map[string]any{"type": "disabled"}
	}
	if effort := strings.ToLower(strings.TrimSpace(thinking.Effort)); effort != "" && effort != "none" {
		fields["output_config"] = map[string]any{"effort": effort}
	}
	return fields
}
