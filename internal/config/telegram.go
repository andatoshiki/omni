package config

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type telegramConfig struct {
	BotToken        string                `yaml:"bot_token"`
	AdminUser       TelegramUserReference `yaml:"admin_user"`
	AllowedGroupIDs []int64               `yaml:"allowed_group_ids"`
}

// TelegramUserReference identifies one Telegram user by immutable numeric ID
// or by a case-insensitive username that starts with @.
type TelegramUserReference struct {
	ID       int64
	Username string
}

func (r TelegramUserReference) IsZero() bool {
	return r.ID == 0 && r.Username == ""
}

func (r TelegramUserReference) String() string {
	if r.ID > 0 {
		return strconv.FormatInt(r.ID, 10)
	}
	if r.Username != "" {
		return "@" + r.Username
	}
	return ""
}

func (r *TelegramUserReference) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("must be a numeric user ID or an @username")
	}
	reference, err := ParseTelegramUserReference(node.Value)
	if err != nil {
		return err
	}
	*r = reference
	return nil
}

func ParseTelegramUserReference(value string) (TelegramUserReference, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return TelegramUserReference{}, fmt.Errorf("must be a numeric user ID or an @username")
	}
	if id, err := strconv.ParseInt(value, 10, 64); err == nil {
		if id <= 0 {
			return TelegramUserReference{}, fmt.Errorf("user ID must be greater than 0")
		}
		return TelegramUserReference{ID: id}, nil
	}

	username, err := NormalizeTelegramUsername(value)
	if err != nil {
		return TelegramUserReference{}, err
	}
	return TelegramUserReference{Username: username}, nil
}

func NormalizeTelegramUsername(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "@") {
		return "", fmt.Errorf("username must start with @")
	}
	username := strings.TrimPrefix(value, "@")
	if len(username) == 0 || len(username) > 32 {
		return "", fmt.Errorf("username must contain 1 to 32 characters after @")
	}
	for _, char := range username {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' {
			continue
		}
		return "", fmt.Errorf("username may contain only letters, numbers, and underscores")
	}
	return strings.ToLower(username), nil
}
