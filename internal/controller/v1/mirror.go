package v1

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"mirror/internal/model"
	modelErr "mirror/internal/model/errors"
	"mirror/internal/model/v1/response"
	"mirror/internal/service/v1"

	"github.com/gofiber/fiber/v3"
)

// MirrorList handles GET /api/v1/mirrors.json
func MirrorList(c fiber.Ctx) error {
	mirrors, err := v1.MirrorList()
	if err != nil {
		return err
	}

	// The list is polled by every open page. The validator covers the mirror
	// rows and their status fields, so an unchanged snapshot answers 304 and
	// the browser reuses what it already has.
	validator := model.Validator(fmt.Sprintf("v1|%d", len(mirrors)), mirrorDigest(mirrors))

	return model.RespSuccessCached(c, mirrors, validator)
}

// mirrorDigest builds a stable fingerprint of the response payload.
func mirrorDigest(mirrors []response.MirrorListResponse) string {
	var builder strings.Builder

	for _, mirror := range mirrors {
		fmt.Fprintf(&builder, "%d:%s:%s:%s:", mirror.ID, mirror.Key, mirror.Type, mirror.Comment)

		if mirror.Status != nil {
			builder.WriteString(*mirror.Status)
		}
		builder.WriteByte(':')

		if mirror.Size != nil {
			fmt.Fprintf(&builder, "%d", *mirror.Size)
		}
		builder.WriteByte(':')

		if mirror.LastUpdate != nil {
			fmt.Fprintf(&builder, "%d", *mirror.LastUpdate)
		}
		builder.WriteByte('|')
	}

	return builder.String()
}

// MirrorStatus handles GET /api/v1/mirrors/:id/status.json
func MirrorStatus(c fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return model.RespBadRequest(c)
	}

	status, err := v1.MirrorStatus(id)
	switch {
	case err == nil:
		return model.RespSuccess(c, status)
	case errors.Is(err, modelErr.ErrStatusNotFound):
		return model.RespNotFound(c)
	case errors.Is(err, modelErr.ErrStatusSourceUnsupported):
		return model.Resp(c, http.StatusNotImplemented, any(nil), "status source unsupported")
	default:
		return err
	}
}
