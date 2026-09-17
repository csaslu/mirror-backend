package mirror

import (
	"net/url"
	"sort"
	"strings"

	dbModel "mirror/internal/model/database"
)

// CacheRoute is one route in the caching proxy's site configuration.
type CacheRoute struct {
	// Path is the mirrored prefix on our site, e.g. "/ubuntu".
	Path string

	// Upstream is the upstream directory, e.g.
	// "https://mirrors.tuna.tsinghua.edu.cn/ubuntu".
	Upstream string
}

// CacheRoutes builds the proxy routes for every HTTP(s) mirror.
//
// Only types that are served over HTTP are included: rsync-only mirrors have
// no upstream to fetch pages or files from, and would only produce a route that
// always fails. Mirrors whose source is inconsistent with their key are skipped
// rather than guessed at; the returned errors explain why.
func CacheRoutes(mirrors []dbModel.MirrorList) (routes []CacheRoute, skipped map[string]error) {
	skipped = make(map[string]error)

	for _, item := range mirrors {
		if !httpServable(item.Type) {
			continue
		}

		upstream, err := ParseUpstream(item.Source, item.Key)
		if err != nil {
			skipped[item.Key] = err
			continue
		}

		routes = append(routes, CacheRoute{Path: upstream.CachePath(), Upstream: upstream.BaseURL})
	}

	// Longest prefix first, so /ubuntu-ports is never shadowed by /ubuntu.
	sort.Slice(routes, func(i, j int) bool {
		if len(routes[i].Path) != len(routes[j].Path) {
			return len(routes[i].Path) > len(routes[j].Path)
		}
		return routes[i].Path < routes[j].Path
	})

	return routes, skipped
}

// httpServable reports whether a mirror type can be fetched over HTTP.
func httpServable(mirrorType dbModel.MirrorType) bool {
	switch mirrorType {
	case dbModel.MirrorTypeReverseProxy, dbModel.MirrorTypeHttp, dbModel.MirrorTypeHttps:
		return true
	default:
		return false
	}
}

// JoinPath appends a relative request path to a base path, rejecting anything
// that would escape it.
//
// The path comes from the URL, so it is untrusted: "..", encoded separators and
// absolute paths must never let a request address a directory outside the
// mirror it claims to be browsing.
//
// The result stays inside the base and never contains an empty segment, so a
// base of "/" with a relative "dists" yields "/dists" rather than "//dists".
func JoinPath(base string, rel string) (string, bool) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	rel = strings.TrimSpace(rel)

	// Decode per cent-escapes before inspecting segments: "%2e%2e" is ".." and
	// must be refused just like the literal form.
	if decoded, err := url.PathUnescape(rel); err == nil {
		rel = decoded
	}

	// Normalise separators a client could have encoded.
	rel = strings.ReplaceAll(rel, "\\", "/")

	segments := make([]string, 0, 8)
	for segment := range strings.SplitSeq(rel, "/") {
		switch segment {
		case "", ".":
			continue
		case "..":
			return "", false
		default:
			segments = append(segments, segment)
		}
	}

	if len(segments) == 0 {
		if base == "" {
			return "/", true
		}
		return base, true
	}

	joined := base + "/" + strings.Join(segments, "/")
	if joined == "" || joined[0] != '/' {
		joined = "/" + joined
	}

	return joined, true
}
