package v1

import (
	"context"
	"errors"
	"sync"
	"time"

	"mirror/internal/adapter"
	"mirror/internal/infra/logger"
	"mirror/internal/model/data"
	dbModel "mirror/internal/model/database"
	modelErr "mirror/internal/model/errors"
	"mirror/internal/model/v1/response"
	"mirror/internal/repository"
	"mirror/internal/service"

	"go.gh.ink/cask"
	"go.gh.ink/json"
	"go.gh.ink/timex"
	"go.uber.org/zap"
)

const (
	// mirrorListCacheTTL is how long a status snapshot stays in Redis; the
	// refresh cron rewrites it well before this expires.
	mirrorListCacheTTL = 24 * time.Hour

	// statusNamespace / statusKey identify the Redis key holding the status
	// map. The reader and the writer must use the very same pair: cask joins
	// every namespace level into the key, so reading from
	// `service:mirror:status` while writing to `service` silently never hits.
	statusNamespace = "mirror"
	statusKey       = "status"
)

var listMutex = sync.RWMutex{}

// MirrorList returns every mirror in the database joined with its last known
// sync status. Mirrors without a known status are still returned, with null
// status fields, so the frontend can list them all.
func MirrorList() ([]response.MirrorListResponse, error) {
	repo := repository.NewMirrorListRepository()

	mirrors, err := repo.ReadAllMirrors()
	if err != nil {
		return nil, err
	}

	// A missing or unreadable status cache is not fatal: mirrors stay listable,
	// just without freshness information. Failing here would turn a cold start
	// into a 500 for the whole homepage.
	statuses, err := getMirrorStatus()
	if err != nil {
		logger.L.Warn("mirror status cache unavailable", zap.Error(err))
		statuses = map[int]data.MirrorListStatus{}
	}

	result := make([]response.MirrorListResponse, 0, len(mirrors))
	for _, mirror := range mirrors {
		item := response.MirrorListResponse{
			ID:      mirror.ID,
			Key:     mirror.Key,
			Comment: mirror.Comment,
			Type:    mirror.Type,
			Source:  mirror.Source,
		}

		if status, ok := statuses[mirror.ID]; ok {
			item.Status = status.Status
			item.Size = status.Size
			item.LastUpdate = status.LastUpdate
		}

		result = append(result, item)
	}

	return result, nil
}

// MirrorStatus resolves the status of one mirror, falling back to a live
// upstream query when the cache holds nothing for it.
func MirrorStatus(id int) (data.MirrorListStatus, error) {
	statuses, err := getMirrorStatus()
	if err == nil {
		if status, ok := statuses[id]; ok {
			return status, nil
		}
	}

	repo := repository.NewMirrorListRepository()
	mirrors, err := repo.ReadAllMirrors()
	if err != nil {
		return data.MirrorListStatus{}, err
	}

	for _, mirror := range mirrors {
		if mirror.ID == id {
			return fetchMirrorStatus(mirror)
		}
	}

	return data.MirrorListStatus{}, modelErr.ErrStatusNotFound
}

// getMirrorStatus reads the whole status map from Redis. An empty cache yields
// an empty map rather than an error.
func getMirrorStatus() (map[int]data.MirrorListStatus, error) {
	listMutex.RLock()
	defer listMutex.RUnlock()

	return readStatusCache()
}

// readStatusCache performs the cache read. Callers must hold listMutex.
func readStatusCache() (map[int]data.MirrorListStatus, error) {
	status := make(map[int]data.MirrorListStatus)

	if service.C == nil {
		return status, modelErr.ErrCacheNotAvailable
	}

	raw, err := statusCache().
		Get(context.Background())
	if err != nil {
		// A cold key — or an unusable cache — is not an error: it just means we
		// have no snapshot yet, and the caller decides how to fill the gap.
		if modelErr.IsNotFound(err) {
			return status, nil
		}
		return status, err
	}

	if len(raw) == 0 {
		return status, nil
	}

	if err = json.Unmarshal(raw, &status); err != nil {
		return make(map[int]data.MirrorListStatus), err
	}

	return status, nil
}

// statusCache returns the namespace holding the status map. Every read and
// write goes through here so the key can never drift apart again.
func statusCache() *cask.Namespace {
	return service.C.
		Namespace(statusNamespace).
		Namespace(statusKey)
}

// RefreshStatus queries every supported upstream once and rewrites the status
// cache. It is registered on the cron and is safe to call concurrently.
func RefreshStatus() error {
	repo := repository.NewMirrorListRepository()

	mirrors, err := repo.ReadAllMirrors()
	if err != nil {
		return err
	}

	// Read the previous map once, update it in memory, write it back once:
	// per-mirror read-modify-write would multiply Redis round trips and lose
	// concurrent updates.
	listMutex.Lock()
	defer listMutex.Unlock()

	statuses, err := readStatusCache()
	if err != nil {
		logger.L.Warn("status cache unreadable, starting a fresh snapshot", zap.Error(err))
		statuses = make(map[int]data.MirrorListStatus)
	}

	updated := 0
	for _, mirror := range mirrors {
		if !supportsStatus(mirror.Type) {
			continue
		}

		status, err := fetchMirrorStatus(mirror)
		switch {
		case err == nil:
			statuses[mirror.ID] = status
			updated++
		case errors.Is(err, modelErr.ErrStatusNotFound):
			// Upstream does not carry this mirror (yet): drop any stale entry
			// so we never advertise an outdated size or timestamp.
			delete(statuses, mirror.ID)
			logger.L.Warn(
				"mirror missing from upstream status",
				zap.String("key", mirror.Key),
				zap.String("source", mirror.Source),
			)
		case errors.Is(err, modelErr.ErrStatusSourceUnsupported):
			logger.L.Warn(
				"no status parser for upstream",
				zap.String("key", mirror.Key),
				zap.String("source", mirror.Source),
			)
		default:
			// Keep the previous snapshot on transport failures.
			logger.L.Error(
				"failed to refresh mirror status",
				zap.String("key", mirror.Key),
				zap.String("source", mirror.Source),
				zap.Error(err),
			)
		}
	}

	if service.C == nil {
		return modelErr.ErrCacheNotAvailable
	}

	payload, err := json.Marshal(statuses)
	if err != nil {
		return err
	}

	if err = statusCache().
		Set(context.Background(), payload, timex.FromStdDuration(mirrorListCacheTTL)); err != nil {
		return err
	}

	logger.L.Info(
		"mirror status snapshot refreshed",
		zap.Int("total", len(mirrors)),
		zap.Int("updated", updated),
	)

	return nil
}

// supportsStatus reports whether sync status can be resolved for a mirror type.
func supportsStatus(mirrorType dbModel.MirrorType) bool {
	switch mirrorType {
	case dbModel.MirrorTypeReverseProxy, dbModel.MirrorTypeRsync:
		return true
	default:
		return false
	}
}

// fetchMirrorStatus asks the mirror's upstream for its current status.
func fetchMirrorStatus(mirror dbModel.MirrorList) (data.MirrorListStatus, error) {
	return adapter.ParseStatus(mirror.Source, mirror.Key)
}

// DropCaches removes the cached mirror list and the cached sync status.
//
// Two caches are involved and they are independent: the list lives 24 hours
// (mirrors change rarely, so re-reading PostgreSQL on every page view would be
// wasteful), and the status snapshot is rewritten hourly by the refresh cron.
// After editing mirror_list, dropping the list cache is what makes the change
// visible without waiting for the TTL.
func DropCaches() error {
	if service.C == nil {
		return modelErr.ErrCacheNotAvailable
	}

	repo := repository.NewMirrorListRepository()
	if err := repo.InvalidateMirrorListCache(context.Background()); err != nil {
		return err
	}

	if _, err := statusCache().Del(context.Background()); err != nil {
		return err
	}

	return nil
}
