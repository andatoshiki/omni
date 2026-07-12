package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	botapp "github.com/andatoshiki/omni/internal/bot"
	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/logging"
	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/storage"
	"github.com/andatoshiki/omni/internal/update"
	"github.com/andatoshiki/omni/internal/version"
)

func main() {
	// Top-level flags — work standalone or alongside any subcommand.
	for _, a := range os.Args[1:] {
		switch a {
		case "--help", "-h":
			update.PrintHelp()
			os.Exit(0)
		case "--version", "-v":
			fmt.Printf("omni %s\ncommit: %s\nbuilt:  %s\ngo:    %s\n",
				version.Version, version.Commit, version.BuildTime, version.GoVersion())
			os.Exit(0)
		}
	}

	// Subcommand dispatch.
	if len(os.Args) > 1 && os.Args[1] == "update" {
		if err := update.Run(context.Background(), nil); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

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
