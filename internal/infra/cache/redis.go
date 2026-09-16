package cache

import (
	"context"
	"mirror/internal/infra/config"
	"mirror/internal/infra/logger"
	"mirror/internal/meta"
	"mirror/internal/repository"
	"mirror/internal/service"

	"github.com/redis/go-redis/v9"
	"go.gh.ink/cask"
	"go.gh.ink/toolbox/xfmt"
	"go.uber.org/zap"

	_ "go.gh.ink/cask/redis"
)

var C *cask.Namespace
var redisClient *redis.Client

type RedisLogger struct{}

func (RedisLogger) Printf(_ context.Context, format string, v ...any) {
	logger.L.Debug(xfmt.Sprintf(format, v...), zap.String("component", "redis"))
}

func Init() {
	redis.SetLogger(RedisLogger{})

	redisClient = redis.NewClient(&redis.Options{
		Addr: xfmt.Sprintf(
			"%s:%d",
			config.Get().Cache.Host,
			config.Get().Cache.Port,
		),
		Password: config.Get().Cache.Pass,
		DB:       config.Get().Cache.DB,
	})

	caskClient, err := cask.New(redisClient, meta.InstanceName)
	if err != nil {
		logger.L.Fatal("failed to init cask client", zap.Error(err))
	}
	C = caskClient

	logger.L.Debug("redis initialized")

	// Init namespace
	repository.C = C.Namespace("database")
	service.C = C.Namespace("service")
}

func Cleanup() {
	_ = redisClient.Close()
}
