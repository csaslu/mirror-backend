package errors

import "errors"

var (
	// ErrStatusSourceUnsupported means we have no parser for the mirror's
	// upstream, so its status is simply unknown. Reporting it is expected and
	// must not be treated as a failure.
	ErrStatusSourceUnsupported = errors.New("status source unsupported")

	// ErrStatusNotFound means the upstream status document was fetched but does
	// not contain the requested mirror.
	ErrStatusNotFound = errors.New("mirror not found in upstream status")
)
