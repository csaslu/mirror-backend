package v1

import (
	"context"
	"mirror/internal/adapter"
	"mirror/internal/infra/database"
	"mirror/internal/infra/logger"
	"mirror/internal/model/data"
	dbModel "mirror/internal/model/database"
	"mirror/internal/model/errors"
	"mirror/internal/model/v1/response"
	"mirror/internal/repository"
	"mirror/internal/service"
	"sync"
	"time"

	"go.gh.ink/json"
	"go.gh.ink/timex"
	"go.uber.org/zap"
)

const (
	mirrorListCacheTTL = 24 * time.Hour
	mirrorListCacheKey = "mirror_list"
)

var listMutex = sync.RWMutex{}

func MirrorList() ([]response.MirrorListResponse, error) {
	// Create database session
	databaseSession := database.NewSession()
	mirrorListRepo := repository.NewMirrorListRepository(databaseSession)

	// Get list of mirrors from database or cache
	mirrors, err := mirrorListRepo.ReadAllMirrors()
	if err != nil {
		return nil, err
	}

	// Get status of mirrors from cache
	result, err := getMirrorStatus(0)
	if err != nil {
		return nil, err
	}

	// Match status with mirrors
	final := make([]response.MirrorListResponse, len(mirrors))
	for i, mirror := range mirrors {
		if _, ok := result[mirror.ID]; !ok {
			continue
		}
		final[i] = response.MirrorListResponse{
			Key:        mirror.Key,
			Comment:    mirror.Comment,
			Type:       mirror.Type,
			Source:     mirror.Source,
			Status:     result[mirror.ID].Status,
			Size:       result[mirror.ID].Size,
			LastUpdate: result[mirror.ID].LastUpdate,
		}
	}

	return final, nil
}

func getMirrorStatus(id int) (map[int]data.MirrorListStatus, error) {
	listMutex.RLock()
	defer listMutex.RUnlock()

	status, err := service.C.Namespace(mirrorListCacheKey).Get(context.Background())
	if err != nil {
		return nil, err
	}

	if status == nil || len(status) == 0 {
		return nil, errors.ErrCacheEmpty
	}

	result := make(map[int]data.MirrorListStatus)
	if err = json.Unmarshal(status, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func RefreshStatus() error {
	// Create database session
	databaseSession := database.NewSession()
	mirrorListRepo := repository.NewMirrorListRepository(databaseSession)

	// Get list of mirrors from database
	mirrors, err := mirrorListRepo.ReadAllMirrors()
	if err != nil {
		return err
	}

	// Refresh status for each mirror
	for _, mirror := range mirrors {
		// TODO: Support local tunasync
		switch mirror.Type {
		case dbModel.MirrorTypeReverseProxy:
			status, err := adapter.ParseStatus(mirror.Source, mirror.Key)
			if err != nil {
				logger.L.Error(
					"failed to parse mirror status",
					zap.String("key", mirror.Key),
					zap.String("source", mirror.Source),
					zap.Error(err),
				)
				continue
			}
			if err = setMirrorStatus(mirror.ID, status); err != nil {
				logger.L.Error(
					"failed to update mirror status",
					zap.String("key", mirror.Key),
					zap.String("source", mirror.Source),
					zap.Error(err),
				)
			}
		default:
			logger.L.Warn(
				"unsupported mirror type for status refresh",
				zap.String("type", mirror.Type),
				zap.String("key", mirror.Key),
			)
		}
	}

	return nil
}

func setMirrorStatus(id int, status data.MirrorListStatus) error {
	listMutex.Lock()
	defer listMutex.Unlock()

	// Read first
	preStatusData, err := service.C.Namespace(mirrorListCacheKey).Get(context.Background())
	if err != nil {
		return err
	}

	preStatus := make(map[int]data.MirrorListStatus)
	if preStatusData != nil && len(preStatusData) != 0 {
		if err = json.Unmarshal(preStatusData, &preStatus); err != nil {
			return err
		}
	}

	// Update the status
	preStatus[id] = status

	// Write back to cache
	nowStatusData, err := json.Marshal(preStatus)
	if err != nil {
		return err
	}

	return service.C.Set(
		context.Background(),
		nowStatusData,
		timex.FromStdDuration(mirrorListCacheTTL),
	)
}
