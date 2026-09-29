package bot

import (
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/config"
)

func TestMessageAllowed(t *testing.T) {
	app := &App{params: &config.Params{
		AllowedGroupIDs: []int64{-100},
	}, access: newTestAccessManager([]int64{10}, 99)}
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

func TestIdentityObservationScope(t *testing.T) {
	app := &App{params: &config.Params{AllowedGroupIDs: []int64{-100}}}
	if !app.chatAllowsIdentityObservation(42) {
		t.Fatal("private chat should be observed before user authorization")
	}
	if !app.chatAllowsIdentityObservation(-100) {
		t.Fatal("allowed group should be observed")
	}
	if app.chatAllowsIdentityObservation(-200) {
		t.Fatal("unallowed group should not create observed-user state")
	}
}

func TestCallbackAllowedUsesSameAccessPolicy(t *testing.T) {
	app := &App{
		params: &config.Params{AllowedGroupIDs: []int64{-100}},
		access: newTestAccessManager([]int64{10}, 99),
	}
	if !app.callbackAllowed(&models.CallbackQuery{From: models.User{ID: 10}}, 10) {
		t.Fatal("allowed private callback was rejected")
	}
	if app.callbackAllowed(&models.CallbackQuery{From: models.User{ID: 20}}, 20) {
		t.Fatal("revoked private callback was accepted")
	}
	if !app.callbackAllowed(&models.CallbackQuery{From: models.User{ID: 20}}, -100) {
		t.Fatal("callback in allowed group was rejected")
	}
}

func TestCommandTargetsBot(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "plain command", token: "/addusr", want: true},
		{name: "this bot", token: "/addusr@omni_bot", want: true},
		{name: "this bot case insensitive", token: "/addusr@OMNI_BOT", want: true},
		{name: "another bot", token: "/addusr@other_bot", want: false},
		{name: "empty target", token: "/addusr@", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := commandTargetsBot(test.token, "omni_bot"); got != test.want {
				t.Fatalf("commandTargetsBot(%q) = %t, want %t", test.token, got, test.want)
			}
		})
	}
}

func TestRemovingUserRevokesMessagesAndCallbacks(t *testing.T) {
	app := &App{params: &config.Params{}, access: newTestAccessManager([]int64{10}, 99)}
	message := &models.Message{Chat: models.Chat{ID: 10}, From: &models.User{ID: 10}}
	query := &models.CallbackQuery{From: models.User{ID: 10}}
	if !app.messageAllowed(message) || !app.callbackAllowed(query, 10) {
		t.Fatal("test user was not initially allowed")
	}
	app.access.store = newAccessTestStore()
	if _, err := app.access.remove("10"); err != nil {
		t.Fatal(err)
	}
	if app.messageAllowed(message) || app.callbackAllowed(query, 10) {
		t.Fatal("removed user retained message or callback access")
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

func TestStripOmniWakeWord(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		wantPrompt string
		wantWake   bool
	}{
		{
			name:       "title case wake word",
			text:       "Omni, what is 2+2?",
			wantPrompt: "what is 2+2?",
			wantWake:   true,
		},
		{
			name:       "lowercase wake word",
			text:       "omni help me",
			wantPrompt: "help me",
			wantWake:   true,
		},
		{
			name:       "uppercase wake word",
			text:       "OMNI: help me",
			wantPrompt: "help me",
			wantWake:   true,
		},
		{
			name:       "mixed case wake word",
			text:       "oMnI: help me",
			wantPrompt: "help me",
			wantWake:   true,
		},
		{
			name:       "wake word after sentence",
			text:       "This is the context. Omni, explain it.",
			wantPrompt: "This is the context. explain it.",
			wantWake:   true,
		},
		{
			name:       "wake word after newline",
			text:       "This is the context:\nOmni — explain it.",
			wantPrompt: "This is the context:\nexplain it.",
			wantWake:   true,
		},
		{
			name:       "full width sentence terminator",
			text:       "Background。Omni，explain it.",
			wantPrompt: "Background。explain it.",
			wantWake:   true,
		},
		{
			name:       "wake word only",
			text:       "Omni",
			wantPrompt: "",
			wantWake:   true,
		},
		{
			name:       "wake word in middle of sentence",
			text:       "Can Omni explain this?",
			wantPrompt: "Can Omni explain this?",
			wantWake:   false,
		},
		{
			name:       "longer word does not trigger",
			text:       "Omnibus routes are useful.",
			wantPrompt: "Omnibus routes are useful.",
			wantWake:   false,
		},
		{
			name:       "username style word does not trigger",
			text:       "omni_bot explain this",
			wantPrompt: "omni_bot explain this",
			wantWake:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt, awakened := stripOmniWakeWord(tt.text)
			if prompt != tt.wantPrompt || awakened != tt.wantWake {
				t.Fatalf("stripOmniWakeWord(%q) = (%q, %t), want (%q, %t)", tt.text, prompt, awakened, tt.wantPrompt, tt.wantWake)
			}
		})
	}
}
