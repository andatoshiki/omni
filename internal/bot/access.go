package bot

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/storage"
)

type accessMutation struct {
	User    storage.TelegramUser
	Changed bool
}

type accessManager struct {
	mu         sync.RWMutex
	mutationMu sync.Mutex
	store      storage.Store
	logger     *slog.Logger
	adminRef   config.TelegramUserReference
	adminID    int64
	users      map[int64]storage.TelegramUser
	usernames  map[string]int64
	allowedIDs map[int64]struct{}
}

func newAccessManager(store storage.Store, adminRef config.TelegramUserReference, logger *slog.Logger) (*accessManager, error) {
	if adminRef.IsZero() {
		return nil, fmt.Errorf("telegram administrator is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	users, err := store.LoadTelegramUsers()
	if err != nil {
		return nil, fmt.Errorf("load telegram access state: %w", err)
	}
	manager := &accessManager{
		store:      store,
		logger:     logger,
		adminRef:   adminRef,
		users:      make(map[int64]storage.TelegramUser, len(users)),
		usernames:  make(map[string]int64, len(users)),
		allowedIDs: make(map[int64]struct{}, len(users)),
	}
	for _, user := range users {
		manager.users[user.UserID] = user
		if user.NormalizedUsername != "" {
			manager.usernames[user.NormalizedUsername] = user.UserID
		}
		if user.Allowed {
			manager.allowedIDs[user.UserID] = struct{}{}
		}
	}
	if adminRef.ID > 0 {
		manager.adminID = adminRef.ID
	} else {
		manager.adminID = manager.usernames[adminRef.Username]
	}
	if manager.adminID > 0 {
		logger.Info("telegram administrator resolved", "admin_user", adminRef.String(), "admin_user_id", manager.adminID)
	} else {
		logger.Warn("telegram administrator awaiting first observed update", "admin_user", adminRef.String())
	}
	return manager, nil
}

func (m *accessManager) observe(user *models.User) error {
	if user == nil || user.ID <= 0 {
		return nil
	}
	normalizedUsername := ""
	if user.Username != "" {
		var err error
		normalizedUsername, err = config.NormalizeTelegramUsername("@" + user.Username)
		if err != nil {
			return fmt.Errorf("normalize observed telegram username: %w", err)
		}
	}

	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	m.mu.RLock()
	current, exists := m.users[user.ID]
	unchanged := exists && current.Username == user.Username && current.NormalizedUsername == normalizedUsername
	m.mu.RUnlock()
	if unchanged {
		return nil
	}

	if err := m.store.ObserveTelegramUser(user.ID, user.Username, normalizedUsername); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if current.NormalizedUsername != "" {
		delete(m.usernames, current.NormalizedUsername)
	}
	if previousID, ok := m.usernames[normalizedUsername]; normalizedUsername != "" && ok && previousID != user.ID {
		previous := m.users[previousID]
		previous.Username = ""
		previous.NormalizedUsername = ""
		m.users[previousID] = previous
	}
	current.UserID = user.ID
	current.Username = user.Username
	current.NormalizedUsername = normalizedUsername
	m.users[user.ID] = current
	if normalizedUsername != "" {
		m.usernames[normalizedUsername] = user.ID
	}
	if m.adminID == 0 && m.adminRef.Username != "" && normalizedUsername == m.adminRef.Username {
		m.adminID = user.ID
		m.logger.Info("telegram administrator bootstrapped", "admin_user", m.adminRef.String(), "admin_user_id", user.ID)
	}
	return nil
}

func (m *accessManager) isAdmin(userID int64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return userID > 0 && userID == m.adminID
}

func (m *accessManager) isAuthorized(userID int64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if userID <= 0 {
		return false
	}
	if userID == m.adminID {
		return true
	}
	_, allowed := m.allowedIDs[userID]
	return allowed
}

func (m *accessManager) allowedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.allowedIDs)
}

func (m *accessManager) add(reference string) (accessMutation, error) {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	m.mu.RLock()
	user, err := m.resolveLocked(reference)
	if err != nil {
		m.mu.RUnlock()
		return accessMutation{}, err
	}
	if user.UserID == m.adminID {
		m.mu.RUnlock()
		return accessMutation{User: user}, nil
	}
	if _, allowed := m.allowedIDs[user.UserID]; allowed {
		m.mu.RUnlock()
		return accessMutation{User: user}, nil
	}
	m.mu.RUnlock()

	if err := m.store.SetTelegramUserAllowed(user.UserID, true); err != nil {
		return accessMutation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	user.Allowed = true
	m.users[user.UserID] = user
	m.allowedIDs[user.UserID] = struct{}{}
	return accessMutation{User: user, Changed: true}, nil
}

func (m *accessManager) remove(reference string) (accessMutation, error) {
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	m.mu.RLock()
	user, err := m.resolveLocked(reference)
	if err != nil {
		m.mu.RUnlock()
		return accessMutation{}, err
	}
	if user.UserID == m.adminID {
		m.mu.RUnlock()
		return accessMutation{}, fmt.Errorf("the configured administrator cannot be removed")
	}
	if _, allowed := m.allowedIDs[user.UserID]; !allowed {
		m.mu.RUnlock()
		return accessMutation{User: user}, nil
	}
	m.mu.RUnlock()

	if err := m.store.SetTelegramUserAllowed(user.UserID, false); err != nil {
		return accessMutation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	user.Allowed = false
	m.users[user.UserID] = user
	delete(m.allowedIDs, user.UserID)
	return accessMutation{User: user, Changed: true}, nil
}

func (m *accessManager) resolveLocked(reference string) (storage.TelegramUser, error) {
	parsed, err := config.ParseTelegramUserReference(reference)
	if err != nil {
		return storage.TelegramUser{}, err
	}
	if parsed.ID > 0 {
		if user, ok := m.users[parsed.ID]; ok {
			return user, nil
		}
		return storage.TelegramUser{UserID: parsed.ID}, nil
	}
	userID, ok := m.usernames[parsed.Username]
	if !ok {
		return storage.TelegramUser{}, fmt.Errorf("unknown username @%s; have the user message the bot or use their numeric user ID", parsed.Username)
	}
	return m.users[userID], nil
}
