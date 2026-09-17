package v1

import (
	v1 "mirror/internal/controller/v1"

	"github.com/gofiber/fiber/v3"
)

func Route(group fiber.Router) {
	group.Get("/mirrors.json", v1.MirrorList)
	group.Get("/mirrors/:id/status.json", v1.MirrorStatus)

	// Directory listings for the browser: the key is the first path segment and
	// everything after it is the directory inside that mirror. Root, one level
	// and arbitrary depth are registered separately because GoFiber matches a
	// wildcard to at least one segment.
	group.Get("/list/:key/mirrors.json", v1.MirrorListing)
	group.Get("/list/:key/*/mirrors.json", v1.MirrorListing)
}
