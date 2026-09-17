package errors

import (
	"errors"

	"github.com/redis/go-redis/v9"
)

// IsNotFound reports whether err means "this key is not in the cache".
//
// The cask Redis driver passes the client's error straight through, so a cold
// key surfaces as redis.Nil rather than as a cask sentinel. Callers must treat
// this as a cache miss, never as a failure.
func IsNotFound(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, redis.Nil):
		return true
	case errors.Is(err, ErrCacheEmpty):
		return true
	case errors.Is(err, ErrCacheNotAvailable):
		return true
	default:
		return false
	}
}
