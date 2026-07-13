package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

const mongodbOperationTimeout = 10 * time.Second

type mongoStore struct {
	client            *mongo.Client
	sessions          *mongo.Collection
	summaryTranscript *mongo.Collection
	activeSessions    *mongo.Collection
	userContext       *mongo.Collection
	tokenUsage        *mongo.Collection
	chatModels        *mongo.Collection
	counters          *mongo.Collection
}

type mongoSessionDocument struct {
	ID             int64                  `bson:"_id"`
	ChatID         int64                  `bson:"chat_id"`
	Title          string                 `bson:"title"`
	TitleGenerated bool                   `bson:"title_generated"`
	Messages       []mongoMessageDocument `bson:"messages"`
	CreatedAt      time.Time              `bson:"created_at"`
	UpdatedAt      time.Time              `bson:"updated_at"`
}

type mongoTranscriptDocument struct {
	ID        string    `bson:"_id"`
	ChatID    int64     `bson:"chat_id"`
	ThreadID  int       `bson:"thread_id"`
	MessageID int       `bson:"message_id"`
	Role      string    `bson:"role"`
	Sender    string    `bson:"sender_name"`
	Text      string    `bson:"message_text"`
	CreatedAt time.Time `bson:"created_at"`
}

type mongoMessageDocument struct {
	Role    string                      `bson:"role"`
	Content mongoMessageContentDocument `bson:"content"`
	Speaker *mongoSpeakerDocument       `bson:"speaker,omitempty"`
	ReplyTo *mongoReplyContextDocument  `bson:"reply_to,omitempty"`
}

type mongoMessageContentDocument struct {
	Kind  string                     `bson:"kind"`
	Text  string                     `bson:"text,omitempty"`
	Parts []mongoContentPartDocument `bson:"parts,omitempty"`
}

type mongoContentPartDocument struct {
	Type     string                 `bson:"type"`
	Text     string                 `bson:"text,omitempty"`
	ImageURL *mongoImageURLDocument `bson:"image_url,omitempty"`
}

type mongoImageURLDocument struct {
	URL string `bson:"url"`
}

type mongoSpeakerDocument struct {
	UserID      int64  `bson:"user_id,omitempty"`
	DisplayName string `bson:"display_name,omitempty"`
	Username    string `bson:"username,omitempty"`
}

type mongoReplyContextDocument struct {
	Speaker *mongoSpeakerDocument `bson:"speaker,omitempty"`
	Text    string                `bson:"text,omitempty"`
}

type mongoActiveSessionDocument struct {
	ChatID    int64 `bson:"_id"`
	SessionID int64 `bson:"session_id"`
}

type mongoUserContextDocument struct {
	ChatID      int64     `bson:"_id"`
	ContextData string    `bson:"context_data"`
	UpdatedAt   time.Time `bson:"updated_at"`
}

type mongoTokenUsageDocument struct {
	ChatID           int64     `bson:"chat_id"`
	UserID           int64     `bson:"user_id"`
	PromptTokens     int64     `bson:"prompt_tokens"`
	CompletionTokens int64     `bson:"completion_tokens"`
	TotalTokens      int64     `bson:"total_tokens"`
	CreatedAt        time.Time `bson:"created_at"`
}

type mongoChatModelDocument struct {
	ChatID    int64     `bson:"_id"`
	Provider  string    `bson:"provider"`
	Model     string    `bson:"model"`
	UpdatedAt time.Time `bson:"updated_at"`
}

type mongoCounterDocument struct {
	Sequence int64 `bson:"sequence"`
}

func newMongoDBStore(cfg config.MongoDBConfig) (Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(cfg.URI).SetAppName("omni"))
	if err != nil {
		return nil, fmt.Errorf("failed to configure mongodb client: %w", err)
	}

	pingContext, cancelPing := mongodbContext()
	if err := client.Ping(pingContext, readpref.Primary()); err != nil {
		cancelPing()
		disconnectMongoClient(client)
		return nil, fmt.Errorf("failed to connect to mongodb: %w", err)
	}
	cancelPing()

	database := client.Database(cfg.DBName)
	db := &mongoStore{
		client:            client,
		sessions:          database.Collection("sessions"),
		summaryTranscript: database.Collection("summary_transcript"),
		activeSessions:    database.Collection("active_sessions"),
		userContext:       database.Collection("user_context"),
		tokenUsage:        database.Collection("token_usage"),
		chatModels:        database.Collection("chat_models"),
		counters:          database.Collection("counters"),
	}
	indexContext, cancelIndexes := mongodbContext()
	err = db.createIndexes(indexContext)
	cancelIndexes()
	if err != nil {
		disconnectMongoClient(client)
		return nil, fmt.Errorf("failed to initialize mongodb indexes: %w", err)
	}

	slog.Default().Info("mongodb database initialized", "db", cfg.DBName)
	return db, nil
}

func mongodbContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), mongodbOperationTimeout)
}

func disconnectMongoClient(client *mongo.Client) {
	ctx, cancel := mongodbContext()
	defer cancel()
	_ = client.Disconnect(ctx)
}

func (db *mongoStore) createIndexes(ctx context.Context) error {
	if _, err := db.sessions.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "chat_id", Value: 1}, {Key: "updated_at", Value: -1}},
			Options: options.Index().SetName("idx_sessions_chat_updated"),
		},
	}); err != nil {
		return err
	}
	if _, err := db.summaryTranscript.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "chat_id", Value: 1}, {Key: "thread_id", Value: 1}, {Key: "message_id", Value: -1}},
			Options: options.Index().SetName("idx_summary_transcript_scope"),
		},
	}); err != nil {
		return err
	}
	_, err := db.tokenUsage.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "chat_id", Value: 1}, {Key: "user_id", Value: 1}},
			Options: options.Index().SetName("idx_token_usage_chat_user"),
		},
	})
	return err
}

func (db *mongoStore) SaveSession(chatID int64, sessionID int64, messages []conversation.Message) error {
	documents, err := toMongoMessages(messages)
	if err != nil {
		return fmt.Errorf("failed to encode session messages: %w", err)
	}

	ctx, cancel := mongodbContext()
	defer cancel()
	_, err = db.sessions.UpdateOne(
		ctx,
		bson.M{"_id": sessionID, "chat_id": chatID},
		bson.M{"$set": bson.M{"messages": documents, "updated_at": time.Now().UTC()}},
	)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

func (db *mongoStore) LoadSession(chatID int64, sessionID int64) ([]conversation.Message, error) {
	ctx, cancel := mongodbContext()
	defer cancel()

	var document struct {
		Messages []mongoMessageDocument `bson:"messages"`
	}
	err := db.sessions.FindOne(ctx, bson.M{"_id": sessionID, "chat_id": chatID}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return []conversation.Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load session: %w", err)
	}

	messages, err := fromMongoMessages(document.Messages)
	if err != nil {
		return nil, fmt.Errorf("failed to decode session messages: %w", err)
	}
	return messages, nil
}

func (db *mongoStore) GetActiveSession(chatID int64) (SessionMeta, error) {
	ctx, cancel := mongodbContext()
	defer cancel()

	var active mongoActiveSessionDocument
	if err := db.activeSessions.FindOne(ctx, bson.M{"_id": chatID}).Decode(&active); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return SessionMeta{}, sql.ErrNoRows
		}
		return SessionMeta{}, fmt.Errorf("failed to load active session: %w", err)
	}

	var session mongoSessionDocument
	if err := db.sessions.FindOne(
		ctx,
		bson.M{"_id": active.SessionID, "chat_id": chatID},
		options.FindOne().SetProjection(bson.M{"messages": 0}),
	).Decode(&session); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return SessionMeta{}, sql.ErrNoRows
		}
		return SessionMeta{}, fmt.Errorf("failed to load active session metadata: %w", err)
	}
	return mongoSessionMeta(session), nil
}

func (db *mongoStore) SetActiveSession(chatID int64, sessionID int64) error {
	ctx, cancel := mongodbContext()
	defer cancel()
	if err := db.ensureSessionBelongsToChat(ctx, chatID, sessionID); err != nil {
		return err
	}

	_, err := db.activeSessions.UpdateOne(
		ctx,
		bson.M{"_id": chatID},
		bson.M{"$set": bson.M{"session_id": sessionID}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("failed to set active session: %w", err)
	}
	return nil
}

func (db *mongoStore) ensureSessionBelongsToChat(ctx context.Context, chatID int64, sessionID int64) error {
	err := db.sessions.FindOne(
		ctx,
		bson.M{"_id": sessionID, "chat_id": chatID},
		options.FindOne().SetProjection(bson.M{"_id": 1}),
	).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("session %d not found for chat %d", sessionID, chatID)
	}
	if err != nil {
		return fmt.Errorf("failed to verify session ownership: %w", err)
	}
	return nil
}

func (db *mongoStore) nextSessionID(ctx context.Context) (int64, error) {
	for attempt := 0; attempt < 3; attempt++ {
		var counter mongoCounterDocument
		err := db.counters.FindOneAndUpdate(
			ctx,
			bson.M{"_id": "sessions"},
			bson.M{"$inc": bson.M{"sequence": 1}},
			options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
		).Decode(&counter)
		if err == nil {
			return counter.Sequence, nil
		}
		if !mongo.IsDuplicateKeyError(err) {
			return 0, fmt.Errorf("failed to allocate session id: %w", err)
		}
	}
	return 0, fmt.Errorf("failed to allocate session id after concurrent counter initialization")
}

func (db *mongoStore) CreateNewSession(chatID int64, title string) (SessionMeta, error) {
	ctx, cancel := mongodbContext()
	defer cancel()

	sessionID, err := db.nextSessionID(ctx)
	if err != nil {
		return SessionMeta{}, err
	}
	now := time.Now().UTC()
	document := mongoSessionDocument{
		ID:        sessionID,
		ChatID:    chatID,
		Title:     title,
		Messages:  make([]mongoMessageDocument, 0),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := db.sessions.InsertOne(ctx, document); err != nil {
		return SessionMeta{}, fmt.Errorf("failed to create new session: %w", err)
	}
	if _, err := db.activeSessions.UpdateOne(
		ctx,
		bson.M{"_id": chatID},
		bson.M{"$set": bson.M{"session_id": sessionID}},
		options.UpdateOne().SetUpsert(true),
	); err != nil {
		return SessionMeta{}, fmt.Errorf("failed to set active session: %w", err)
	}
	return mongoSessionMeta(document), nil
}

func (db *mongoStore) ListSessions(chatID int64, limit int) ([]SessionMeta, error) {
	ctx, cancel := mongodbContext()
	defer cancel()

	findOptions := options.Find().
		SetProjection(bson.M{"messages": 0}).
		SetSort(bson.D{{Key: "updated_at", Value: -1}})
	if limit > 0 {
		findOptions.SetLimit(int64(limit))
	}
	cursor, err := db.sessions.Find(ctx, bson.M{"chat_id": chatID}, findOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer cursor.Close(ctx)

	var sessions []SessionMeta
	for cursor.Next(ctx) {
		var document mongoSessionDocument
		if err := cursor.Decode(&document); err != nil {
			return nil, fmt.Errorf("failed to decode session metadata: %w", err)
		}
		sessions = append(sessions, mongoSessionMeta(document))
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate sessions: %w", err)
	}
	return sessions, nil
}

func (db *mongoStore) UpdateSessionTitle(sessionID int64, title string, generated bool) error {
	ctx, cancel := mongodbContext()
	defer cancel()
	_, err := db.sessions.UpdateOne(
		ctx,
		bson.M{"_id": sessionID},
		bson.M{"$set": bson.M{
			"title":           title,
			"title_generated": generated,
			"updated_at":      time.Now().UTC(),
		}},
	)
	if err != nil {
		return fmt.Errorf("failed to update session title: %w", err)
	}
	return nil
}

func (db *mongoStore) DeleteSession(chatID int64, sessionID int64) error {
	ctx, cancel := mongodbContext()
	defer cancel()

	result, err := db.sessions.DeleteOne(ctx, bson.M{"_id": sessionID, "chat_id": chatID})
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	if result.DeletedCount == 0 {
		return fmt.Errorf("session %d not found for chat %d", sessionID, chatID)
	}
	if _, err := db.activeSessions.DeleteOne(ctx, bson.M{"_id": chatID, "session_id": sessionID}); err != nil {
		return fmt.Errorf("failed to clear active session: %w", err)
	}
	return nil
}

func (db *mongoStore) ClearSessions(chatID int64) error {
	ctx, cancel := mongodbContext()
	defer cancel()
	if _, err := db.sessions.DeleteMany(ctx, bson.M{"chat_id": chatID}); err != nil {
		return fmt.Errorf("failed to delete sessions: %w", err)
	}
	if _, err := db.activeSessions.DeleteOne(ctx, bson.M{"_id": chatID}); err != nil {
		return fmt.Errorf("failed to clear active session: %w", err)
	}
	return nil
}

func (db *mongoStore) SaveUserContext(chatID int64, userContext string) error {
	ctx, cancel := mongodbContext()
	defer cancel()
	_, err := db.userContext.UpdateOne(
		ctx,
		bson.M{"_id": chatID},
		bson.M{"$set": bson.M{"context_data": userContext, "updated_at": time.Now().UTC()}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("failed to save user context: %w", err)
	}
	return nil
}

func (db *mongoStore) LoadUserContext(chatID int64) (string, error) {
	ctx, cancel := mongodbContext()
	defer cancel()
	var document mongoUserContextDocument
	err := db.userContext.FindOne(ctx, bson.M{"_id": chatID}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to load user context: %w", err)
	}
	return document.ContextData, nil
}

func (db *mongoStore) GetAllChats() ([]int64, error) {
	ctx, cancel := mongodbContext()
	defer cancel()
	var chatIDs []int64
	if err := db.sessions.Distinct(ctx, "chat_id", bson.D{}).Decode(&chatIDs); err != nil {
		return nil, fmt.Errorf("failed to get chats: %w", err)
	}
	return chatIDs, nil
}

func (db *mongoStore) ExportMemory(filename string) error {
	type sessionExport struct {
		ID        int64                  `json:"id"`
		Title     string                 `json:"title"`
		Messages  []conversation.Message `json:"messages"`
		UpdatedAt string                 `json:"updated_at"`
	}
	type conversationExport struct {
		ChatID   int64           `json:"chat_id"`
		Context  string          `json:"context,omitempty"`
		Sessions []sessionExport `json:"sessions"`
	}

	chatIDs, err := db.GetAllChats()
	if err != nil {
		return err
	}
	exports := make([]conversationExport, 0, len(chatIDs))
	for _, chatID := range chatIDs {
		userContext, _ := db.LoadUserContext(chatID)
		sessionMetas, err := db.ListSessions(chatID, 0)
		if err != nil {
			continue
		}
		sessions := make([]sessionExport, 0, len(sessionMetas))
		for _, meta := range sessionMetas {
			messages, _ := db.LoadSession(chatID, meta.ID)
			sessions = append(sessions, sessionExport{
				ID: meta.ID, Title: meta.Title, Messages: messages, UpdatedAt: meta.UpdatedAt,
			})
		}
		exports = append(exports, conversationExport{
			ChatID: chatID, Context: userContext, Sessions: sessions,
		})
	}

	data, err := json.MarshalIndent(exports, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal exports: %w", err)
	}
	if err := os.WriteFile(filename, data, 0600); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}
	slog.Default().Info("memory exported", "file", filename, "chats", len(exports))
	return nil
}

func (db *mongoStore) SaveTokenUsage(chatID, userID int64, usage providers.TokenUsage) error {
	ctx, cancel := mongodbContext()
	defer cancel()
	_, err := db.tokenUsage.InsertOne(ctx, mongoTokenUsageDocument{
		ChatID:           chatID,
		UserID:           userID,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		CreatedAt:        time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("failed to save token usage: %w", err)
	}
	return nil
}

func (db *mongoStore) GetTokenUsage(chatID, userID int64) (TokenUsageSummary, error) {
	ctx, cancel := mongodbContext()
	defer cancel()
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "chat_id", Value: chatID}, {Key: "user_id", Value: userID}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "requests", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "prompt_tokens", Value: bson.D{{Key: "$sum", Value: "$prompt_tokens"}}},
			{Key: "completion_tokens", Value: bson.D{{Key: "$sum", Value: "$completion_tokens"}}},
			{Key: "total_tokens", Value: bson.D{{Key: "$sum", Value: "$total_tokens"}}},
		}}},
	}
	cursor, err := db.tokenUsage.Aggregate(ctx, pipeline)
	if err != nil {
		return TokenUsageSummary{}, fmt.Errorf("failed to load token usage: %w", err)
	}
	defer cursor.Close(ctx)

	if !cursor.Next(ctx) {
		if err := cursor.Err(); err != nil {
			return TokenUsageSummary{}, fmt.Errorf("failed to load token usage: %w", err)
		}
		return TokenUsageSummary{}, nil
	}
	var summary struct {
		Requests         int64 `bson:"requests"`
		PromptTokens     int64 `bson:"prompt_tokens"`
		CompletionTokens int64 `bson:"completion_tokens"`
		TotalTokens      int64 `bson:"total_tokens"`
	}
	if err := cursor.Decode(&summary); err != nil {
		return TokenUsageSummary{}, fmt.Errorf("failed to decode token usage: %w", err)
	}
	return TokenUsageSummary(summary), nil
}

func (db *mongoStore) SaveChatModel(chatID int64, provider, model string) error {
	ctx, cancel := mongodbContext()
	defer cancel()
	_, err := db.chatModels.UpdateOne(
		ctx,
		bson.M{"_id": chatID},
		bson.M{"$set": bson.M{
			"provider":   provider,
			"model":      model,
			"updated_at": time.Now().UTC(),
		}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("failed to save chat model: %w", err)
	}
	return nil
}

func (db *mongoStore) LoadChatModel(chatID int64) (providers.ModelID, bool) {
	ctx, cancel := mongodbContext()
	defer cancel()
	var document mongoChatModelDocument
	if err := db.chatModels.FindOne(ctx, bson.M{"_id": chatID}).Decode(&document); err != nil {
		return providers.ModelID{}, false
	}
	return providers.ModelID{Provider: document.Provider, Model: document.Model}, true
}

func (db *mongoStore) Close() error {
	if db.client == nil {
		return nil
	}
	ctx, cancel := mongodbContext()
	defer cancel()
	return db.client.Disconnect(ctx)
}

func mongoSessionMeta(document mongoSessionDocument) SessionMeta {
	return SessionMeta{
		ID:             document.ID,
		ChatID:         document.ChatID,
		Title:          document.Title,
		TitleGenerated: document.TitleGenerated,
		UpdatedAt:      document.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toMongoMessages(messages []conversation.Message) ([]mongoMessageDocument, error) {
	documents := make([]mongoMessageDocument, 0, len(messages))
	for index, message := range messages {
		document := mongoMessageDocument{
			Role:    message.Role,
			Speaker: toMongoSpeaker(message.Speaker),
			ReplyTo: toMongoReplyContext(message.ReplyTo),
		}
		switch content := message.Content.(type) {
		case string:
			document.Content = mongoMessageContentDocument{Kind: "text", Text: content}
		case []providers.ChatContentPart:
			parts := make([]mongoContentPartDocument, 0, len(content))
			for _, part := range content {
				mongoPart := mongoContentPartDocument{Type: part.Type, Text: part.Text}
				if part.ImageURL != nil {
					mongoPart.ImageURL = &mongoImageURLDocument{URL: part.ImageURL.URL}
				}
				parts = append(parts, mongoPart)
			}
			document.Content = mongoMessageContentDocument{Kind: "parts", Parts: parts}
		default:
			return nil, fmt.Errorf("message %d has unsupported content type %T", index, message.Content)
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func fromMongoMessages(documents []mongoMessageDocument) ([]conversation.Message, error) {
	messages := make([]conversation.Message, 0, len(documents))
	for index, document := range documents {
		message := conversation.Message{
			Role:    document.Role,
			Speaker: fromMongoSpeaker(document.Speaker),
			ReplyTo: fromMongoReplyContext(document.ReplyTo),
		}
		switch document.Content.Kind {
		case "text":
			message.Content = document.Content.Text
		case "parts":
			parts := make([]providers.ChatContentPart, 0, len(document.Content.Parts))
			for _, part := range document.Content.Parts {
				providerPart := providers.ChatContentPart{Type: part.Type, Text: part.Text}
				if part.ImageURL != nil {
					providerPart.ImageURL = &providers.ChatImageURL{URL: part.ImageURL.URL}
				}
				parts = append(parts, providerPart)
			}
			message.Content = parts
		default:
			return nil, fmt.Errorf("message %d has unknown content kind %q", index, document.Content.Kind)
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func toMongoSpeaker(speaker *conversation.Speaker) *mongoSpeakerDocument {
	if speaker == nil {
		return nil
	}
	return &mongoSpeakerDocument{
		UserID: speaker.UserID, DisplayName: speaker.DisplayName, Username: speaker.Username,
	}
}

func fromMongoSpeaker(speaker *mongoSpeakerDocument) *conversation.Speaker {
	if speaker == nil {
		return nil
	}
	return &conversation.Speaker{
		UserID: speaker.UserID, DisplayName: speaker.DisplayName, Username: speaker.Username,
	}
}

func toMongoReplyContext(reply *conversation.ReplyContext) *mongoReplyContextDocument {
	if reply == nil {
		return nil
	}
	return &mongoReplyContextDocument{Speaker: toMongoSpeaker(reply.Speaker), Text: reply.Text}
}

func fromMongoReplyContext(reply *mongoReplyContextDocument) *conversation.ReplyContext {
	if reply == nil {
		return nil
	}
	return &conversation.ReplyContext{Speaker: fromMongoSpeaker(reply.Speaker), Text: reply.Text}
}

var _ Store = (*mongoStore)(nil)
