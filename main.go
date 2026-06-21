package main

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"

	botapp "github.com/andatoshiki/omni/internal/bot"
	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/logging"
	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/storage"
)

func main() {
	logger := logging.ConfigureDefault()
	logger.Info("bot starting")

	var params config.Params
	if err := params.Init(); err != nil {
		logger.Error("failed to initialize configuration", "error", err)
		os.Exit(1)
	}

	registry, err := providers.NewRegistry(params.Providers)
	if err != nil {
		logger.Error("failed to initialize providers", "error", err)
		os.Exit(1)
	}
	logger.Info("providers loaded", "count", registry.Len(), "providers", strings.Join(registry.ProviderNames(), ","))

	store, err := storage.Open(params.Database)
	if err != nil {
		logger.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Warn("failed to close database", "error", err)
		}
	}()

	app, err := botapp.New(&params, store, registry, logger)
	if err != nil {
		logger.Error("failed to initialize telegram bot", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	app.Run(ctx)
}
