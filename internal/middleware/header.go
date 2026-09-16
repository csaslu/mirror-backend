package middleware

import (
	"mirror/internal/meta"

	"github.com/gofiber/fiber/v3"
)

// CustomHeader sets custom header
func CustomHeader(c fiber.Ctx) error {
	c.Set("X-Powered-By", meta.PoweredByText)
	c.Set("X-Tech-Support", "Communication and Software Association of Shanghai Lida University")

	return c.Next()
}
