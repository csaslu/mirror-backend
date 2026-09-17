// Package proxyconfig renders the caching proxy's configuration file from the
// mirror list in the database.
//
// The proxy routes mirror paths to upstreams, so its configuration is derived
// data: hand-maintaining it means a newly added mirror silently 404s until
// someone remembers to edit a second file. Generating it keeps the database the
// single source of truth.
//
// The generated file is a complete, deployable configuration (cache storage,
// policy rules, site routes). Deployment-specific values — disk size, cache
// directory, port — have sensible defaults and are documented inline so an
// operator can see what to change.
package proxyconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"mirror/internal/infra/config"
	"mirror/internal/infra/logger"
	"mirror/internal/mirror"
	"mirror/internal/repository"

	"go.uber.org/zap"
)

// templateData is the input of the configuration template.
type templateData struct {
	// ListenHost/ListenPort are the proxy's own listener.
	ListenHost string
	ListenPort int

	// Hosts are the Host header values that select our routes.
	Hosts []string

	// StorageDir holds cached objects; it should be on the big disk.
	StorageDir string

	// MaxSize caps the cache on disk (soft cap; eviction runs on a timer).
	MaxSize string

	// Routes maps mirror prefixes to upstream directories.
	Routes []mirror.CacheRoute

	// Skipped lists mirrors that produced no route, with the reason.
	Skipped []skippedMirror

	// GeneratedAt is a timestamp header for the file.
	GeneratedAt string
}

type skippedMirror struct {
	Key    string
	Reason string
}

// Write renders the configuration to path (or to stdout when path is "-").
func Write(path string) error {
	rendered, err := Render()
	if err != nil {
		return err
	}

	if path == "-" {
		fmt.Print(rendered)
		return nil
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	// Write through a temporary file so a reader (or the proxy's SIGHUP
	// reload) never observes a half-written configuration.
	temp := path + ".tmp"
	if err := os.WriteFile(temp, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", temp, err)
	}

	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}

	return nil
}

// Render builds the configuration text.
func Render() (string, error) {
	repo := repository.NewMirrorListRepository()

	mirrors, err := repo.ReadAllMirrors()
	if err != nil {
		return "", err
	}

	routes, skippedReasons := mirror.CacheRoutes(mirrors)

	cfg := config.Get()

	// The proxy is reached by the backend, so it listens on loopback by
	// default; the Host header list has to include whatever the backend sends.
	proxyHost := strings.TrimSpace(cfg.Mirror.CacheHost)
	if proxyHost == "" {
		proxyHost = strings.TrimSpace(cfg.Mirror.CacheAddr)
	}

	hosts := []string{"127.0.0.1", "localhost"}
	if host, _, found := strings.Cut(proxyHost, ":"); found && host != "" {
		hosts = append(hosts, host)
	} else if proxyHost != "" {
		hosts = append(hosts, proxyHost)
	}
	if publicHost := strings.TrimSpace(cfg.Mirror.Host); publicHost != "" {
		hosts = append(hosts, publicHost)
	}

	hosts = uniqueStrings(hosts)

	skipped := make([]skippedMirror, 0, len(skippedReasons))
	for key, reason := range skippedReasons {
		skipped = append(skipped, skippedMirror{Key: key, Reason: reason.Error()})
	}
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Key < skipped[j].Key })

	data := templateData{
		ListenHost:  "127.0.0.1",
		ListenPort:  8001,
		Hosts:       hosts,
		StorageDir:  "./cache_data",
		MaxSize:     "200GB",
		Routes:      routes,
		Skipped:     skipped,
		GeneratedAt: timeNow(),
	}

	var buffer bytes.Buffer
	if err := configTemplate.Execute(&buffer, data); err != nil {
		return "", err
	}

	logger.L.Info("caching proxy configuration rendered",
		zap.Int("routes", len(routes)),
		zap.Int("skipped", len(skipped)),
	)

	return buffer.String(), nil
}

// uniqueStrings keeps the first occurrence of each value, preserving order.
func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
}

// configTemplate is the generated file.
//
// It is deliberately a complete configuration rather than a fragment: the
// routes are generated, everything else is a documented default that an
// operator can adjust in place (or override by keeping their own file, since
// only the routes change when mirrors change).
var configTemplate = template.Must(template.New("httpcached").Funcs(template.FuncMap{
	"quote": func(s string) string { return strconvQuote(s) },
}).Parse(`# Code generated by mirror-backend from the mirror_list table. DO NOT EDIT.
#
# Regenerate with:
#     go run . -dump-cache-config <this file>
# then reload the running proxy:
#     kill -HUP <httpcached pid>
#
# Generated at {{ .GeneratedAt }}

server:
  # The backend reaches the cache on loopback; keep it off the public network.
  host: {{ quote .ListenHost }}
  port: {{ .ListenPort }}
  body_limit: 0

log:
  file:
    all: "logs/app.log"
    err: "logs/err.log"
  max_size: 5
  max_backups: 10
  max_age: 30
  compress: true

cache:
  storage:
    # Put this on the disk that holds the cached artefacts.
    dir: {{ quote .StorageDir }}
    # Soft cap. Eviction only runs on the caretaker tick, so leave headroom
    # below the real disk size.
    max_size: {{ quote .MaxSize }}
    # Hard retention cap regardless of freshness.
    max_age: "720h"
    caretaker_interval: "5m"

  upstream:
    max_conns_per_host: 256
    max_idle_conns_per_host: 64
    idle_conn_timeout: "90s"
    dial_timeout: "10s"
    response_header_timeout: "30s"
    # NOTE: when this key is omitted the proxy does NOT follow redirects.
    follow_redirects: true
    user_agent: "LidaMirror/1.0.0 (mirror-proxy)"

  admission:
    # Cold objects are streamed through without touching disk until they have
    # been requested this many times, which keeps one-off downloads from
    # evicting hot packages.
    window_size: 100000
    default_min_hits: 2

  defaults:
    cache: true
    ttl: "24h"
    revalidate: true
    stale_if_error: true
    max_object_size: "0"

  rules:
    # Directory listings change on every sync: short TTL, always revalidate.
    - name: directory-listing
      match:
        path_regex: '/$'
      policy:
        cache: true
        ttl: "60s"
        revalidate: true
        min_hits: 1

    # Content-addressed artefacts never change under the same name.
    - name: immutable-artifacts
      match:
        ext: [deb, udeb, ddeb, rpm, apk, whl, egg, gz, xz, zst, bz2, tgz, iso, jar, conda, pkg]
      policy:
        cache: true
        immutable: true
        ttl: "720h"
        min_hits: 1

    # Repository metadata is rewritten in place: short TTL, cheap 304s.
    - name: repo-metadata
      match:
        path_regex: '(InRelease|Release(\.gpg)?|Packages(\.\w+)?|Sources(\.\w+)?|Contents-.*|repodata/.*|repomd\.xml(\.\w+)?|.*\.(json|db|files|sqlite)(\.\w+)?)$'
      policy:
        cache: true
        ttl: "5m"
        revalidate: true
        min_hits: 1

    # Image signatures and indices are tiny and must stay correct.
    - name: container-index
      match:
        path_regex: '(index\.json|manifest\.json|/v2/.*/manifests/|/v2/.*/blobs/sha256:)'
      policy:
        cache: true
        ttl: "10m"
        revalidate: true
        min_hits: 1

    # Tokens, sessions and auth handshakes must never be cached.
    - name: no-cache-dynamic
      match:
        path_regex: '(/token|/v2/auth|/login|session|/api/)'
      policy:
        cache: false

  sites:
    # The Host header selects this site; within it, the request path is
    # longest-prefix matched against the routes below, and the matched prefix
    # is stripped before being joined onto the upstream.
    - hosts: [{{ range $i, $h := .Hosts }}{{ if $i }}, {{ end }}{{ quote $h }}{{ end }}]
      routes:
{{- range .Routes }}
        - { path: {{ quote .Path }}, upstream: {{ quote .Upstream }} }
{{- end }}
{{- if .Skipped }}
# Mirrors skipped when this file was generated (they have no HTTP upstream):
{{- range .Skipped }}
#   {{ .Key }}: {{ .Reason }}
{{- end }}
{{- end }}
`))

// strconvQuote quotes a string as a YAML scalar.
func strconvQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

// timeNow is indirected so tests can pin the generated header.
var timeNow = func() string {
	return time.Now().Format(time.RFC3339)
}
