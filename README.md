# mirror-backend

[English](./README.md) · [简体中文](./README.zh-CN.md)

Backend of Lida Mirror (the Shanghai Lida University open source mirror): mirror
list and sync-status aggregation, static hosting for the frontend, mirror
directory proxying through [httpcached](https://github.com/ghink/httpcached),
and parsing of upstream directory listings.

## Stack

Go 1.27 · [Fiber v3](https://gofiber.io/) · [Xorm](https://xorm.io/) (PostgreSQL) ·
Redis (through `go.gh.ink/cask`) · zap · gocron

## Layout

```
main.go                     Entry point: config → logger → database → cache → cron → HTTP
config.yaml                 Static config (server / log / database / cache / web / mirror)
database.sql                PostgreSQL schema
internal/
  adapter/                  Upstream sync-status adapters
    adapter.go              ParseStatus: resolve the upstream, then read its status
    parser/upstream.go      Upstream registry: host + status document path per site
    parser/tunasync.go      tunasync.json decoding + document-level cache
    parser/status_format.go Per-format decoders (tunasync, USTC)
    parser/utils.go         Human-readable size → bytes
  cli/                      One-shot maintenance commands (-dump-cache-config / -drop-caches)
  cacheclient/              HTTP client for httpcached (the only outbound path)
  cron/cron.go              Refreshes sync status hourly
  handler/
    fiber.go                Fiber app assembly, middleware, root routes
    route.go                /api/v1 route registration
    v1/v1.go                API v1 endpoints
    web.go                  Frontend static hosting (web.dir, SPA fallback, cache headers)
    mirror_proxy.go         /{key}/... content proxy (streaming, Range, Location rewriting)
  infra/{cache,config,database,logger}   Infrastructure
  infra/cacheproxy/         Process-wide holder of the httpcached client
  mirror/                   source + key → upstream directory; proxy route generation
  parser/directory/         Upstream listing parsers (nginx fancyindex / Apache autoindex)
  proxyconfig/              Generates the httpcached configuration from the database
  middleware/header.go      Custom response headers
  meta/                     Version, user agent, site name constants
  model/                    Request/response/database models and the response envelope
  repository/               Data access (PostgreSQL + Redis)
  service/v1/               Mirror list, status refresh, directory listing logic
scripts/dev.sh              Local development: dependency containers + caching proxy
```

## Quick start

```bash
# 1) Dependencies: a reachable PostgreSQL and Redis
# 2) Schema
psql -U <user> -d <db> -f database.sql

# 3) Configuration
cp config.yaml config_debug.yaml     # local overrides; git-ignored
#    fill in database.host/port/user/name/pass and cache.host/port
#    web.dir points at the built frontend (default "web", relative to the working directory)

# 4) Add a mirror (example)
psql -U <user> -d <db> -c "INSERT INTO mirror_list (key, comment, type, source)
  VALUES ('ubuntu', 'Ubuntu 发行版', 'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn');"

# 5) Run
go run .

# 6) Verify
curl -s localhost:8000/api/v1/mirrors.json | head -c 400
```

With `database.*` left empty the process exits with an explicit log line
(`dbname is empty`); that is intended, not a crash.

## Configuration

| Key                         | Default              | Meaning                                                        |
| --------------------------- | -------------------- | -------------------------------------------------------------- |
| `server.host` / `port`      | `0.0.0.0:8000`       | Listen address                                                 |
| `log.file.all` / `err`      | —                    | Log files; empty means stdout only (recommended when developing) |
| `database.host` / `port`    | `127.0.0.1:5432`     | PostgreSQL connection                                          |
| `cache.host` / `port`       | `127.0.0.1:6379`     | Redis connection                                               |
| `web.dir`                   | `web`                | Built frontend directory, relative to the working directory; a missing directory only logs a warning |
| `mirror.proxy`              | `true`               | Enable the `/{key}/...` content proxy and directory browsing    |
| `mirror.cache_addr`         | `127.0.0.1:8001`     | httpcached address                                             |
| `mirror.cache_host`         | empty (uses `cache_addr`) | Host header sent to the cache; must appear in its `sites[].hosts` |
| `mirror.cache_scheme`       | `http`               | Scheme used towards the cache                                  |
| `mirror.cache_dir`          | `./cache_data`       | Where the caching proxy stores blobs; put it on the big disk    |
| `mirror.cache_max_size`     | `200GB`              | The proxy's soft disk cap, above which it evicts                |
| `mirror.cache_config`       | empty                | Path of the proxy's config file; lets `-dump-cache-config` default to it and makes startup warn when it is missing |
| `mirror.host`               | empty                | Public hostname of this site; used to build the proxy's host list |
| `mirror.cors_allow_origins` | empty                | Browser origins allowed to call the API cross-origin; needs the dev server's port when using `pnpm dev` |

When `config_debug.yaml` exists it overrides `config.yaml` and enables debug
logging. `SIGHUP` reloads the configuration.

## API

```
GET /api/v1/mirrors.json                      Mirror list + sync status (ETag / 304)
GET /api/v1/mirrors/:id/status.json           One mirror's status (queries upstream on a cache miss)
GET /api/v1/list/:key/mirrors.json            File listing of a mirror's root directory
GET /api/v1/list/:key/<sub/dir>/mirrors.json  Listing of a subdirectory (any depth)
GET /ping                                     Liveness check
```

Who owns which path:

| Path                      | Owner             | Notes                                                        |
| ------------------------- | ----------------- | ------------------------------------------------------------ |
| `/mirror/{key}/{...path}` | Frontend browser  | Our own styling; data comes from `/api/v1/list/...`            |
| `/{key}/{...path}`        | Caching proxy     | Real files, through httpcached; supports Range and 304         |
| `/api/**`                 | JSON API          | Never swallowed by the frontend or by the proxy                |

Response fields and the status enum are documented in
[`../docs/README.md`](../docs/README.md).

## Command line

```bash
go run .                                   # start the server
go run . -dump-cache-config proxy.yaml     # generate the proxy config from the database, then exit
go run . -drop-caches                      # drop the cached list and status, then exit
```

Both commands live in `internal/cli`. `main` initialises only the dependencies a
command declares (`Needs`): generating the proxy configuration reads the
database and does not need Redis, dropping the cache needs both. That keeps
`main` a straight line of initialisation steps, and adding a command does not
touch the flag-parsing code.

An edit to `mirror_list` only becomes visible after `-drop-caches`: the mirror
list is cached for 24 hours and the status snapshot is rewritten hourly by cron.

## Development and testing

```bash
./scripts/dev.sh up        # start PostgreSQL + Redis (Docker, with sample data)
./scripts/dev.sh proxy     # start the caching proxy (routes generated from the database)
go run .                   # start the backend

go build ./... && go vet ./... && go test ./...
gofmt -l .                 # no output expected
```

scripts/dev.sh also provides: `up | down | destroy | reset | status | logs | proxy |
probe | drop | audit`. The database on 55433, Redis on 56380 and the caching proxy on 8001.

The built frontend goes into `web.dir` (see [`../docs/README.md`](../docs/README.md)).

## Licence

See [LICENSE](./LICENSE).
