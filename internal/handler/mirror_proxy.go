package handler

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"mirror/internal/cacheclient"
	"mirror/internal/infra/cacheproxy"
	"mirror/internal/infra/config"
	"mirror/internal/infra/logger"
	"mirror/internal/mirror"
	"mirror/internal/model"
	dbModel "mirror/internal/model/database"
	"mirror/internal/repository"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"go.uber.org/zap"
)

// mirrorLookupTTL bounds how long the resolved mirror list is reused when
// deciding whether a path belongs to a mirror.
const mirrorLookupTTL = 30 * time.Second

// RegisterMirrorProxy mounts mirror content at /{key}/...
//
// Content is streamed through the caching proxy (httpcached), which is where
// cache rules, admission control, range requests and stale serving live. This
// handler only decides whether the path belongs to a mirror we serve, and keeps
// upstream redirects pointing at our own host.
//
// Register it after the API routes and before the frontend static handler: a
// path that matches no mirror falls through to the SPA shell, so the frontend
// can render its own not-found page.
func RegisterMirrorProxy(app *fiber.App) {
	cfg := config.Get().Mirror
	if !cfg.Proxy {
		return
	}

	client := cacheproxy.Client()
	if client == nil {
		logger.L.Warn("mirror proxy is enabled but the caching proxy client is unavailable; /{key}/ will not be served")
		return
	}

	proxy := newMirrorReverseProxy(client, cfg.CacheHost)
	if proxy == nil {
		return
	}

	handler := adaptor.HTTPHandler(proxy)

	app.All("/*", func(c fiber.Ctx) error {
		if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
			return c.Next()
		}

		path := c.Path()

		// Reserved prefixes for the API and for our own browser UI.
		//
		// "/mirror/..." is the frontend's directory browser: it must render our
		// own page (fed by /api/v1/list/...), not stream the upstream's HTML.
		// Real files live at "/{key}/...", which is what the browser links to.
		if path == apiPrefix || strings.HasPrefix(path, apiPrefix+"/") {
			return c.Next()
		}
		if path == browserPrefix || strings.HasPrefix(path, browserPrefix+"/") {
			return c.Next()
		}

		key, _, ok := splitMirrorPath(path)
		if !ok {
			return c.Next()
		}

		mirrorItem, ok := lookupMirror(key)
		if !ok {
			// Unknown key: let the frontend route answer with its own page.
			return c.Next()
		}

		if !servedOverHTTP(mirrorItem.Type) {
			return model.Resp(c, http.StatusNotImplemented, any(nil), "this mirror is not served over HTTP")
		}

		// A malformed source would make every request fail downstream; catch it
		// here where the error is still attributable to the mirror entry.
		if _, err := mirror.ParseUpstream(mirrorItem.Source, mirrorItem.Key); err != nil {
			logger.L.Error("mirror source is not usable for proxying",
				zap.String("key", mirrorItem.Key),
				zap.Error(err),
			)
			return model.Resp(c, http.StatusInternalServerError, any(nil), "this mirror is misconfigured")
		}

		// Hand the resolved mirror to the reverse proxy through a header.
		//
		// Rewriting the request URI here would not work: the GoFiber -> net/http
		// adaptor parses the raw request URI into URL.Path, while fasthttp's
		// SetRequestURI only updates its own URI buffer, so the reverse proxy
		// would see the old path and prepend the mirror key twice.
		c.Request().Header.Set(mirrorKeyHeader, mirrorItem.Key)

		return handler(c)
	})
}

// browserPrefix is the path space owned by the frontend's directory browser.
const browserPrefix = "/mirror"

// mirrorKeyHeader carries the resolved mirror key from the GoFiber handler to the
// reverse proxy. It is added by us and stripped before the request is forwarded.
const mirrorKeyHeader = "X-Lida-Mirror-Key"

// canonicalMirrorPath joins a mirror key with the path inside it.
//
// "/ubuntu" and "/dists/noble" become "/ubuntu/dists/noble"; an empty or
// root-only remainder keeps the trailing slash, because the upstream treats
// "/ubuntu/" (the directory index) and "/ubuntu" (a redirect) differently.
func canonicalMirrorPath(key string, rest string) string {
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return "/" + key + "/"
	}

	return "/" + key + "/" + rest
}

// newMirrorReverseProxy builds the http.ReverseProxy that talks to httpcached.
func newMirrorReverseProxy(client *cacheclient.Client, cacheHost string) *httputil.ReverseProxy {
	cacheURL, err := url.Parse(client.CachePath("/"))
	if err != nil {
		logger.L.Error("failed to build the cache target URL", zap.Error(err))
		return nil
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			// The GoFiber handler resolved which mirror this path belongs to and
			// passed it in this header; the visible path is the rest.
			mirrorKey := request.In.Header.Get(mirrorKeyHeader)

			cachePath := request.In.URL.Path
			if mirrorKey != "" {
				rest := strings.TrimPrefix(request.In.URL.Path, "/"+mirrorKey)
				cachePath = canonicalMirrorPath(mirrorKey, rest)
			}

			// We need the cache to receive `cachePath`, and we already know the
			// exact path, so the incoming path is replaced before SetURL rather
			// than joined with a target path: ProxyRequest.SetURL appends the
			// target's path to the request's own path (joinURLPath), which
			// would duplicate it.
			request.Out.URL.Path = cachePath
			request.Out.URL.RawPath = ""

			target := *cacheURL
			target.Path = ""
			target.RawPath = ""
			target.RawQuery = ""

			request.SetURL(&target)

			// httpcached selects its virtual host by Host header, so it must be
			// a name that appears in its sites[].hosts list.
			if cacheHost != "" {
				request.Out.Host = cacheHost
			} else {
				request.Out.Host = cacheURL.Host
			}

			request.Out.Header.Set("User-Agent", "LidaMirror/1.0.0 (mirror-proxy)")

			// Internal marker and per-user credentials must never leave us.
			request.Out.Header.Del(mirrorKeyHeader)
			request.Out.Header.Del("Cookie")
			request.Out.Header.Del("Authorization")
		},
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			MaxIdleConns:          256,
			MaxIdleConnsPerHost:   64,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			WriteBufferSize:       64 << 10,
			ReadBufferSize:        64 << 10,
		},
		FlushInterval: 200 * time.Millisecond,
		ErrorHandler: func(writer http.ResponseWriter, request *http.Request, err error) {
			logger.L.Error("mirror proxy failed",
				zap.String("path", request.URL.Path),
				zap.Error(err),
			)
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusBadGateway)
			_, _ = writer.Write([]byte(`{"code":502,"msg":"the caching proxy or the upstream is unreachable","data":null}`))
		},
	}

	// Keep upstream redirects inside our namespace: "Location:
	// https://mirrors.tuna.tsinghua.edu.cn/ubuntu/dists/" would take the user
	// off-site and bypass the cache.
	proxy.ModifyResponse = func(response *http.Response) error {
		rewriteRedirect(response, cacheURL, cacheHost)

		// Shared cache entries must never carry per-user cookies.
		response.Header.Del("Set-Cookie")

		return nil
	}

	return proxy
}

// rewriteRedirect maps a redirect that points back at the cache onto our host.
//
// httpcached leaves upstream locations untouched only when they are relative;
// an absolute location naming the cache (our own prefix) is turned into a path,
// which keeps the browser on this site and inside the cache.
func rewriteRedirect(response *http.Response, cacheURL *url.URL, cacheHost string) {
	location := response.Header.Get("Location")
	if location == "" {
		return
	}

	parsed, err := url.Parse(location)
	if err != nil {
		return
	}

	if parsed.Host == "" {
		// Already relative: nothing to rewrite.
		return
	}

	pointsAtCache := strings.EqualFold(parsed.Host, cacheURL.Host)
	if cacheHost != "" {
		pointsAtCache = pointsAtCache || strings.EqualFold(parsed.Host, cacheHost)
	}

	if pointsAtCache {
		rewritten := parsed.Path
		if parsed.RawQuery != "" {
			rewritten += "?" + parsed.RawQuery
		}
		response.Header.Set("Location", rewritten)
	}
}

// splitMirrorPath splits "/ubuntu/dists/noble" into ("ubuntu", "/dists/noble").
func splitMirrorPath(requestPath string) (string, string, bool) {
	trimmed := strings.TrimPrefix(requestPath, "/")
	if trimmed == "" {
		return "", "", false
	}

	key, rest, found := strings.Cut(trimmed, "/")
	if key == "" {
		return "", "", false
	}

	// The key becomes part of a URL we build, so keep it to a safe shape.
	for _, r := range key {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if !allowed {
			return "", "", false
		}
	}

	if !found {
		// "/ubuntu" and "/ubuntu/" both mean the mirror root; the trailing
		// slash matters to the upstream, so keep it.
		if strings.HasSuffix(requestPath, "/") {
			return key, "/", true
		}
		return key, "", true
	}

	return key, "/" + rest, true
}

// mirrorLookup caches the key -> mirror map used for prefix resolution.
var mirrorLookup = struct {
	mu      sync.RWMutex
	items   map[string]dbModel.MirrorList
	fetched time.Time
}{items: map[string]dbModel.MirrorList{}}

// lookupMirror resolves a mirror key to its row, refreshing periodically so a
// newly added mirror starts working without a restart.
func lookupMirror(key string) (dbModel.MirrorList, bool) {
	normalized := strings.ToLower(key)

	mirrorLookup.mu.RLock()
	fresh := time.Since(mirrorLookup.fetched) < mirrorLookupTTL
	item, ok := mirrorLookup.items[normalized]
	mirrorLookup.mu.RUnlock()

	if fresh {
		return item, ok
	}

	refreshMirrorLookup()

	mirrorLookup.mu.RLock()
	defer mirrorLookup.mu.RUnlock()

	item, ok = mirrorLookup.items[normalized]
	return item, ok
}

// refreshMirrorLookup reloads the key -> mirror map.
//
// On failure the previous map is kept: a database blip must not turn every
// mirror into a 404.
func refreshMirrorLookup() {
	repo := repository.NewMirrorListRepository()

	mirrors, err := repo.ReadAllMirrors()
	if err != nil {
		logger.L.Error("failed to refresh the mirror lookup table", zap.Error(err))
		return
	}

	items := make(map[string]dbModel.MirrorList, len(mirrors))
	for _, item := range mirrors {
		items[strings.ToLower(item.Key)] = item
	}

	mirrorLookup.mu.Lock()
	mirrorLookup.items = items
	mirrorLookup.fetched = time.Now()
	mirrorLookup.mu.Unlock()
}

// servedOverHTTP reports whether a mirror type can be proxied over HTTP.
func servedOverHTTP(mirrorType dbModel.MirrorType) bool {
	switch mirrorType {
	case dbModel.MirrorTypeReverseProxy, dbModel.MirrorTypeHttp, dbModel.MirrorTypeHttps:
		return true
	default:
		return false
	}
}
