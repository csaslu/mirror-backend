package handler

import (
	"mirror/internal/infra/config"
	"mirror/internal/infra/logger"
	"mirror/internal/meta"
	"mirror/internal/middleware"
	"mirror/internal/model"
	"time"

	"github.com/go-playground/validator/v10"
	fiberzap "github.com/gofiber/contrib/v3/zap"
	"github.com/gofiber/fiber/v3"
	recoverer "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/gofiber/utils/v2"
	"go.gh.ink/json/v2"
	fiberip "go.gh.ink/toolbox/fiber/v3/ip"
	"go.gh.ink/toolbox/xfmt"
	"go.gh.ink/toolbox/xtype"
	"go.uber.org/zap"
)

// structValidator struct implementation
type structValidator struct {
	validate *validator.Validate
}

// Validate method implementation
func (v *structValidator) Validate(out any) error {
	return v.validate.Struct(out)
}

// fiberApp provides a GoFiber app
func fiberApp() *fiber.App {
	app := fiber.New(fiber.Config{
		JSONEncoder:     json.MarshalV1,
		JSONDecoder:     json.UnmarshalV1,
		ProxyHeader:     fiber.HeaderXForwardedFor,
		StructValidator: &structValidator{validate: validator.New()},
		ErrorHandler:    model.RespInternalServerError,
	})

	// Use recoverer
	app.Use(recoverer.New())

	// Use requestID middleware
	app.Use(requestid.New(requestid.Config{
		Next:      nil,
		Header:    fiber.HeaderXRequestID,
		Generator: utils.UUIDv4,
	}))

	// Use global logger
	app.Use(fiberzap.New(fiberzap.Config{
		Logger:   logger.L,
		SkipURIs: []string{"/ping"},
		Fields:   []string{"latency", "status", "method", "url", "requestId", "ua"},
		FieldsFunc: func(c fiber.Ctx) []zap.Field {
			return []zap.Field{
				zap.String("ip", fiberip.GetIP(c)),
			}
		},
	}))

	// Use customer header middleware
	app.Use(middleware.CustomHeader)

	// Ping test router handler
	app.All("/ping", func(c fiber.Ctx) error {
		return model.RespSuccess(c, struct {
			Msg   string  `json:"msg"`
			Stamp float64 `json:"stamp"`
		}{
			Msg:   "pong",
			Stamp: float64(time.Now().UnixNano()) / 1e9,
		})
	})

	// Root info router handler
	app.All("/", func(c fiber.Ctx) error {
		return model.RespSuccess(c, xtype.MS[string]{
			"powered_by":   meta.PoweredByText,
			"tech_support": "Communication and Software Association of Shanghai Lida University",
		})
	})

	// Register global routes
	Register(app)

	// Not found router handler
	app.Use(func(c fiber.Ctx) error {
		return model.RespNotFound(c)
	})

	return app
}

// Run runs an HTTP server in a goroutine
func Run() *fiber.App {
	// Create GoFiber app
	app := fiberApp()

	addr := xfmt.Sprintf("%s:%d", config.Get().Server.Host, config.Get().Server.Port)

	// Start HTTP server with GoFiber native listener in a goroutine.
	// Retry for a while on startup failure (e.g. the previous instance is
	// still releasing the port during a restart), then exit with a non-zero
	// code so process managers can detect the failed startup.
	const (
		listenRetryLimit    = 10
		listenRetryInterval = time.Second
	)
	go func() {
		for attempt := 1; ; attempt++ {
			err := app.Listen(addr, fiber.ListenConfig{
				DisableStartupMessage: true,
			})
			if err == nil {
				// Listener closed by graceful shutdown
				return
			}
			if attempt >= listenRetryLimit {
				logger.L.Fatal("Failed to start HTTP server", zap.Error(err), zap.Int("attempts", attempt))
			}
			logger.L.Error("Failed to start HTTP server, retrying", zap.Error(err), zap.Int("attempt", attempt))
			time.Sleep(listenRetryInterval)
		}
	}()

	if config.Debug {
		host := config.Get().Server.Host
		if host == "" {
			host = "[::]"
		}
		visit := host
		if host == "[::]" || host == "0.0.0.0" {
			visit = "localhost"
		}

		logger.L.Info(xfmt.Sprintf("Server is running on %s:%d", host, config.Get().Server.Port))
		logger.L.Debug(xfmt.Sprintf("Visit by %s:%d", visit, config.Get().Server.Port))
	}

	return app
}
