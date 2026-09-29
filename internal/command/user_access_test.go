package command

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/storage"
)

func TestAddUserRequiresAdministrator(t *testing.T) {
	bot := &accessCommandBot{}
	AddUser(context.Background(), bot, &models.Message{
		From: &models.User{ID: 7},
		Text: "42",
	})
	if bot.addCalls != 0 {
		t.Fatal("non-administrator reached access mutation")
	}
	if len(bot.replies) != 1 || !strings.Contains(bot.replies[0], "Only the configured administrator") {
		t.Fatalf("replies = %#v", bot.replies)
	}
}

func TestAddUserValidatesOneArgument(t *testing.T) {
	bot := &accessCommandBot{admin: true}
	AddUser(context.Background(), bot, &models.Message{
		From: &models.User{ID: 7},
		Text: "@alice extra",
	})
	if bot.addCalls != 0 {
		t.Fatal("invalid arguments reached access mutation")
	}
	if len(bot.replies) != 1 || bot.replies[0] != "Usage: /addusr <@username|user_id>, or reply to a user's message with /addusr" {
		t.Fatalf("replies = %#v", bot.replies)
	}
}

func TestAddAndDeleteUserFromReply(t *testing.T) {
	bot := &accessCommandBot{
		admin:   true,
		result:  storage.TelegramUser{UserID: 42, Username: "alice"},
		changed: true,
	}
	message := &models.Message{
		From: &models.User{ID: 7},
		ReplyToMessage: &models.Message{
			From: &models.User{ID: 42, Username: "alice"},
		},
	}

	AddUser(context.Background(), bot, message)
	if bot.addCalls != 1 || bot.addReference != "42" || len(bot.replies) != 1 || bot.replies[0] != "✅ Allowed @alice (42)." {
		t.Fatalf("add calls = %d, reference = %q, replies = %#v", bot.addCalls, bot.addReference, bot.replies)
	}

	bot.replies = nil
	DeleteUser(context.Background(), bot, message)
	if bot.deleteCalls != 1 || bot.deleteReference != "42" || len(bot.replies) != 1 || bot.replies[0] != "✅ Removed @alice (42)." {
		t.Fatalf("delete calls = %d, reference = %q, replies = %#v", bot.deleteCalls, bot.deleteReference, bot.replies)
	}
}

func TestAccessCommandExplicitTargetTakesPrecedenceOverReply(t *testing.T) {
	bot := &accessCommandBot{admin: true}
	AddUser(context.Background(), bot, &models.Message{
		From:           &models.User{ID: 7},
		Text:           "@alice",
		ReplyToMessage: &models.Message{From: &models.User{ID: 42}},
	})
	if bot.addReference != "@alice" {
		t.Fatalf("add reference = %q, want explicit target", bot.addReference)
	}
}

func TestAccessCommandReplyRequiresUserSender(t *testing.T) {
	bot := &accessCommandBot{admin: true}
	DeleteUser(context.Background(), bot, &models.Message{
		From:           &models.User{ID: 7},
		ReplyToMessage: &models.Message{},
	})
	if bot.deleteCalls != 0 {
		t.Fatal("reply without a user sender reached access mutation")
	}
	if len(bot.replies) != 1 || !strings.Contains(bot.replies[0], "reply to a user's message") {
		t.Fatalf("replies = %#v", bot.replies)
	}
}

func TestAddAndDeleteUserResponses(t *testing.T) {
	bot := &accessCommandBot{
		admin:   true,
		result:  storage.TelegramUser{UserID: 42, Username: "alice"},
		changed: true,
	}
	message := &models.Message{From: &models.User{ID: 7}, Text: "@alice"}
	AddUser(context.Background(), bot, message)
	if bot.addCalls != 1 || len(bot.replies) != 1 || bot.replies[0] != "✅ Allowed @alice (42)." {
		t.Fatalf("add calls = %d, replies = %#v", bot.addCalls, bot.replies)
	}

	bot.replies = nil
	DeleteUser(context.Background(), bot, message)
	if bot.deleteCalls != 1 || len(bot.replies) != 1 || bot.replies[0] != "✅ Removed @alice (42)." {
		t.Fatalf("delete calls = %d, replies = %#v", bot.deleteCalls, bot.replies)
	}
}

func TestAddUserReportsResolutionError(t *testing.T) {
	bot := &accessCommandBot{admin: true, err: errors.New("unknown username @alice")}
	AddUser(context.Background(), bot, &models.Message{
		From: &models.User{ID: 7},
		Text: "@alice",
	})
	if len(bot.replies) != 1 || bot.replies[0] != "❌ unknown username @alice" {
		t.Fatalf("replies = %#v", bot.replies)
	}
}

type accessCommandBot struct {
	testBotContext
	admin           bool
	result          storage.TelegramUser
	changed         bool
	err             error
	addCalls        int
	deleteCalls     int
	addReference    string
	deleteReference string
	replies         []string
}

func (b *accessCommandBot) IsAdmin(int64) bool { return b.admin }

func (b *accessCommandBot) Reply(_ context.Context, _ *models.Message, text string) (*models.Message, error) {
	b.replies = append(b.replies, text)
	return nil, nil
}

func (b *accessCommandBot) AddAllowedUser(reference string) (storage.TelegramUser, bool, error) {
	b.addCalls++
	b.addReference = reference
	return b.result, b.changed, b.err
}

func (b *accessCommandBot) DeleteAllowedUser(reference string) (storage.TelegramUser, bool, error) {
	b.deleteCalls++
	b.deleteReference = reference
	return b.result, b.changed, b.err
}
