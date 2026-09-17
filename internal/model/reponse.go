package model

import (
	"crypto/sha256"
	"encoding/hex"
	"mirror/internal/infra/logger"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"go.uber.org/zap"
)

type response[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

// Resp writes the unified envelope. The HTTP status mirrors the semantic code
// so status-code based clients (and the browser) see 404/400/500 correctly.
func Resp[T any](c fiber.Ctx, code int, data T, msg string) error {
	return c.Status(code).JSON(response[T]{code, msg, data})
}

// --------------- 200 ---------------

func RespSuccess[T any](c fiber.Ctx, data T) error {
	return Resp(c, http.StatusOK, data, "success")
}

// RespSuccessCached answers with data plus a validator, and replies 304 Not
// Modified when the client already holds the same version.
//
// The mirror list is polled by every open page, so this is what keeps the
// minute-by-minute refresh close to free: an unchanged snapshot costs headers
// only, and the frontend keeps its cached copy.
func RespSuccessCached[T any](c fiber.Ctx, data T, validator string) error {
	if validator != "" {
		c.Set(fiber.HeaderETag, validator)

		if match := c.Get(fiber.HeaderIfNoneMatch); match != "" && etagMatches(match, validator) {
			return c.SendStatus(http.StatusNotModified)
		}
	}

	return RespSuccess(c, data)
}

// etagMatches reports whether an If-None-Match header covers our validator.
// The header may be a list, and "W/" prefixes mark weak comparison.
func etagMatches(header string, validator string) bool {
	target := strings.TrimPrefix(validator, "W/")

	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == target {
			return true
		}
	}

	return false
}

// Validator derives a stable ETag for a response payload.
func Validator(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return `W/"` + hex.EncodeToString(hash[:8]) + `"`
}

// --------------- 400 ---------------

func RespBadRequest(c fiber.Ctx) error {
	return Resp(c, http.StatusBadRequest, any(nil), "bad request")
}

func RespForbidden(c fiber.Ctx) error {
	return Resp(c, http.StatusForbidden, any(nil), "forbidden")
}

func RespNotFound(c fiber.Ctx) error {
	return Resp(c, http.StatusNotFound, any(nil), "not found")
}

func RespMethodNotAllowed(c fiber.Ctx) error {
	return Resp(c, http.StatusMethodNotAllowed, any(nil), "method not allowed")
}

func RespTeaPot[T any](c fiber.Ctx, data T) error {
	return Resp(c, http.StatusTeapot, data, "I'm a tea pot")
}

func RespTooManyRequests(c fiber.Ctx) error {
	return Resp(c, http.StatusTooManyRequests, any(nil), "too many requests")
}

// --------------- 500 ---------------

func RespInternalServerError(c fiber.Ctx, err error) error {
	requestID := requestid.FromContext(c)
	logger.L.Error(
		err.Error(),
		zap.String("request_id", requestID),
	)
	return Resp(c, http.StatusInternalServerError, any(nil), "internal server error")
}
