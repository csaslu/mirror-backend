package logger

import (
	"io"
	"mirror/internal/infra/config"
	"os"

	"github.com/goccy/go-json"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var L *zap.Logger

func Init() {
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
	_ = L.Sync()
}
