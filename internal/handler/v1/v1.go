package v1

import (
	v1 "mirror/internal/controller/v1"

	"github.com/gofiber/fiber/v3"
)

func Route(group fiber.Router) {
	group.Get("/mirrors.json", v1.MirrorList)
}
