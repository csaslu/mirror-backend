package v1

import (
	"errors"
	"net/http"
	"strings"

	"mirror/internal/cacheclient"
	"mirror/internal/infra/cacheproxy"
	"mirror/internal/model"
	"mirror/internal/service/v1"

	"github.com/gofiber/fiber/v3"
)

// MirrorListing handles
//
//	GET /api/v1/list/:key/mirrors.json
//	GET /api/v1/list/:key/<sub/dir>/mirrors.json
//
// It answers with the directory listing of a mirror folder, read through the
// caching proxy and parsed into a neutral shape. When the upstream does not
// return a parseable listing (a file, listing disabled, an unknown format) the
// endpoint says so explicitly instead of returning an empty directory, and the
// frontend offers the original upstream page instead.
func MirrorListing(c fiber.Ctx) error {
	key := c.Params("key")
	subPath := listingSubPath(c)

	client := cacheproxy.Client()
	if client == nil {
		return model.Resp(c, http.StatusServiceUnavailable, any(nil), "caching proxy is not configured")
	}

	listing, err := v1.DirectoryListing(c.Context(), key, subPath, client)

	switch {
	case err == nil:
		return model.RespSuccess(c, listing)

	case errors.Is(err, v1.ErrMirrorNotFound):
		return model.RespNotFound(c)

	case errors.Is(err, cacheclient.ErrNotConfigured):
		return model.Resp(c, http.StatusServiceUnavailable, any(nil), "caching proxy is not configured")

	case errors.Is(err, v1.ErrUnsupportedType):
		return model.Resp(c, http.StatusNotImplemented, any(nil), "this mirror has no browsable directory")

	case errors.Is(err, v1.ErrListingUnavailable):
		// 404 so a client can distinguish "no listing here" from "server error",
		// with a message that says what to do instead.
		return model.Resp(c, http.StatusNotFound, any(nil),
			"the upstream did not return a directory listing for this path")

	case errors.Is(err, cacheclient.ErrBadGateway):
		return model.Resp(c, http.StatusBadGateway, any(nil), "the caching proxy or the upstream is unreachable")

	default:
		return err
	}
}

// listingSubPath extracts the directory part of a listing URL.
//
// The registered routes end in "/mirrors.json", so the wildcard covers
// "dists/noble/mirrors.json"; the trailing file name is not part of the
// directory being browsed.
func listingSubPath(c fiber.Ctx) string {
	raw := c.Params("*")
	if raw == "" {
		raw = c.Params("path")
	}

	trimmed := strings.Trim(raw, "/")
	trimmed = strings.TrimSuffix(trimmed, "mirrors.json")
	trimmed = strings.Trim(trimmed, "/")

	return trimmed
}
