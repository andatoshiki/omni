package bot

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestExtractSpeaker(t *testing.T) {
	msg1 := &models.Message{
		From: &models.User{
			ID:        123,
			FirstName: "Alice",
			LastName:  "Smith",
			Username:  "alicesmith",
		},
	}
	speaker1 := ExtractSpeaker(msg1)
	if speaker1 == nil || speaker1.DisplayName != "Alice Smith" || speaker1.Username != "alicesmith" {
		t.Errorf("unexpected speaker1: %+v", speaker1)
	}

	msg2 := &models.Message{
		From: &models.User{
			ID:       456,
			Username: "bob",
		},
	}
	speaker2 := ExtractSpeaker(msg2)
	if speaker2 == nil || speaker2.DisplayName != "bob" || speaker2.Username != "bob" {
		t.Errorf("unexpected speaker2: %+v", speaker2)
	}

	msg3 := &models.Message{
		SenderChat: &models.Chat{
			ID:       789,
			Title:    "Admin Channel",
			Username: "admin_chan",
		},
	}
	speaker3 := ExtractSpeaker(msg3)
	if speaker3 == nil || speaker3.DisplayName != "Admin Channel" || speaker3.Username != "admin_chan" {
		t.Errorf("unexpected speaker3: %+v", speaker3)
	}
}

func TestExtractMentions(t *testing.T) {
	msg := &models.Message{
		Text: "Hello @alice and John",
		Entities: []models.MessageEntity{
			{Type: "mention", Offset: 6, Length: 6},
			{
				Type: "text_mention", Offset: 17, Length: 4,
				User: &models.User{ID: 999, FirstName: "John", LastName: "Doe", Username: "johndoe"},
			},
		},
	}

	mentions := ExtractMentions(msg)
	if len(mentions) != 2 {
		t.Fatalf("expected 2 mentions, got %d", len(mentions))
	}
	if mentions[0].Username != "alice" {
		t.Errorf("expected alice, got %v", mentions[0].Username)
	}
	if mentions[1].DisplayName != "John Doe" || mentions[1].Username != "johndoe" {
		t.Errorf("unexpected text_mention: %+v", mentions[1])
	}
}
