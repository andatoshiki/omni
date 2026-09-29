package storage

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (db *mongoStore) LoadTelegramUsers() ([]TelegramUser, error) {
	ctx, cancel := mongodbContext()
	defer cancel()
	cursor, err := db.telegramUsers.Find(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("failed to load telegram users: %w", err)
	}
	defer cursor.Close(ctx)

	var documents []mongoTelegramUserDocument
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, fmt.Errorf("failed to decode telegram users: %w", err)
	}

	claimCursor, err := db.telegramUsernames.Find(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("failed to load telegram username claims: %w", err)
	}
	defer claimCursor.Close(ctx)
	var claims []mongoTelegramUsernameDocument
	if err := claimCursor.All(ctx, &claims); err != nil {
		return nil, fmt.Errorf("failed to decode telegram username claims: %w", err)
	}
	return joinMongoTelegramUsers(documents, claims), nil
}

func (db *mongoStore) ObserveTelegramUser(userID int64, username, normalizedUsername string) error {
	ctx, cancel := mongodbContext()
	defer cancel()
	now := time.Now().UTC()
	identityToken := bson.NewObjectID()

	// Username ownership is a versioned two-record claim so reassignment remains
	// safe on standalone MongoDB deployments that do not support transactions.
	// LoadTelegramUsers exposes a handle only when both records have the same
	// token. Writing the user first therefore preserves the previous owner if the
	// claim write fails, while every partial state fails closed after restart.
	set := bson.M{"identity_token": identityToken, "updated_at": now}
	update := bson.M{
		"$set":         set,
		"$setOnInsert": bson.M{"allowed": false, "created_at": now},
	}
	if normalizedUsername == "" {
		update["$unset"] = bson.M{"username": "", "normalized_username": ""}
	} else {
		set["username"] = username
		set["normalized_username"] = normalizedUsername
	}
	if _, err := db.telegramUsers.UpdateOne(
		ctx,
		bson.M{"_id": userID},
		update,
		options.UpdateOne().SetUpsert(true),
	); err != nil {
		return fmt.Errorf("failed to observe telegram user: %w", err)
	}
	if normalizedUsername == "" {
		_, _ = db.telegramUsernames.DeleteMany(ctx, bson.M{"user_id": userID})
		return nil
	}

	if _, err := db.telegramUsernames.UpdateOne(
		ctx,
		bson.M{"_id": normalizedUsername},
		bson.M{"$set": bson.M{
			"user_id":        userID,
			"username":       username,
			"identity_token": identityToken,
			"updated_at":     now,
		}},
		options.UpdateOne().SetUpsert(true),
	); err != nil {
		return fmt.Errorf("failed to claim telegram username: %w", err)
	}
	_, _ = db.telegramUsernames.DeleteMany(ctx, bson.M{
		"user_id": userID,
		"_id":     bson.M{"$ne": normalizedUsername},
	})
	return nil
}

func joinMongoTelegramUsers(
	documents []mongoTelegramUserDocument,
	claims []mongoTelegramUsernameDocument,
) []TelegramUser {
	claimsByUsername := make(map[string]mongoTelegramUsernameDocument, len(claims))
	for _, claim := range claims {
		claimsByUsername[claim.NormalizedUsername] = claim
	}

	users := make([]TelegramUser, 0, len(documents))
	for _, document := range documents {
		user := TelegramUser{UserID: document.UserID, Allowed: document.Allowed}
		claim, claimed := claimsByUsername[document.NormalizedUsername]
		if document.NormalizedUsername != "" && !document.IdentityToken.IsZero() && claimed &&
			claim.UserID == document.UserID && claim.IdentityToken == document.IdentityToken {
			user.Username = claim.Username
			user.NormalizedUsername = claim.NormalizedUsername
		}
		users = append(users, user)
	}
	return users
}

func (db *mongoStore) migrateTelegramAccess(ctx context.Context) error {
	cursor, err := db.telegramUsers.Find(ctx, bson.M{
		"normalized_username": bson.M{"$exists": true, "$ne": ""},
	})
	if err != nil {
		return fmt.Errorf("load legacy telegram users: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var document mongoTelegramUserDocument
		if err := cursor.Decode(&document); err != nil {
			return fmt.Errorf("decode legacy telegram user: %w", err)
		}
		if document.IdentityToken.IsZero() {
			document.IdentityToken = bson.NewObjectID()
			if _, err := db.telegramUsers.UpdateOne(
				ctx,
				bson.M{"_id": document.UserID},
				bson.M{"$set": bson.M{"identity_token": document.IdentityToken}},
			); err != nil {
				return fmt.Errorf("version legacy telegram user: %w", err)
			}
		}
		if _, err := db.telegramUsernames.UpdateOne(
			ctx,
			bson.M{"_id": document.NormalizedUsername},
			bson.M{"$setOnInsert": bson.M{
				"user_id":        document.UserID,
				"username":       document.Username,
				"identity_token": document.IdentityToken,
				"updated_at":     document.UpdatedAt,
			}},
			options.UpdateOne().SetUpsert(true),
		); err != nil {
			return fmt.Errorf("backfill telegram username claim: %w", err)
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("iterate legacy telegram users: %w", err)
	}
	return dropMongoIndexIfPresent(ctx, db.telegramUsers, "idx_telegram_users_normalized_username")
}

func dropMongoIndexIfPresent(ctx context.Context, collection *mongo.Collection, name string) error {
	collections, err := collection.Database().ListCollectionNames(ctx, bson.M{"name": collection.Name()})
	if err != nil {
		return fmt.Errorf("list telegram user collections: %w", err)
	}
	if len(collections) == 0 {
		return nil
	}
	cursor, err := collection.Indexes().List(ctx)
	if err != nil {
		return fmt.Errorf("list telegram user indexes: %w", err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var index struct {
			Name string `bson:"name"`
		}
		if err := cursor.Decode(&index); err != nil {
			return fmt.Errorf("decode telegram user index: %w", err)
		}
		if index.Name != name {
			continue
		}
		if err := collection.Indexes().DropOne(ctx, name); err != nil {
			return fmt.Errorf("drop legacy telegram username index: %w", err)
		}
		return nil
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("iterate telegram user indexes: %w", err)
	}
	return nil
}

func (db *mongoStore) SetTelegramUserAllowed(userID int64, allowed bool) error {
	ctx, cancel := mongodbContext()
	defer cancel()
	now := time.Now().UTC()
	_, err := db.telegramUsers.UpdateOne(
		ctx,
		bson.M{"_id": userID},
		bson.M{
			"$set":         bson.M{"allowed": allowed, "updated_at": now},
			"$setOnInsert": bson.M{"created_at": now},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("failed to update telegram user access: %w", err)
	}
	return nil
}
