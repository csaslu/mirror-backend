package parser

import (
	"net/url"
	"strings"

	"mirror/internal/model/data"
	modelErr "mirror/internal/model/errors"
)

// Upstream describes one mirror site we can read sync status from.
//
// Every site runs its own TunaSync deployment and, annoyingly, publishes the
// status document at its own path: TUNA under /static, NJU under /configs.
// Adding a site is therefore one entry here — no new parser code, because the
// document schema is the same everywhere.
type Upstream struct {
	// Name identifies the site in logs.
	Name string

	// Host is the canonical hostname of the mirror site.
	Host string

	// StatusPath is the path of the site's tunasync.json document.
	StatusPath string

	// Scheme is used when building the document URL.
	Scheme string

	// Format names the shape of the status document, which decides which
	// decoder reads it. Sites differ: most run tunasync and publish its
	// schema, USTC publishes its own.
	Format StatusFormat
}

// StatusFormat identifies a status document shape.
type StatusFormat string

const (
	// FormatTunaSync is the tunasync schema: an array of objects with
	// name/status/size/last_update_ts, where size is a string like "1.5G".
	FormatTunaSync StatusFormat = "tunasync"

	// FormatUSTC is 中国科学技术大学's document: an array of objects with
	// name/syncing/disable/exitCode/lastSuccess and an exact byte size.
	FormatUSTC StatusFormat = "ustc"
)

// Upstreams is the registry of supported mirror sites.
//
// Two things vary between deployments and both are declared here rather than in
// code:
//
//   - where the status document lives
//     TUNA https://mirrors.tuna.tsinghua.edu.cn/static/tunasync.json
//     NJU  https://mirror.nju.edu.cn/configs/tunasync.json
//     USTC https://mirrors.ustc.edu.cn/status/json
//   - what shape that document has (see StatusFormat)
//
// Adding a site is therefore one entry, and only a genuinely new document shape
// needs a decoder.
var Upstreams = []Upstream{
	{
		Name:       "tuna",
		Host:       "mirrors.tuna.tsinghua.edu.cn",
		StatusPath: "/static/tunasync.json",
		Scheme:     "https",
		Format:     FormatTunaSync,
	},
	{
		Name:       "nju",
		Host:       "mirror.nju.edu.cn",
		StatusPath: "/configs/tunasync.json",
		Scheme:     "https",
		Format:     FormatTunaSync,
	},
	{
		Name:       "ustc",
		Host:       "mirrors.ustc.edu.cn",
		StatusPath: "/status/json",
		Scheme:     "https",
		Format:     FormatUSTC,
	},
}

// StatusURL builds the absolute URL of an upstream's status document.
func (u Upstream) StatusURL() string {
	return u.Scheme + "://" + u.Host + u.StatusPath
}

// Match returns the upstream serving source, if we know it.
//
// Matching is done on the parsed hostname (not on a substring of the whole
// URL), so "https://mirrors.tuna.tsinghua.edu.cn" and "https://mirror.nju.edu.cn"
// cannot be confused with each other, and a lookalike host such as
// "evil-mirrors.tuna.tsinghua.edu.cn.attacker.net" does not match either.
func Match(source string) (Upstream, bool) {
	host := hostOf(source)
	if host == "" {
		return Upstream{}, false
	}

	for _, upstream := range Upstreams {
		if host == upstream.Host || strings.HasSuffix(host, "."+upstream.Host) {
			return upstream, true
		}
	}

	return Upstream{}, false
}

// hostOf extracts the lowercase hostname from a source, which may be a bare
// host, a full URL, or an rsync URL.
func hostOf(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}

	// A bare host has no scheme; give the parser something to work with.
	if !strings.Contains(source, "://") {
		source = "//" + source
	}

	parsed, err := url.Parse(source)
	if err != nil {
		return ""
	}

	return strings.ToLower(parsed.Hostname())
}

// FetchStatus reads one mirror's status from a specific upstream.
func FetchStatus(upstream Upstream, key string) (data.MirrorListStatus, error) {
	return TunaSyncParse(upstream, key)
}

// ErrUnsupported reports that we have no status source for a mirror.
func ErrUnsupported() error {
	return modelErr.ErrStatusSourceUnsupported
}
