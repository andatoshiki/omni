package bot

import (
	"context"
	"sync"
	"time"

	"github.com/go-telegram/bot/models"
)

const mediaGroupDebounceDelay = 1000 * time.Millisecond

type mediaGroup struct {
	timer    *time.Timer
	messages []*models.Message
}

// Aggregator buffers incoming Telegram messages that share a MediaGroupID,
// waiting until no new messages arrive for a debounce period before dispatching
// them together as a single batch.
type Aggregator struct {
	mu     sync.Mutex
	groups map[string]*mediaGroup
	app    *App
}

// NewAggregator initializes a thread-safe message aggregator.
func NewAggregator(app *App) *Aggregator {
	return &Aggregator{
		groups: make(map[string]*mediaGroup),
		app:    app,
	}
}

// Add buffers a message. If the timer for its MediaGroupID expires, it flushes
// the group to the application's processMessages pipeline.
func (a *Aggregator) Add(ctx context.Context, msg *models.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()

	groupID := msg.MediaGroupID
	if groupID == "" {
		// Should not happen, but safeguard
		a.app.processMessages(ctx, msg)
		return
	}

	group, exists := a.groups[groupID]
	if !exists {
		group = &mediaGroup{}
		a.groups[groupID] = group

		group.timer = time.AfterFunc(mediaGroupDebounceDelay, func() {
			a.mu.Lock()
			g, ok := a.groups[groupID]
			if ok {
				delete(a.groups, groupID)
			}
			a.mu.Unlock()

			if ok && len(g.messages) > 0 {
				a.app.processMessages(ctx, g.messages...)
			}
		})
	} else {
		group.timer.Reset(mediaGroupDebounceDelay)
	}

	group.messages = append(group.messages, msg)
}
