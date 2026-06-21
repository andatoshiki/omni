package bot

import (
	"fmt"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"

	"github.com/andatoshiki/omni/internal/providers"
)

const chatMessageTokenOverhead = 3
const chatReplyPrimingTokens = 3

var (
	cl100kMu       sync.Mutex
	cl100kEncoding *tiktoken.Tiktoken
)

type textTokenCounter func(string) (int, error)

// countTokens returns the cl100k_base token count for text.
func countTokens(text string) (int, error) {
	cl100kMu.Lock()
	if cl100kEncoding == nil {
		encoding, err := tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			cl100kMu.Unlock()
			return 0, fmt.Errorf("load cl100k_base tokenizer: %w", err)
		}
		cl100kEncoding = encoding
	}
	encoding := cl100kEncoding
	cl100kMu.Unlock()
	return len(encoding.EncodeOrdinary(text)), nil
}

func countChatMessageTokensWith(message providers.ChatMessage, counter textTokenCounter) (int, error) {
	roleTokens, err := counter(message.Role)
	if err != nil {
		return 0, err
	}
	contentTokens, err := countContentTokensWith(message.Content, counter)
	if err != nil {
		return 0, err
	}
	return chatMessageTokenOverhead + roleTokens + contentTokens, nil
}

func countContentTokensWith(content any, counter textTokenCounter) (int, error) {
	switch value := content.(type) {
	case nil:
		return 0, nil
	case string:
		return counter(value)
	case []providers.ChatContentPart:
		total := 0
		for _, part := range value {
			if part.Type != "text" {
				continue
			}
			count, err := counter(part.Text)
			if err != nil {
				return 0, err
			}
			total += count
		}
		return total, nil
	case []any:
		total := 0
		for _, part := range value {
			count, err := countContentTokensWith(part, counter)
			if err != nil {
				return 0, err
			}
			total += count
		}
		return total, nil
	case map[string]any:
		if value["type"] != "text" {
			return 0, nil
		}
		text, _ := value["text"].(string)
		return counter(text)
	default:
		return 0, fmt.Errorf("unsupported chat message content type %T", content)
	}
}

// messagesWithinContext returns the newest suffix of history that
// fits beside the mandatory system/current messages and the reserved reply.
func messagesWithinContext(
	system providers.ChatMessage,
	history []providers.ChatMessage,
	current []providers.ChatMessage,
	maxContextTokens int,
	maxReplyTokens int,
) (messages []providers.ChatMessage, promptTokens int, dropped int, err error) {
	return messagesWithinContextWithCounter(system, history, current, maxContextTokens, maxReplyTokens, countTokens)
}

func messagesWithinContextWithCounter(
	system providers.ChatMessage,
	history []providers.ChatMessage,
	current []providers.ChatMessage,
	maxContextTokens int,
	maxReplyTokens int,
	counter textTokenCounter,
) (messages []providers.ChatMessage, promptTokens int, dropped int, err error) {
	inputBudget := maxContextTokens - maxReplyTokens
	mandatory := make([]providers.ChatMessage, 0, 1+len(current))
	mandatory = append(mandatory, system)
	mandatory = append(mandatory, current...)

	promptTokens = chatReplyPrimingTokens
	for _, message := range mandatory {
		count, countErr := countChatMessageTokensWith(message, counter)
		if countErr != nil {
			return nil, 0, len(history), countErr
		}
		promptTokens += count
	}
	if promptTokens > inputBudget {
		return nil, promptTokens, len(history), fmt.Errorf(
			"system and current prompt require %d tokens, exceeding the %d-token input budget",
			promptTokens,
			inputBudget,
		)
	}

	start := len(history)
	for index := len(history) - 1; index >= 0; index-- {
		count, countErr := countChatMessageTokensWith(history[index], counter)
		if countErr != nil {
			return nil, 0, len(history), countErr
		}
		if promptTokens+count > inputBudget {
			break
		}
		promptTokens += count
		start = index
	}

	messages = make([]providers.ChatMessage, 0, 1+len(history)-start+len(current))
	messages = append(messages, system)
	messages = append(messages, history[start:]...)
	messages = append(messages, current...)
	return messages, promptTokens, start, nil
}
