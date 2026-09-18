package config

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/viper"
)

var config StaticConfig
var configRWMutex sync.RWMutex
var Debug = false
var signalStopChan chan struct{}

// defaultsFor returns the fallback values for keys a deployment may omit.
// They are registered as viper defaults (which explicit config values
// override), so an empty `cache:` block cannot silently point Redis at port 0.
func defaultsFor(cfg *viper.Viper) {
	cfg.SetDefault("server.host", "0.0.0.0")
	cfg.SetDefault("server.port", 8000)
	cfg.SetDefault("database.host", "127.0.0.1")
	cfg.SetDefault("database.port", 5432)
	cfg.SetDefault("cache.host", "127.0.0.1")
	cfg.SetDefault("cache.port", 6379)
	cfg.SetDefault("web.dir", "web")
	cfg.SetDefault("mirror.cors_allow_origins", []string{})
	cfg.SetDefault("mirror.cache_dir", "./cache_data")
	cfg.SetDefault("mirror.cache_max_size", "200GB")
}

// load is constructor of static config
func load() (*viper.Viper, error) {
	// Init viper
	cfg := viper.New()

	// Set config type
	cfg.SetConfigType("yaml")

	// Set config path
	cfg.AddConfigPath("./")

	// Set config file
	cfg.SetConfigName("config")

	// Register fallbacks first: viper only uses a default when the key is
	// absent from the config file.
	defaultsFor(cfg)

	// Read the config file
	if err := cfg.ReadInConfig(); err != nil {
		return nil, err
	}

	// Is debug mode?
	if _, err := os.Stat("config_debug.yaml"); err == nil {
		// Init config file
		cfg.SetConfigName("config_debug")

		// Set debug status
		Debug = true

		// Read the debug config file
		if err = cfg.ReadInConfig(); err != nil {
			return nil, err
		}
	}

	// Unmarshal config
	if err := cfg.Unmarshal(&config); err != nil {
		return nil, err
	}

	return cfg, nil
}

// reload static config
func reload() {
	configRWMutex.Lock()
	defer configRWMutex.Unlock()

	if _, err := load(); err != nil {
		log.Println("reload static config failed:", err)
	}
}

// Init loads static config
func Init() {
	configRWMutex.Lock()
	defer configRWMutex.Unlock()

	if _, err := load(); err != nil {
		log.Fatal("load static config failed:", err)
	}

	// Prepare a channel to receive signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP)

	// Create stop channel
	signalStopChan = make(chan struct{})

	// Start a goroutine to listen for signals
	go func() {
		for {
			select {
			case sig := <-sigChan:
				if sig == syscall.SIGHUP {
					reload()
				}
			case <-signalStopChan:
				signal.Stop(sigChan)
				return
			}
		}
	}()
}

// Cleanup stops the signal listening goroutine
func Cleanup() {
	if signalStopChan != nil {
		close(signalStopChan)
	}
}

// Get returns static config
func Get() StaticConfig {
	configRWMutex.RLock()
	defer configRWMutex.RUnlock()

	return config
}
