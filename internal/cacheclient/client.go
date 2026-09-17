// Package cacheclient talks to the httpcached reverse-proxy cache.
//
// The backend never reaches an upstream directly: every mirror request — a
// directory listing the parser wants to read, or a file the browser asked for —
// goes through the cache, so cache rules (TTL, immutability, admission, stale
// serving, range support) are applied in exactly one place and the cache stays
// warm.
package cacheclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config describes how to reach the cache.
type Config struct {
	// Addr is host:port of the httpcached instance.
	Addr string

	// Host is the Host header used for the request. httpcached selects its
	// virtual host by Host header, so this must appear in the site's `hosts`
	// list. It defaults to Addr.
	Host string

	// Scheme is http or https (the cache itself usually speaks plain HTTP and
	// is terminated elsewhere).
	Scheme string

	// UserAgent is sent to the cache.
	UserAgent string

	// Timeout bounds a whole request, headers and body.
	Timeout time.Duration
}

// ErrNotConfigured is returned when no cache address is set. Callers use it to
// decide whether they can serve the request at all.
var ErrNotConfigured = errors.New("caching proxy is not configured")

// ErrBadGateway wraps transport failures, so handlers can answer 502 (the cache
// or the upstream is unreachable) instead of 500.
var ErrBadGateway = errors.New("caching proxy request failed")

// Client is a thin HTTP client for the caching proxy.
type Client struct {
	base   *url.URL
	host   string
	agent  string
	client *http.Client
}

// New validates the configuration and builds a client.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, ErrNotConfigured
	}

	scheme := cfg.Scheme
	if scheme == "" {
		scheme = "http"
	}

	base, err := url.Parse(scheme + "://" + cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("invalid cache address %q: %w", cfg.Addr, err)
	}

	host := cfg.Host
	if host == "" {
		host = cfg.Addr
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	return &Client{
		base:  base,
		host:  host,
		agent: cfg.UserAgent,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				MaxIdleConns:          64,
				MaxIdleConnsPerHost:   16,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
				// Never follow redirects here: the caller decides whether a
				// redirect is a directory move (worth relaying) or an upstream
				// quirk.
				DisableCompression: false,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// CachePath builds the cache URL for a mirror path ("/ubuntu/dists/").
func (c *Client) CachePath(mirrorPath string) string {
	if !strings.HasPrefix(mirrorPath, "/") {
		mirrorPath = "/" + mirrorPath
	}

	u := *c.base
	u.Path = c.base.Path + mirrorPath
	u.RawPath = ""

	return u.String()
}

// Get fetches a mirror path through the cache.
//
// The returned response body is the caller's to close.
func (c *Client) Get(ctx context.Context, mirrorPath string, accept string) (*http.Response, error) {
	target := c.CachePath(mirrorPath)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadGateway, err)
	}

	// Virtual-host selection, plus a UA the upstream can identify.
	request.Host = c.host
	if c.agent != "" {
		request.Header.Set("User-Agent", c.agent)
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadGateway, err)
	}

	return response, nil
}

// ReadBody fetches a mirror path and returns its body, refusing to buffer more
// than limit bytes. It is the entry point used by the directory parser.
func (c *Client) ReadBody(ctx context.Context, mirrorPath string, accept string, limit int64) ([]byte, *http.Response, error) {
	response, err := c.Get(ctx, mirrorPath, accept)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	reader := io.Reader(response.Body)
	if limit > 0 {
		// Read one byte past the limit to tell "exactly limit" from "too big".
		reader = io.LimitReader(response.Body, limit+1)
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, response, fmt.Errorf("%w: reading response: %v", ErrBadGateway, err)
	}

	if limit > 0 && int64(len(body)) > limit {
		return nil, response, fmt.Errorf("response exceeds the %d byte limit", limit)
	}

	return body, response, nil
}

// Stats is the subset of the cache's /_cache/stats we surface.
type Stats struct {
	Requests        int64   `json:"requests"`
	Hits            int64   `json:"hits"`
	Misses          int64   `json:"misses"`
	Pass            int64   `json:"pass"`
	Revalidated     int64   `json:"revalidated"`
	Stale           int64   `json:"stale"`
	Errors          int64   `json:"errors"`
	Stored          int64   `json:"stored"`
	Evictions       int64   `json:"evictions"`
	Expired         int64   `json:"expired"`
	BytesFromCache  int64   `json:"bytes_from_cache"`
	BytesFromOrigin int64   `json:"bytes_from_origin"`
	HitRatio        float64 `json:"hit_ratio"`
	Entries         int64   `json:"entries"`
	DiskBytes       int64   `json:"disk_bytes"`
}
