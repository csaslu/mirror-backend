package handler

import (
	v1 "mirror/internal/handler/v1"

	"github.com/gofiber/fiber/v3"
)

// Register global routes
func Register(app *fiber.App) {
	v1.Route(app.Group("/api/v1"))
}
