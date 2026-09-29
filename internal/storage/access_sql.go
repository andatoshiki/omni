package storage

import (
	"database/sql"
	"fmt"
)

func updateTelegramUsernameOwner(
	tx *sql.Tx,
	query string,
	userID int64,
	username, normalizedUsername string,
) (any, any, error) {
	if normalizedUsername == "" {
		return nil, nil, nil
	}
	if _, err := tx.Exec(query, normalizedUsername, userID); err != nil {
		return nil, nil, fmt.Errorf("failed to release telegram username: %w", err)
	}
	return username, normalizedUsername, nil
}
