package logger

import (
	"io"
	"mirror/internal/infra/config"
	"os"
	"sync"

	"github.com/goccy/go-json"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// L is the global logger. It is always safe to use: before Init runs it is a
// no-op logger, so a stray log line can never panic with a nil dereference
// (Packages that log before Init, tests, or a reordered startup would).
var L = zap.NewNop()

var initOnce sync.Once

// ensureReady makes L usable without clobbering a logger set by Init. It is a
// safety net for tests and for packages imported before main starts logging.
func ensureReady() {
	initOnce.Do(func() {
		if L == nil {
			L = zap.NewNop()
		}
	})
}

func Init() {
	// Mark the one-time initialisation done so ensureReady stops substituting.
	initOnce.Do(func() {})

	// Set log level
	var level zapcore.Level
	if config.Debug {
		level = zap.DebugLevel
	} else {
		level = zap.InfoLevel
	}

	// Create config
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
		NewReflectedEncoder: func(w io.Writer) zapcore.ReflectedEncoder {
			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)
			return enc
		},
	}

	cores := make([]zapcore.Core, 0)

	stdoutCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.Lock(os.Stdout),
		level,
	)
	cores = append(cores, stdoutCore)

	// Output to log file
	if config.Get().Log.File.All != "" {
		// Normal log output
		fileWriter := &lumberjack.Logger{
			Filename:   config.Get().Log.File.All,
			MaxSize:    config.Get().Log.MaxSize,
			MaxBackups: config.Get().Log.MaxBackups,
			MaxAge:     config.Get().Log.MaxAge,
			Compress:   config.Get().Log.Compress,
			LocalTime:  true,
		}

		fileCore := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderConfig),
			zapcore.AddSync(fileWriter),
			level,
		)
		cores = append(cores, fileCore)
	}

	if config.Get().Log.File.Err != "" {
		// Error log output
		errWriter := &lumberjack.Logger{
			Filename:   config.Get().Log.File.Err,
			MaxSize:    config.Get().Log.MaxSize,
			MaxBackups: config.Get().Log.MaxBackups,
			MaxAge:     config.Get().Log.MaxAge,
			Compress:   config.Get().Log.Compress,
			LocalTime:  true,
		}
		errCore := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderConfig),
			zapcore.AddSync(errWriter),
			zapcore.WarnLevel, // Only record Warn、Error、Fatal、Panic
		)
		cores = append(cores, errCore)
	}

	// Create zap core
	core := zapcore.NewTee(cores...)

	// Create zap logger
	L = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zap.ErrorLevel))

	L.Debug("logger initialized")
}

func Cleanup() {
	if L != nil {
		_ = L.Sync()
	}
}
