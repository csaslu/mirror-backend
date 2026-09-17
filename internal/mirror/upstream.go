// Package mirror derives the upstream locations of mirrors and the information
// the caching proxy needs to reach them.
//
// The database stores a mirror's `source` as the site root
// ("https://mirrors.tuna.tsinghua.edu.cn") while the mirror itself lives in a
// directory named after `key` ("/ubuntu"). The caching proxy routes on path
// prefixes, so it has to be told the full upstream directory for every mirror:
// that translation lives here, so the HTTP client, the directory parser and the
// generated proxy configuration cannot disagree with each other.
package mirror

import (
	"fmt"
	"net/url"
	"strings"
)

// Upstream is a resolved upstream location of one mirror.
type Upstream struct {
	// BaseURL is the upstream directory without a trailing slash, e.g.
	// "https://mirrors.tuna.tsinghua.edu.cn/ubuntu".
	BaseURL string

	// Root is the site root as stored in the database.
	Root string

	// Path is the path of the directory relative to Root ("/ubuntu"); it is
	// empty when the source already points at the mirror directory itself.
	Path string

	// Scheme and Host are the parsed parts of Root, for cache-key purposes.
	Scheme string
	Host   string
}

// ParseUpstream resolves the upstream directory for a mirror.
//
// Accepted sources:
//
//	https://mirrors.tuna.tsinghua.edu.cn          -> .../<key>
//	https://mirrors.tuna.tsinghua.edu.cn/ubuntu    -> unchanged (must equal /<key>)
//	https://mirror.nju.edu.cn/fedora-archive       -> unchanged (must equal /<key>)
//	rsync://mirrors.tuna.tsinghua.edu.cn/openwrt   -> scheme https, .../openwrt
//
// A source whose path disagrees with the mirror key is an error: silently
// guessing would make every URL we hand out, and every file we cache, point at
// the wrong directory.
func ParseUpstream(source string, key string) (Upstream, error) {
	source = strings.TrimSpace(source)
	key = strings.Trim(strings.TrimSpace(key), "/")

	if source == "" {
		return Upstream{}, fmt.Errorf("mirror %q: empty source", key)
	}
	if key == "" {
		return Upstream{}, fmt.Errorf("mirror: empty key for source %q", source)
	}

	parsed, err := url.Parse(source)
	if err != nil {
		return Upstream{}, fmt.Errorf("mirror %q: invalid source %q: %w", key, source, err)
	}

	// rsync:// sources describe the same directory tree; httpcached and the
	// directory parser talk HTTP, so treat them as https.
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()

	if host == "" || scheme == "" {
		return Upstream{}, fmt.Errorf("mirror %q: source %q has no host", key, source)
	}
	if scheme != "http" && scheme != "https" && scheme != "rsync" {
		return Upstream{}, fmt.Errorf("mirror %q: unsupported source scheme %q", key, scheme)
	}
	if scheme == "rsync" {
		scheme = "https"
	}

	root := scheme + "://" + host
	if port != "" {
		root += ":" + port
	}

	mirrorPath := strings.TrimRight(parsed.Path, "/")

	switch {
	case mirrorPath == "" || mirrorPath == "/":
		// Site root: the mirror lives in the directory named after the key.
		mirrorPath = "/" + key
	case strings.TrimPrefix(mirrorPath, "/") != key:
		return Upstream{}, fmt.Errorf(
			"mirror %q: source path %q does not match the mirror key (expected %q)",
			key, mirrorPath, "/"+key,
		)
	}

	return Upstream{
		BaseURL: root + mirrorPath,
		Root:    root,
		Path:    mirrorPath,
		Scheme:  scheme,
		Host:    host,
	}, nil
}

// CachePath is the path httpcached is asked for when serving this mirror.
// The caching proxy matches routes by path prefix, so this equals the mirror's
// directory path on our own site as well.
func (u Upstream) CachePath() string {
	return u.Path
}
