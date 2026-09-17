// Package cacheproxy owns the process-wide connection to the httpcached instance.
//
// It exists so that handlers and services do not each build their own client
// (and so the "is the cache configured?" question has exactly one answer).
package cacheproxy

import (
	"sync"

	"mirror/internal/cacheclient"
	"mirror/internal/infra/config"
	"mirror/internal/infra/logger"
	"mirror/internal/meta"

	"go.uber.org/zap"
)

var (
	mutex  sync.RWMutex
	client *cacheclient.Client
)

// Init builds the shared cache client from the static config. It is safe to
// call when no cache is configured: the client stays nil and callers report
// the feature as unavailable.
func Init() {
	cfg := config.Get().Mirror

	if !cfg.Proxy {
		logger.L.Info("mirror proxy disabled (mirror.proxy is false)")
		return
	}

	if cfg.CacheAddr == "" {
		logger.L.Warn("mirror proxy enabled but mirror.cache_addr is empty; browsing and proxying stay disabled")
		return
	}

	built, err := cacheclient.New(cacheclient.Config{
		Addr:      cfg.CacheAddr,
		Host:      cfg.CacheHost,
		Scheme:    cfg.CacheScheme,
		UserAgent: meta.UserAgent,
	})
	if err != nil {
		logger.L.Error("failed to build the caching proxy client", zap.Error(err))
		return
	}

	mutex.Lock()
	client = built
	mutex.Unlock()

	logger.L.Info("caching proxy client ready", zap.String("addr", cfg.CacheAddr))
}

// Client returns the shared cache client, or nil when unavailable.
func Client() *cacheclient.Client {
	mutex.RLock()
	defer mutex.RUnlock()

	return client
}

// Enabled reports whether mirror content can be served.
func Enabled() bool {
	return Client() != nil
}
