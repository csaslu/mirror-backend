package parser

import (
	"mirror/internal/infra/logger"
	"mirror/internal/meta"
	"mirror/internal/model/data"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
)

// TunaSyncParse provides a common method to parse
// mirror site use TunaSync to sync data from upstream.
func TunaSyncParse(metaUrl string, key string) (data.MirrorListStatus, error) {
	// Get tunalist.json metadata
	result := make([]data.TunaSync, 0)
	client := resty.New()
	resp, err := client.R().
		SetHeader("User-Agent", meta.UserAgent).
		SetResult(&result).
		Get(metaUrl)
	if err != nil {
		return data.MirrorListStatus{}, err
	}

	// Process the response
	if resp.StatusCode() != 200 {
		logger.L.Warn(
			"failed to fetch mirror status",
			zap.String("url", metaUrl),
			zap.Int("status_code", resp.StatusCode()),
		)
		return data.MirrorListStatus{}, nil
	}

	// Save specific mirror item status
	status := ""
	size := int64(0)
	lastUpdate := int64(0)
	for _, item := range result {
		if item.Name == key {
			status = item.Status
			size = UnifySize(item.Size)
			lastUpdate = item.LastUpdateTS
			return data.MirrorListStatus{
				Status:     &status,
				Size:       &size,
				LastUpdate: &lastUpdate,
			}, nil
		}
	}

	return data.MirrorListStatus{}, nil
}
