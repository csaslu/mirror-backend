package main

import (
	"mirror/internal/cli"
	"mirror/internal/cron"
	"mirror/internal/handler"
	"mirror/internal/infra/cache"
	"mirror/internal/infra/cacheproxy"
	"mirror/internal/infra/config"
	"mirror/internal/infra/database"
	"mirror/internal/infra/logger"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	// Embed IANA tz database so time.LoadLocation works in slim containers without system tzdata
	_ "time/tzdata"
)

func main() {
	// Static config first: every step below reads it.
	config.Init()
	defer config.Cleanup()

	logger.Init()
	defer logger.Cleanup()

	// A maintenance command (`-dump-cache-config`, `-drop-caches`) replaces the
	// server run entirely; it is executed below and the process then stops.
	command := cli.Parse()

	// Init database
	if command == nil || command.Requires(cli.NeedsDatabase) {
		database.Init()
		defer database.Cleanup()
	}

	// Init Redis-backed cache
	if command == nil || command.Requires(cli.NeedsCache) {
		cache.Init()
		defer cache.Cleanup()
	}

	if command != nil {
		if err := command.Run(); err != nil {
			logger.L.Fatal("maintenance command failed", zap.Error(err))
		}
		return
	}

	// Client for the caching proxy that fronts every mirror directory.
	cacheproxy.Init()

	// Background refresh of upstream sync status.
	cron.Init()

	// Run main server
	app := handler.Run()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	logger.L.Info("received shutdown signal", zap.Any("signal", sig))

	// Graceful shutdown with 5 second timeout
	if err := app.ShutdownWithTimeout(5 * time.Second); err != nil {
		logger.L.Error("error during graceful shutdown", zap.Error(err))
	}
}
