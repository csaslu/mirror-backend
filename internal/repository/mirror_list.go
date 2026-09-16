package repository

import (
	"context"
	"mirror/internal/model/database"
	"mirror/internal/model/errors"
	"time"

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

func NewMirrorListRepository(session *xorm.Session) *MirrorListRepository {
	return &MirrorListRepository{session: session, cache: C.Namespace("mirror_list")}
}

func (r *MirrorListRepository) ReadAllMirrors() ([]database.MirrorList, error) {
	ctx := context.Background()

	// Try to get from cache first
	if cached, err := r.getMirrorListCache(ctx); err == nil && cached != nil {
		return cached, nil
	}

	// If cache miss, read from database
	result := make([]database.MirrorList, 0)
	err := r.session.Find(&result)
	if err != nil {
		return result, err
	}

	// Store in cache for future requests
	_ = r.setMirrorListCache(ctx, result)

	return result, err
}

// setMirrorListCache stores the mirror list in cache with TTL
func (r *MirrorListRepository) setMirrorListCache(ctx context.Context, mirrors []database.MirrorList) error {
	if r.cache == nil || mirrors == nil {
		return nil
	}

	data, err := json.Marshal(mirrors)
	if err != nil {
		return err
	}

	return r.cache.Namespace(mirrorListCacheKey).Set(ctx, data, timex.FromStdDuration(mirrorListCacheTTL))
}

// getMirrorListCache retrieves the mirror list from cache
func (r *MirrorListRepository) getMirrorListCache(ctx context.Context) ([]database.MirrorList, error) {
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

	var result []database.MirrorList
	if err = json.Unmarshal(data, &result); err != nil {
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
