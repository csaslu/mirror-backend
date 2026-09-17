package handler

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"mirror/internal/infra/config"
	"mirror/internal/infra/logger"
	"mirror/internal/model"

	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

// apiPrefix is the route prefix reserved for JSON APIs. Nothing below it may
// ever fall through to the static frontend handler.
const apiPrefix = "/api"

// immutableAssetPrefix mirrors Nuxt's build output: every file under /_nuxt/
// carries a content hash in its name, so it can be cached forever.
const immutableAssetPrefix = "/_nuxt/"

// spaFallbacks are tried in order for client-side routes that have no file on
// disk. Nuxt emits 200.html (SPA fallback) and app.html (shell) depending on
// the version and config.
var spaFallbacks = []string{"200.html", "app.html", "index.html"}

// RegisterStatic mounts the built frontend (Nuxt static output) at the root
// path. The files are read from disk at runtime (config `web.dir`), so the
// frontend can be replaced without rebuilding this binary.
//
// Routing layout:
//
//	/api/...  -> JSON APIs, registered before this handler and never shadowed
//	/...      -> files from web.dir
//	/<misc>   -> SPA fallback from web.dir (client-side routed pages)
//
// Files are streamed through c.SendFile with a path relative to an fs.FS root,
// which is what makes GoFiber v3 resolve them. The ready-made static middleware
// cannot be used here: it rewrites the request path in a way that never matches
// an fs.FS whose root is "." (see internal/handler/web_test.go).
func RegisterStatic(app *fiber.App) {
	dir := strings.TrimSpace(config.Get().Web.Dir)
	if dir == "" {
		logger.L.Warn("web.dir is empty, frontend will not be served")
		return
	}

	root := dir
	if !filepath.IsAbs(root) {
		if cwd, err := os.Getwd(); err == nil {
			root = filepath.Join(cwd, root)
		}
	}
	root = filepath.Clean(root)

	// Fail loudly in the log at startup: a wrong web.dir otherwise shows up as
	// a mysterious 404 on every page.
	switch info, err := os.Stat(root); {
	case err != nil:
		logger.L.Warn("frontend directory is not readable, frontend will not be served",
			zap.String("dir", root), zap.Error(err))
		return
	case !info.IsDir():
		logger.L.Warn("web.dir is not a directory, frontend will not be served",
			zap.String("dir", root))
		return
	case !hasIndex(root):
		logger.L.Warn("no index.html in web.dir, build the frontend first",
			zap.String("dir", root))
	}

	fsys := os.DirFS(root)

	app.Use(func(c fiber.Ctx) error {
		// Only GET and HEAD can be a file or a page; anything else belongs to
		// the API router.
		if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
			return c.Next()
		}

		// Keep the API surface JSON-only; without this an unknown /api path
		// would be answered with the SPA shell and the frontend would fail
		// while parsing HTML as JSON. HEAD requests land here too, because the
		// API routes are registered for GET only.
		if c.Path() == apiPrefix || strings.HasPrefix(c.Path(), apiPrefix+"/") {
			return model.RespNotFound(c)
		}

		// path.Clean collapses "..", so a traversal attempt cannot escape root.
		rel := strings.TrimPrefix(path.Clean("/"+c.Path()), "/")

		// 1. The exact file requested.
		if isRegularFile(fsys, rel) {
			return sendAsset(c, fsys, rel)
		}

		// 2. A directory request ("/", "/en/", "/mirror/ubuntu/") maps to its
		//    index file.
		if rel == "" || strings.HasSuffix(c.Path(), "/") {
			if index := path.Join(rel, "index.html"); isRegularFile(fsys, index) {
				return sendAsset(c, fsys, index)
			}
		}

		// 3. A missing asset stays a 404: an extension in the last segment means
		//    "file", so it is never a client-side route. This keeps "/nope.css"
		//    and "/_nuxt/missing.js" from being answered with the app shell.
		base := path.Base(rel)
		if base != "." && strings.Contains(base, ".") {
			return model.RespNotFound(c)
		}

		// 4. A client-side route has no file on disk: hand over the SPA shell.
		//    A trailing slash means the visitor asked for a directory, so that
		//    still requires a real index file (step 2).
		if !strings.HasSuffix(c.Path(), "/") {
			for _, candidate := range spaFallbacks {
				if isRegularFile(fsys, candidate) {
					return sendAsset(c, fsys, candidate)
				}
			}
		}

		return model.RespNotFound(c)
	})

	logger.L.Info("frontend static files mounted", zap.String("dir", root))
}

// sendAsset streams one file from the frontend directory with cache headers
// tuned to its kind.
func sendAsset(c fiber.Ctx, fsys fs.FS, rel string) error {
	if strings.HasPrefix(c.Path(), immutableAssetPrefix) {
		c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
	} else {
		// HTML and other unhashed files must be revalidated so a new frontend
		// build becomes visible without a hard refresh.
		c.Set(fiber.HeaderCacheControl, "no-cache")
	}

	c.Type(path.Ext(rel))

	return c.SendFile(rel, fiber.SendFile{
		FS:        fsys,
		ByteRange: true,
	})
}

// isRegularFile reports whether rel names a regular file inside fsys.
func isRegularFile(fsys fs.FS, rel string) bool {
	if rel == "" {
		return false
	}

	info, err := fs.Stat(fsys, rel)
	return err == nil && info.Mode().IsRegular()
}

// hasIndex reports whether a directory holds an index.html.
func hasIndex(root string) bool {
	info, err := os.Stat(filepath.Join(root, "index.html"))
	return err == nil && info.Mode().IsRegular()
}
