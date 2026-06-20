package bot

import (
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/config"
)

func TestMessageAllowed(t *testing.T) {
	app := &App{params: &config.Params{
		AllowedUserIDs:  []int64{10},
		AllowedGroupIDs: []int64{-100},
	}}
	tests := []struct {
		name string
		msg  *models.Message
		want bool
	}{
		{
			name: "allowed private user",
			msg:  &models.Message{Chat: models.Chat{ID: 10}, From: &models.User{ID: 10}},
			want: true,
		},
		{
			name: "disallowed private user",
			msg:  &models.Message{Chat: models.Chat{ID: 20}, From: &models.User{ID: 20}},
			want: false,
		},
		{
			name: "private message without sender",
			msg:  &models.Message{Chat: models.Chat{ID: 10}},
			want: false,
		},
		{
			name: "allowed group",
			msg:  &models.Message{Chat: models.Chat{ID: -100}},
			want: true,
		},
		{
			name: "disallowed group",
			msg:  &models.Message{Chat: models.Chat{ID: -200}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := app.messageAllowed(tt.msg); got != tt.want {
				t.Fatalf("messageAllowed() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestStripBotMention(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		username    string
		wantPrompt  string
		wantMention bool
	}{
		{
			name:        "mention with prompt",
			text:        "@omni_bot What is 2+2?",
			username:    "omni_bot",
			wantPrompt:  "What is 2+2?",
			wantMention: true,
		},
		{
			name:        "case insensitive username",
			text:        "@OMNI_BOT hello",
			username:    "@omni_bot",
			wantPrompt:  "hello",
			wantMention: true,
		},
		{
			name:        "mention only",
			text:        "@omni_bot",
			username:    "omni_bot",
			wantPrompt:  "",
			wantMention: true,
		},
		{
			name:        "mention not at start",
			text:        "hello @omni_bot",
			username:    "omni_bot",
			wantPrompt:  "hello @omni_bot",
			wantMention: false,
		},
		{
			name:        "different username",
			text:        "@other_bot hello",
			username:    "omni_bot",
			wantPrompt:  "@other_bot hello",
			wantMention: false,
		},
		{
			name:        "longer username",
			text:        "@omni_bot_extra hello",
			username:    "omni_bot",
			wantPrompt:  "@omni_bot_extra hello",
			wantMention: false,
		},
		{
			name:        "username unavailable",
			text:        "@omni_bot hello",
			username:    "",
			wantPrompt:  "@omni_bot hello",
			wantMention: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt, mentioned := stripBotMention(tt.text, tt.username)
			if prompt != tt.wantPrompt || mentioned != tt.wantMention {
				t.Fatalf("stripBotMention(%q, %q) = (%q, %t), want (%q, %t)", tt.text, tt.username, prompt, mentioned, tt.wantPrompt, tt.wantMention)
			}
		})
	}
}
