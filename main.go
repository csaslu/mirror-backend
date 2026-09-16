package main

import (
	"fmt"
	"mirror/internal/adapter"
	"mirror/internal/cron"
	"mirror/internal/handler"
	"mirror/internal/infra/cache"
	"mirror/internal/infra/config"
	"mirror/internal/infra/database"
	"mirror/internal/infra/logger"
	"os"
	"os/signal"
	"syscall"
	"time"
	// Embed IANA tz database so time.LoadLocation works in slim containers without system tzdata
	_ "time/tzdata"

	"go.gh.ink/toolbox/pointer"
	"go.uber.org/zap"
)

func main() {
	// Load static config
	config.Init()
	defer config.Cleanup()

	// Init logger
	logger.Init()
	defer logger.Cleanup()

	r, e := adapter.ParseStatus("https://pypi.mirrors.tuna.tsinghua.edu.cn", "pypi")
	fmt.Println(e)
	fmt.Println(pointer.SafeDeref(r.Status), pointer.SafeDeref(r.Size), pointer.SafeDeref(r.LastUpdate))
	syscall.Exit(0)

	// Init cache
	cache.Init()
	defer cache.Cleanup()

	// Init database
	database.Init()
	defer database.Cleanup()

	// Init cron
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
