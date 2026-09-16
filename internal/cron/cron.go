package cron

import (
	"mirror/internal/infra/logger"
	v1 "mirror/internal/service/v1"
	"time"

	"github.com/go-co-op/gocron"
	"go.uber.org/zap"
)

var C *gocron.Scheduler

// Init inits global cron object
func Init() {
	C = gocron.NewScheduler(time.Local)

	registerDefault()

	C.StartAsync()

	logger.L.Debug("cron initialized")
}

// registerDefault registers default cron tasks
func registerDefault() {
	if _, err := C.Every(1).Hours().Do(v1.RefreshStatus); err != nil {
		logger.L.Error("failed to register upstream status sync cron", zap.Error(err))
	}
}
