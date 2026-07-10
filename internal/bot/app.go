package bot

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	telegram "github.com/go-telegram/bot"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/storage"
)

// streamPreviewLimit leaves room for Telegram HTML expansion below its
// 4096-character message limit.
const streamPreviewLimit = 3500

var pollingAllowedUpdates = telegram.AllowedUpdates{"message", "callback_query"}

// App owns the bot's runtime dependencies and Telegram handlers.
type App struct {
	client          *telegram.Bot
	params          *config.Params
	store           storage.Store
	providers       *providers.Registry
	logger          *slog.Logger
	commands        *CommandHandler
	mediaAggregator *Aggregator
	botUsername     string
}

func New(
	params *config.Params,
	store storage.Store,
	registry *providers.Registry,
	logger *slog.Logger,
) (*App, error) {
	if params == nil {
		return nil, errors.New("configuration is required")
	}
	if store == nil {
		return nil, errors.New("database is required")
	}
	if registry == nil || registry.Len() == 0 {
		return nil, errors.New("at least one provider is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	app := &App{
		params:    params,
		store:     store,
		providers: registry,
		logger:    logger,
	}
	client, err := telegram.New(
		params.BotToken,
		telegram.WithDefaultHandler(app.updateHandler),
		telegram.WithAllowedUpdates(pollingAllowedUpdates),
		telegram.WithErrorsHandler(func(err error) {
			logger.Error("telegram polling error", "error", err)
		}),
	)
	if err != nil {
		return nil, err
	}
	app.client = client
	app.commands = NewCommandHandler(app)
	app.mediaAggregator = NewAggregator(app)
	return app, nil
}

// Run registers commands, notifies admins, and serves updates until ctx ends.
func (a *App) Run(ctx context.Context) {
	botUser, err := a.client.GetMe(ctx)
	if err != nil {
		a.logger.Error("failed to get bot identity", "error", err)
		return
	}
	a.botUsername = strings.TrimPrefix(strings.TrimSpace(botUser.Username), "@")
	if a.botUsername == "" {
		a.logger.Error("failed to get bot identity: username is empty")
		return
	}

	a.preparePolling(ctx)
	a.registerCommands(ctx)
	a.logger.Info(
		"telegram polling starting",
		"bot_username", a.botUsername,
		"bot_id", a.client.ID(),
		"allowed_updates", strings.Join([]string(pollingAllowedUpdates), ","),
		"allowed_user_count", len(a.params.AllowedUserIDs),
		"allowed_group_count", len(a.params.AllowedGroupIDs),
	)
	a.client.Start(ctx)
	a.logger.Info("bot stopped")
}
