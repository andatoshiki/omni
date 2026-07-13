package storage

import "fmt"

func (db *postgresStore) SaveTranscriptMessage(message TranscriptMessage) error {
	message, err := validateTranscriptMessage(message)
	if err != nil {
		return err
	}

	_, err = db.conn.Exec(`
		INSERT INTO summary_transcript (
			chat_id, thread_id, message_id, role, sender_name, message_text
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT(chat_id, message_id) DO UPDATE SET
			thread_id = EXCLUDED.thread_id,
			role = EXCLUDED.role,
			sender_name = EXCLUDED.sender_name,
			message_text = EXCLUDED.message_text
	`, message.ChatID, message.ThreadID, message.MessageID, message.Role, message.Sender, message.Text)
	if err != nil {
		return fmt.Errorf("failed to save summary transcript message: %w", err)
	}

	_, err = db.conn.Exec(`
		DELETE FROM summary_transcript
		WHERE chat_id = $1 AND thread_id = $2 AND message_id NOT IN (
			SELECT message_id FROM summary_transcript
			WHERE chat_id = $1 AND thread_id = $2
			ORDER BY message_id DESC
			LIMIT $3
		)
	`, message.ChatID, message.ThreadID, transcriptRetentionMessages)
	if err != nil {
		return fmt.Errorf("failed to prune summary transcript: %w", err)
	}
	return nil
}

func (db *postgresStore) RecentTranscriptMessages(chatID int64, threadID, beforeMessageID, limit int) ([]TranscriptMessage, error) {
	limit = normalizeTranscriptLimit(limit)
	if limit == 0 {
		return []TranscriptMessage{}, nil
	}

	rows, err := db.conn.Query(`
		SELECT chat_id, thread_id, message_id, role, sender_name, message_text
		FROM summary_transcript
		WHERE chat_id = $1 AND thread_id = $2 AND message_id < $3
		ORDER BY message_id DESC
		LIMIT $4
	`, chatID, threadID, beforeMessageID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to load summary transcript: %w", err)
	}
	defer rows.Close()

	messages := make([]TranscriptMessage, 0, limit)
	for rows.Next() {
		var message TranscriptMessage
		if err := rows.Scan(&message.ChatID, &message.ThreadID, &message.MessageID, &message.Role, &message.Sender, &message.Text); err != nil {
			return nil, fmt.Errorf("failed to scan summary transcript message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate summary transcript: %w", err)
	}
	reverseTranscriptMessages(messages)
	return messages, nil
}
