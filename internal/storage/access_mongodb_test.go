package storage

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestJoinMongoTelegramUsersAcceptsOnlyMatchingClaims(t *testing.T) {
	oldToken := bson.NewObjectID()
	newToken := bson.NewObjectID()
	documents := []mongoTelegramUserDocument{
		{UserID: 1, Username: "Alice", NormalizedUsername: "alice", IdentityToken: oldToken, Allowed: true},
		{UserID: 2, Username: "Alice", NormalizedUsername: "alice", IdentityToken: newToken},
	}

	t.Run("failed claim write preserves prior owner", func(t *testing.T) {
		users := joinMongoTelegramUsers(documents, []mongoTelegramUsernameDocument{
			{NormalizedUsername: "alice", UserID: 1, Username: "Alice", IdentityToken: oldToken},
		})
		assertMongoUsernameOwner(t, users, "alice", 1)
		if !users[0].Allowed {
			t.Fatal("joining username claims changed access state")
		}
	})

	t.Run("successful claim write transfers owner", func(t *testing.T) {
		users := joinMongoTelegramUsers(documents, []mongoTelegramUsernameDocument{
			{NormalizedUsername: "alice", UserID: 2, Username: "Alice", IdentityToken: newToken},
		})
		assertMongoUsernameOwner(t, users, "alice", 2)
	})

	t.Run("mismatched token fails closed", func(t *testing.T) {
		users := joinMongoTelegramUsers(documents, []mongoTelegramUsernameDocument{
			{NormalizedUsername: "alice", UserID: 2, Username: "Alice", IdentityToken: bson.NewObjectID()},
		})
		assertMongoUsernameOwner(t, users, "alice", 0)
	})
}

func assertMongoUsernameOwner(t *testing.T, users []TelegramUser, username string, wantUserID int64) {
	t.Helper()
	var owner int64
	for _, user := range users {
		if user.NormalizedUsername == username {
			if owner != 0 {
				t.Fatalf("username %q has multiple owners", username)
			}
			owner = user.UserID
		}
	}
	if owner != wantUserID {
		t.Fatalf("username %q owner = %d, want %d", username, owner, wantUserID)
	}
}
