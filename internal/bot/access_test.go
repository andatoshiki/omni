package bot

import (
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/storage"
)

func newTestAccessManager(allowedUserIDs []int64, adminID int64) *accessManager {
	manager := &accessManager{
		logger:     slog.Default(),
		adminID:    adminID,
		users:      make(map[int64]storage.TelegramUser),
		usernames:  make(map[string]int64),
		allowedIDs: make(map[int64]struct{}),
	}
	for _, userID := range allowedUserIDs {
		manager.users[userID] = storage.TelegramUser{UserID: userID, Allowed: true}
		manager.allowedIDs[userID] = struct{}{}
	}
	return manager
}

func TestAccessManagerBootstrapsAndResolvesHandleOnRestart(t *testing.T) {
	store := newAccessTestStore()
	adminRef := config.TelegramUserReference{Username: "andatoshiki"}
	manager, err := newAccessManager(store, adminRef, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if manager.isAdmin(42) {
		t.Fatal("unknown admin handle resolved before observation")
	}
	if err := manager.observe(&models.User{ID: 42, Username: "AndaToshiki"}); err != nil {
		t.Fatal(err)
	}
	if !manager.isAdmin(42) || !manager.isAuthorized(42) {
		t.Fatal("matching observed handle did not bootstrap administrator")
	}

	restarted, err := newAccessManager(store, adminRef, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.isAdmin(42) {
		t.Fatal("admin handle was not resolved from persisted observations")
	}
}

func TestAccessManagerMovesReassignedUsernameWithoutMovingAccess(t *testing.T) {
	store := newAccessTestStore()
	store.users[1] = storage.TelegramUser{
		UserID: 1, Username: "Alice", NormalizedUsername: "alice", Allowed: true,
	}
	manager, err := newAccessManager(store, config.TelegramUserReference{ID: 99}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.observe(&models.User{ID: 2, Username: "Alice"}); err != nil {
		t.Fatal(err)
	}
	mutation, err := manager.add("@alice")
	if err != nil {
		t.Fatal(err)
	}
	if mutation.User.UserID != 2 || !mutation.Changed {
		t.Fatalf("add by reassigned username = %#v", mutation)
	}
	if !manager.isAuthorized(1) || !manager.isAuthorized(2) {
		t.Fatal("username reassignment changed immutable-ID access unexpectedly")
	}
}

func TestAccessManagerReResolvesReassignedAdminHandleOnlyOnRestart(t *testing.T) {
	store := newAccessTestStore()
	store.users[1] = storage.TelegramUser{
		UserID: 1, Username: "Admin", NormalizedUsername: "admin",
	}
	adminRef := config.TelegramUserReference{Username: "admin"}
	manager, err := newAccessManager(store, adminRef, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if !manager.isAdmin(1) {
		t.Fatal("initial administrator handle did not resolve")
	}
	if err := manager.observe(&models.User{ID: 2, Username: "Admin"}); err != nil {
		t.Fatal(err)
	}
	if !manager.isAdmin(1) || manager.isAdmin(2) {
		t.Fatal("administrator changed before restart")
	}

	restarted, err := newAccessManager(store, adminRef, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if restarted.isAdmin(1) || !restarted.isAdmin(2) {
		t.Fatal("administrator handle was not re-resolved after restart")
	}
}

func TestAccessManagerNumericMutationIsIdempotentAndProtectsAdmin(t *testing.T) {
	store := newAccessTestStore()
	manager, err := newAccessManager(store, config.TelegramUserReference{ID: 99}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.add("42")
	if err != nil || !first.Changed || first.User.UserID != 42 {
		t.Fatalf("first add = %#v, %v", first, err)
	}
	second, err := manager.add("42")
	if err != nil || second.Changed {
		t.Fatalf("second add = %#v, %v", second, err)
	}
	removed, err := manager.remove("42")
	if err != nil || !removed.Changed || manager.isAuthorized(42) {
		t.Fatalf("remove = %#v, %v", removed, err)
	}
	if _, err := manager.remove("99"); err == nil {
		t.Fatal("configured administrator was removable")
	}
}

func TestAccessManagerConcurrentReadsAndMutations(t *testing.T) {
	store := newAccessTestStore()
	manager, err := newAccessManager(store, config.TelegramUserReference{ID: 99}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		userID := int64(index + 1)
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _ = manager.add(stringID(userID))
			_ = manager.isAuthorized(userID)
			_, _ = manager.remove(stringID(userID))
		}()
	}
	wait.Wait()
}

func TestAccessManagerKeepsSafeStateWhenPersistenceFails(t *testing.T) {
	store := newAccessTestStore()
	manager, err := newAccessManager(store, config.TelegramUserReference{Username: "andatoshiki"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	store.observeErr = errors.New("write failed")
	if err := manager.observe(&models.User{ID: 99, Username: "andatoshiki"}); err == nil {
		t.Fatal("observation failure was not returned")
	}
	if manager.isAdmin(99) {
		t.Fatal("failed observation bootstrapped administrator")
	}
	store.observeErr = nil
	store.allowedErr = errors.New("write failed")
	if _, err := manager.add("42"); err == nil {
		t.Fatal("access persistence failure was not returned")
	}
	if manager.isAuthorized(42) {
		t.Fatal("failed access persistence changed in-memory authorization")
	}
}

func TestAccessManagerAuthorizationReadsContinueDuringPersistence(t *testing.T) {
	store := newAccessTestStore()
	store.users[7] = storage.TelegramUser{UserID: 7, Allowed: true}
	store.observeStarted = make(chan struct{})
	store.observeRelease = make(chan struct{})
	manager, err := newAccessManager(store, config.TelegramUserReference{ID: 99}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	observeDone := make(chan error, 1)
	go func() {
		observeDone <- manager.observe(&models.User{ID: 42, Username: "alice"})
	}()
	<-store.observeStarted

	readDone := make(chan bool, 1)
	go func() {
		readDone <- manager.isAuthorized(7)
	}()
	select {
	case authorized := <-readDone:
		if !authorized {
			t.Fatal("allowed user became unauthorized during persistence")
		}
	case <-time.After(250 * time.Millisecond):
		close(store.observeRelease)
		<-observeDone
		t.Fatal("authorization read blocked on access persistence")
	}

	close(store.observeRelease)
	if err := <-observeDone; err != nil {
		t.Fatal(err)
	}
}

func TestAccessManagerLoadFailureAbortsInitialization(t *testing.T) {
	store := newAccessTestStore()
	store.loadErr = errors.New("read failed")
	if _, err := newAccessManager(store, config.TelegramUserReference{ID: 99}, slog.Default()); err == nil {
		t.Fatal("access manager initialized after load failure")
	}
}

func TestAccessManagerRequiresAdministrator(t *testing.T) {
	if _, err := newAccessManager(newAccessTestStore(), config.TelegramUserReference{}, slog.Default()); err == nil {
		t.Fatal("access manager initialized without an administrator")
	}
}

type accessTestStore struct {
	storage.Store
	mu             sync.Mutex
	users          map[int64]storage.TelegramUser
	loadErr        error
	observeErr     error
	allowedErr     error
	observeStarted chan struct{}
	observeRelease chan struct{}
}

func newAccessTestStore() *accessTestStore {
	return &accessTestStore{users: make(map[int64]storage.TelegramUser)}
}

func (s *accessTestStore) LoadTelegramUsers() ([]storage.TelegramUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	users := make([]storage.TelegramUser, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	return users, nil
}

func (s *accessTestStore) ObserveTelegramUser(userID int64, username, normalizedUsername string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.observeStarted != nil {
		close(s.observeStarted)
		<-s.observeRelease
	}
	if s.observeErr != nil {
		return s.observeErr
	}
	for id, user := range s.users {
		if id != userID && normalizedUsername != "" && user.NormalizedUsername == normalizedUsername {
			user.Username = ""
			user.NormalizedUsername = ""
			s.users[id] = user
		}
	}
	user := s.users[userID]
	user.UserID = userID
	user.Username = username
	user.NormalizedUsername = normalizedUsername
	s.users[userID] = user
	return nil
}

func (s *accessTestStore) SetTelegramUserAllowed(userID int64, allowed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.allowedErr != nil {
		return s.allowedErr
	}
	user := s.users[userID]
	user.UserID = userID
	user.Allowed = allowed
	s.users[userID] = user
	return nil
}

func stringID(userID int64) string {
	return strconv.FormatInt(userID, 10)
}
