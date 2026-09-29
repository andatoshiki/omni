package storage

import (
	"database/sql"
	"fmt"
)

func (db *postgresStore) LoadTelegramUsers() ([]TelegramUser, error) {
	rows, err := db.conn.Query(`SELECT user_id, username, normalized_username, allowed FROM telegram_users`)
	if err != nil {
		return nil, fmt.Errorf("failed to load telegram users: %w", err)
	}
	defer rows.Close()

	var users []TelegramUser
	for rows.Next() {
		var user TelegramUser
		var username, normalizedUsername sql.NullString
		if err := rows.Scan(&user.UserID, &username, &normalizedUsername, &user.Allowed); err != nil {
			return nil, fmt.Errorf("failed to scan telegram user: %w", err)
		}
		user.Username = username.String
		user.NormalizedUsername = normalizedUsername.String
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate telegram users: %w", err)
	}
	return users, nil
}

func (db *postgresStore) ObserveTelegramUser(userID int64, username, normalizedUsername string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin telegram user update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	usernameValue, normalizedValue, err := updateTelegramUsernameOwner(
		tx,
		`UPDATE telegram_users SET username = NULL, normalized_username = NULL, updated_at = CURRENT_TIMESTAMP WHERE normalized_username = $1 AND user_id <> $2`,
		userID,
		username,
		normalizedUsername,
	)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO telegram_users (user_id, username, normalized_username, allowed)
		VALUES ($1, $2, $3, FALSE)
		ON CONFLICT (user_id) DO UPDATE SET
			username = EXCLUDED.username,
			normalized_username = EXCLUDED.normalized_username,
			updated_at = CURRENT_TIMESTAMP
	`, userID, usernameValue, normalizedValue); err != nil {
		return fmt.Errorf("failed to observe telegram user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit telegram user update: %w", err)
	}
	return nil
}

func (db *postgresStore) SetTelegramUserAllowed(userID int64, allowed bool) error {
	_, err := db.conn.Exec(`
		INSERT INTO telegram_users (user_id, allowed)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET
			allowed = EXCLUDED.allowed,
			updated_at = CURRENT_TIMESTAMP
	`, userID, allowed)
	if err != nil {
		return fmt.Errorf("failed to update telegram user access: %w", err)
	}
	return nil
}
