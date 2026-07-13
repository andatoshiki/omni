package storage

import (
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (db *mongoStore) SaveTranscriptMessage(message TranscriptMessage) error {
	message, err := validateTranscriptMessage(message)
	if err != nil {
		return err
	}

	ctx, cancel := mongodbContext()
	defer cancel()

	documentID := fmt.Sprintf("%d:%d", message.ChatID, message.MessageID)
	_, err = db.summaryTranscript.UpdateOne(
		ctx,
		bson.M{"_id": documentID},
		bson.M{
			"$set": bson.M{
				"chat_id":      message.ChatID,
				"thread_id":    message.ThreadID,
				"message_id":   message.MessageID,
				"role":         message.Role,
				"sender_name":  message.Sender,
				"message_text": message.Text,
			},
			"$setOnInsert": bson.M{"created_at": time.Now().UTC()},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("failed to save summary transcript message: %w", err)
	}

	cursor, err := db.summaryTranscript.Find(
		ctx,
		bson.M{"chat_id": message.ChatID, "thread_id": message.ThreadID},
		options.Find().
			SetSort(bson.D{{Key: "message_id", Value: -1}}).
			SetSkip(transcriptRetentionMessages).
			SetProjection(bson.M{"_id": 1}),
	)
	if err != nil {
		return fmt.Errorf("failed to find expired summary transcript messages: %w", err)
	}
	defer cursor.Close(ctx)

	var expiredIDs []string
	for cursor.Next(ctx) {
		var document struct {
			ID string `bson:"_id"`
		}
		if err := cursor.Decode(&document); err != nil {
			return fmt.Errorf("failed to decode expired summary transcript message: %w", err)
		}
		expiredIDs = append(expiredIDs, document.ID)
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("failed to iterate expired summary transcript messages: %w", err)
	}
	if len(expiredIDs) == 0 {
		return nil
	}
	if _, err := db.summaryTranscript.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": expiredIDs}}); err != nil {
		return fmt.Errorf("failed to prune summary transcript: %w", err)
	}
	return nil
}

func (db *mongoStore) RecentTranscriptMessages(chatID int64, threadID, beforeMessageID, limit int) ([]TranscriptMessage, error) {
	limit = normalizeTranscriptLimit(limit)
	if limit == 0 {
		return []TranscriptMessage{}, nil
	}

	ctx, cancel := mongodbContext()
	defer cancel()
	cursor, err := db.summaryTranscript.Find(
		ctx,
		bson.M{
			"chat_id":    chatID,
			"thread_id":  threadID,
			"message_id": bson.M{"$lt": beforeMessageID},
		},
		options.Find().SetSort(bson.D{{Key: "message_id", Value: -1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load summary transcript: %w", err)
	}
	defer cursor.Close(ctx)

	messages := make([]TranscriptMessage, 0, limit)
	for cursor.Next(ctx) {
		var document mongoTranscriptDocument
		if err := cursor.Decode(&document); err != nil {
			return nil, fmt.Errorf("failed to decode summary transcript message: %w", err)
		}
		messages = append(messages, TranscriptMessage{
			ChatID:    document.ChatID,
			ThreadID:  document.ThreadID,
			MessageID: document.MessageID,
			Role:      document.Role,
			Sender:    document.Sender,
			Text:      document.Text,
		})
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate summary transcript: %w", err)
	}
	reverseTranscriptMessages(messages)
	return messages, nil
}
