package adapter

import (
	"mirror/internal/adapter/parser"
	"mirror/internal/model/data"
	modelErr "mirror/internal/model/errors"
)

// ParseStatus resolves the sync status of one mirror from its upstream.
//
// Sources we have no parser for yield modelErr.ErrStatusSourceUnsupported
// rather than an empty result, so a mirror is never reported as "0 bytes,
// synced at the epoch" when we simply do not know.
//
// The set of supported upstreams lives in parser.Upstreams (host + status
// document path), because every site publishes its tunasync.json somewhere
// different. Adding a site does not change this function.
func ParseStatus(source string, key string) (data.MirrorListStatus, error) {
	upstream, ok := parser.Match(source)
	if !ok {
		return data.MirrorListStatus{}, modelErr.ErrStatusSourceUnsupported
	}

	return parser.FetchStatus(upstream, key)
}
