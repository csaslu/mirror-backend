package model

import (
	"mirror/internal/infra/logger"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"go.uber.org/zap"
)

type response[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

func Resp[T any](c fiber.Ctx, code int, data T, msg string) error {
	return c.Status(http.StatusOK).JSON(response[T]{code, msg, data})
}

// --------------- 200 ---------------

func RespSuccess[T any](c fiber.Ctx, data T) error {
	return Resp(c, http.StatusOK, data, "success")
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
