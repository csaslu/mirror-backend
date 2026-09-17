package repository

import (
	"context"
	"time"

	"mirror/internal/infra/database"
	dbModel "mirror/internal/model/database"
	"mirror/internal/model/errors"

	"go.gh.ink/cask"
	"go.gh.ink/json"
	"go.gh.ink/timex"
	"xorm.io/xorm"
)

type MirrorListRepository struct {
	session *xorm.Session
	cache   *cask.Namespace
}

const (
	mirrorListCacheTTL = 24 * time.Hour
	mirrorListCacheKey = "all_mirrors"
)

// NewMirrorListRepository builds a repository. The database session is opened
// lazily by ReadAllMirrors, so cache hits never touch PostgreSQL and callers
// cannot forget to close a session.
//
// The cache namespace is optional: when the Redis cache has not been
// initialised (for example in a tool that only reads the database, such as the
// proxy-config generator), reads and writes simply skip the cache instead of
// panicking on a nil namespace.
func NewMirrorListRepository() *MirrorListRepository {
	repo := &MirrorListRepository{}

	if C != nil {
		repo.cache = C.Namespace("mirror_list")
	}

	return repo
}

// ReadAllMirrors returns every mirror, from cache when warm and from
// PostgreSQL otherwise.
func (r *MirrorListRepository) ReadAllMirrors() ([]dbModel.MirrorList, error) {
	ctx := context.Background()

	// Try to get from cache first
	if cached, err := r.getMirrorListCache(ctx); err == nil && cached != nil {
		return cached, nil
	}

	// If cache miss, read from database
	if r.session == nil {
		r.session = database.NewSession()
		defer database.Close(r.session)
	}

	result := make([]dbModel.MirrorList, 0)
	if err := r.session.Find(&result); err != nil {
		return nil, err
	}

	// Refresh the cache, but never fail the read because of a cache problem:
	// the database answer is already valid.
	_ = r.setMirrorListCache(ctx, result)

	return result, nil
}

// setMirrorListCache stores the mirror list in cache with TTL
func (r *MirrorListRepository) setMirrorListCache(ctx context.Context, mirrors []dbModel.MirrorList) error {
	if r.cache == nil {
		return errors.ErrCacheNotAvailable
	}

	data, err := json.Marshal(mirrors)
	if err != nil {
		return err
	}

	return r.cache.Namespace(mirrorListCacheKey).Set(ctx, data, timex.FromStdDuration(mirrorListCacheTTL))
}

// getMirrorListCache retrieves the mirror list from cache
func (r *MirrorListRepository) getMirrorListCache(ctx context.Context) ([]dbModel.MirrorList, error) {
	if r.cache == nil {
		return nil, errors.ErrCacheNotAvailable
	}

	data, err := r.cache.Namespace(mirrorListCacheKey).Get(ctx)
	if err != nil {
		return nil, err
	}

	if data == nil || len(data) == 0 {
		return nil, errors.ErrCacheEmpty
	}

	var result []dbModel.MirrorList
	if err = json.Unmarshal(data, &result); err != nil {
		// A corrupt entry must not break the endpoint: drop it and let the
		// caller fall back to PostgreSQL.
		_, _ = r.cache.Namespace(mirrorListCacheKey).Del(ctx)
		return nil, err
	}

	return result, nil
}

// InvalidateMirrorListCache removes the mirror list from cache
func (r *MirrorListRepository) InvalidateMirrorListCache(ctx context.Context) error {
	if r.cache == nil {
		return nil
	}

	_, err := r.cache.Namespace(mirrorListCacheKey).Del(ctx)
	return err
}
