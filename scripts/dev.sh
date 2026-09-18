#!/usr/bin/env bash
#
# Local development stack for the mirror backend.
#
# Starts PostgreSQL and Redis for local work, creates the schema and seeds a
# mirror list, then tells you how to run the server.
#
#   ./scripts/dev.sh up        start the dependencies (idempotent)
#   ./scripts/dev.sh down      stop them, keep the data
#   ./scripts/dev.sh destroy   remove containers AND data volumes
#   ./scripts/dev.sh reset     recreate the schema and re-seed the sample list
#   ./scripts/dev.sh status    show what is running
#   ./scripts/dev.sh logs      follow the PostgreSQL log
#   ./scripts/dev.sh proxy     start the caching proxy (httpcached) on :8001
#   ./scripts/dev.sh probe     print cache statistics and route count
#   ./scripts/dev.sh drop      drop the backend's Redis caches
#   ./scripts/dev.sh audit     check every mirror against its upstream's status document
#
# The caching proxy is downloaded from the published release rather than built,
# because building it pulls its own dependency tree. Set HTTPCACHED_BIN to use a
# binary you built yourself.
#
# The ports deliberately differ from the defaults (5432/6379) so this stack
# never collides with an existing local PostgreSQL or Redis.

set -euo pipefail

PG_CONTAINER=csaslu-pg-dev
REDIS_CONTAINER=csaslu-redis-dev
PG_VOLUME=csaslu-pg-data
REDIS_VOLUME=csaslu-redis-data

PG_PORT=55433
REDIS_PORT=56380
PROXY_PORT=8001

PG_USER=mirror
PG_PASSWORD=testpass
PG_DATABASE=mirror

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(dirname "$SCRIPT_DIR")"

# The proxy's own files (binary, config, cached blobs). Kept outside the repo
# because the blob store grows to gigabytes.
PROXY_DIR="${PROXY_DIR:-$BACKEND_DIR/.cache-proxy}"
HTTPCACHED_BIN="${HTTPCACHED_BIN:-$PROXY_DIR/httpcached}"
HTTPCACHED_URL="${HTTPCACHED_URL:-https://source.gh.ink/httpcached/httpcached-v1.1.0-darwin-arm64}"
HTTPCACHED_REPO="${HTTPCACHED_REPO:-https://github.com/ghink/httpcached.git}"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }

# Every external step is bounded: a hanging download or build would otherwise
# look like the script itself is broken.
NET_TIMEOUT="${NET_TIMEOUT:-60}"
BUILD_TIMEOUT="${BUILD_TIMEOUT:-300}"

# timeout(1) is not present on a stock macOS; fall back to plain execution.
bounded() {
  local seconds="$1"
  shift

  if command -v timeout >/dev/null 2>&1; then
    timeout "$seconds" "$@"
    return
  fi
  if command -v gtimeout >/dev/null 2>&1; then
    gtimeout "$seconds" "$@"
    return
  fi

  "$@"
}

fetch_release() {
  curl -fsSL --max-time "$NET_TIMEOUT" "$HTTPCACHED_URL" -o "$HTTPCACHED_BIN" 2>/dev/null
}

build_from_source() {
  command -v go >/dev/null 2>&1 || return 1

  local tmp
  tmp="$(mktemp -d)"

  if bounded "$NET_TIMEOUT" git clone --depth 1 "$HTTPCACHED_REPO" "$tmp/src" >/dev/null 2>&1 \
    && (cd "$tmp/src" && bounded "$BUILD_TIMEOUT" go build -o "$HTTPCACHED_BIN" . >/dev/null 2>&1); then
    rm -rf "$tmp"
    return 0
  fi

  rm -rf "$tmp"
  return 1
}
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }

require_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    echo "docker is required but was not found in PATH" >&2
    exit 1
  fi
  if ! docker info >/dev/null 2>&1; then
    echo "the docker daemon is not reachable" >&2
    exit 1
  fi
}

pg_ready() {
  docker exec "$PG_CONTAINER" pg_isready -U "$PG_USER" -d "$PG_DATABASE" >/dev/null 2>&1
}

wait_for_pg() {
  for _ in $(seq 1 60); do
    if pg_ready; then return 0; fi
    sleep 1
  done
  echo "PostgreSQL did not become ready in time" >&2
  return 1
}

start_dependencies() {
  require_docker

  if ! docker container inspect "$PG_CONTAINER" >/dev/null 2>&1; then
    log "creating PostgreSQL container ($PG_CONTAINER) on port $PG_PORT"
    # postgres:18+ wants the volume at /var/lib/postgresql (the cluster lives in
    # a version-specific subdirectory). Mounting /var/lib/postgresql/data makes
    # the entrypoint refuse to start with "data in unused mount/volume".
    docker run -d \
      --name "$PG_CONTAINER" \
      -e POSTGRES_USER="$PG_USER" \
      -e POSTGRES_PASSWORD="$PG_PASSWORD" \
      -e POSTGRES_DB="$PG_DATABASE" \
      -p "127.0.0.1:$PG_PORT:5432" \
      -v "$PG_VOLUME:/var/lib/postgresql" \
      postgres:18-alpine >/dev/null
  else
    log "starting PostgreSQL container ($PG_CONTAINER)"
    docker start "$PG_CONTAINER" >/dev/null
  fi

  wait_for_pg
  log "PostgreSQL is ready on 127.0.0.1:$PG_PORT"

  if ! docker container inspect "$REDIS_CONTAINER" >/dev/null 2>&1; then
    log "creating Redis container ($REDIS_CONTAINER) on port $REDIS_PORT"
    docker run -d \
      --name "$REDIS_CONTAINER" \
      -p "127.0.0.1:$REDIS_PORT:6379" \
      -v "$REDIS_VOLUME:/data" \
      redis:7-alpine >/dev/null
  else
    log "starting Redis container ($REDIS_CONTAINER)"
    docker start "$REDIS_CONTAINER" >/dev/null
  fi

  log "Redis is ready on 127.0.0.1:$REDIS_PORT"
}

apply_schema() {
  log "creating schema (safe to re-run: drops and recreates the table)"

  docker exec -i "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -q <<'SQL'
DROP TABLE IF EXISTS mirror_list;
DROP TYPE IF EXISTS mirror_type;

CREATE TYPE mirror_type AS ENUM (
    'reverse_proxy', 'rsync',
    'http', 'https',
    'ftp', 's3'
);

CREATE TABLE mirror_list (
    id         SERIAL        PRIMARY KEY,
    key        TEXT          NOT NULL,
    comment    TEXT,
    type       mirror_type   NOT NULL,
    source     TEXT          NOT NULL
);
SQL
}

# Mirrors whose existence in the upstream's own status document has been
# verified, and whose source URL is the real upstream (key == upstream
# directory name, which the backend enforces). Nothing here is invented: the
# status and size the site shows come from these upstreams.
#
#   TUNA  https://mirrors.tuna.tsinghua.edu.cn/static/tunasync.json
#   NJU   https://mirror.nju.edu.cn/configs/tunasync.json
#   USTC  https://mirrors.ustc.edu.cn/status/json
seed_mirrors() {
  log "seeding verified mirrors (TUNA / NJU / USTC)"

  docker exec -i "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -q <<'SQL'
INSERT INTO mirror_list (key, comment, type, source) VALUES
 -- 清华 TUNA
 ('ubuntu',              'Ubuntu 发行版',              'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('ubuntu-ports',        'Ubuntu Ports（ARM 等架构）',  'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('ubuntu-releases',     'Ubuntu 安装镜像',            'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('debian',              'Debian 发行版',              'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('debian-security',     'Debian 安全更新',            'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('archlinux',           'Arch Linux 发行版',          'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('centos',              'CentOS 发行版',              'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('centos-vault',        'CentOS Vault 归档',          'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('fedora',              'Fedora 发行版',              'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('openeuler',           'openEuler 发行版',           'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('docker-ce',           'Docker CE 软件源',           'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('kubernetes',          'Kubernetes 软件源',          'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('pypi',                'Python 软件包索引（PyPI）',   'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('anaconda',            'Anaconda 软件源',            'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('homebrew',            'Homebrew 软件源',            'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('nodejs-release',      'Node.js 发行版',             'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 ('openwrt',             'OpenWrt 发行版',             'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn'),
 -- 南京大学 NJU
 ('fedora-archive',      'Fedora 归档',                'reverse_proxy', 'https://mirror.nju.edu.cn'),
 ('ubuntu-old-releases', 'Ubuntu 旧版本',              'reverse_proxy', 'https://mirror.nju.edu.cn'),
 ('iina',                'IINA 播放器',                'reverse_proxy', 'https://mirror.nju.edu.cn'),
 ('almalinux-elevate',   'AlmaLinux Elevate',          'reverse_proxy', 'https://mirror.nju.edu.cn'),
 ('nix-channels',        'Nix Channels',               'reverse_proxy', 'https://mirror.nju.edu.cn'),
 ('elpa',                'GNU ELPA',                   'reverse_proxy', 'https://mirror.nju.edu.cn'),
 -- 中国科学技术大学 USTC
 ('alpine',              'Alpine Linux（USTC 上游）',   'reverse_proxy', 'https://mirrors.ustc.edu.cn'),
 ('ceph',                'Ceph 软件源（USTC 上游）',    'reverse_proxy', 'https://mirrors.ustc.edu.cn'),
 ('jenkins',             'Jenkins 软件源（USTC 上游）', 'reverse_proxy', 'https://mirrors.ustc.edu.cn'),
 ('raspbian',            'Raspbian（USTC 上游）',       'reverse_proxy', 'https://mirrors.ustc.edu.cn');

SELECT count(*) AS seeded FROM mirror_list;
SQL
}

case "${1:-up}" in
  up)
    start_dependencies

    if ! docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -tAc \
      "SELECT to_regclass('public.mirror_list')" 2>/dev/null | grep -q mirror_list; then
      apply_schema
      seed_mirrors
    else
      log "schema already present, leaving the data alone (use 'reset' to reseed)"
    fi

    echo
    log "dependencies are up. Now run the server:"
    echo
    echo "    cd $BACKEND_DIR"
    echo "    go run .          # uses config_debug.yaml, listens on 127.0.0.1:18000"
    echo
    echo "  then open http://127.0.0.1:18000/"
    echo
    echo "  config_debug.yaml must point at these ports:"
    echo "    database.port: $PG_PORT   cache.port: $REDIS_PORT   web.dir: \"web\""
    ;;

  down)
    require_docker
    docker stop "$PG_CONTAINER" "$REDIS_CONTAINER" >/dev/null 2>&1 || true
    log "stopped (data kept in volumes $PG_VOLUME and $REDIS_VOLUME)"
    ;;

  destroy)
    require_docker
    docker rm -f "$PG_CONTAINER" "$REDIS_CONTAINER" >/dev/null 2>&1 || true
    docker volume rm "$PG_VOLUME" "$REDIS_VOLUME" >/dev/null 2>&1 || true
    pkill -f "$HTTPCACHED_BIN" >/dev/null 2>&1 || true
    log "containers, volumes and the caching proxy stopped"
    ;;

  reset)
    require_docker
    docker start "$PG_CONTAINER" >/dev/null 2>&1 || start_dependencies
    wait_for_pg
    apply_schema
    seed_mirrors
    log "database reset"
    ;;

  proxy)
    mkdir -p "$PROXY_DIR"

    if [ ! -x "$HTTPCACHED_BIN" ]; then
      log "obtaining the caching proxy (binary at $HTTPCACHED_BIN)"
      # The published release is the fast path; a source build is the fallback
      # for platforms without a release (it needs the Go module proxy, which the
      # build honours through GOPROXY, e.g. https://goproxy.cn).
      if ! fetch_release; then
        warn "no published release for this platform, building from source"
        build_from_source || {
          echo "could not obtain the caching proxy." >&2
          echo "Build it yourself and point HTTPCACHED_BIN at the result." >&2
          exit 1
        }
      fi
      chmod +x "$HTTPCACHED_BIN" 2>/dev/null || true
    fi

    # Routes are generated from the mirror list, never hand-written.
    log "generating the proxy configuration from the database"
    (cd "$BACKEND_DIR" && go run . -dump-cache-config "$PROXY_DIR/config.yaml" >/dev/null)

    # A process is not the same as a working proxy. A leftover instance bound to
    # a different port (or one whose working directory has been removed) would
    # make the branch below send SIGHUP to something that never answers, and the
    # only symptom would be "the proxy is not answering yet".
    if pgrep -f "$HTTPCACHED_BIN" >/dev/null 2>&1; then
      if ! curl -fsS --max-time 2 "http://127.0.0.1:$PROXY_PORT/_cache/stats" >/dev/null 2>&1; then
        warn "a process matches $HTTPCACHED_BIN but nothing answers on 127.0.0.1:$PROXY_PORT"
        warn "stopping the stale instance so a working one can start"
        pkill -f "$HTTPCACHED_BIN" >/dev/null 2>&1 || true
        sleep 1
      fi
    fi

    if pgrep -f "$HTTPCACHED_BIN" >/dev/null 2>&1; then
      # A running proxy re-reads its routes on SIGHUP and keeps its cached data.
      log "reloading the running proxy"
      pkill -HUP -f "$HTTPCACHED_BIN"
    else
      log "starting the caching proxy on 127.0.0.1:$PROXY_PORT"
      (cd "$PROXY_DIR" && nohup "$HTTPCACHED_BIN" >"$PROXY_DIR/proxy.log" 2>&1 </dev/null &)

      for _ in $(seq 1 20); do
        if curl -fsS --max-time 2 "http://127.0.0.1:$PROXY_PORT/_cache/stats" >/dev/null 2>&1; then
          break
        fi
        sleep 0.5
      done
    fi

    log "cache statistics:"
    curl -s --max-time 5 "http://127.0.0.1:$PROXY_PORT/_cache/stats" || warn "the proxy is not answering yet"
    echo
    ;;

  probe)
    curl -s --max-time 5 "http://127.0.0.1:$PROXY_PORT/_cache/stats" | python3 -m json.tool 2>/dev/null \
      || curl -s --max-time 5 "http://127.0.0.1:$PROXY_PORT/_cache/stats"
    echo
    ;;

  audit)
    # Every mirror we list is checked against the upstream's own status
    # document, so a typo or an invented row cannot survive unnoticed.
    require_docker

    rows="$(docker exec -i "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DATABASE" -tAc \
      "SELECT key||'|'||type||'|'||source FROM mirror_list ORDER BY id;")"

    PYTHONPATH="" python3 - "$rows" <<'PYAUDIT'
import json, sys, urllib.request

def load(url):
    req = urllib.request.Request(url, headers={"User-Agent": "LidaMirror/1.0.0 (audit)"})
    try:
        return json.loads(urllib.request.urlopen(req, timeout=25).read())
    except Exception as exc:  # a site being down must not abort the audit
        print(f"  ! {url}: {exc}")
        return []

pools = [
    ("TUNA", "mirrors.tuna.tsinghua.edu.cn", "https://mirrors.tuna.tsinghua.edu.cn/static/tunasync.json"),
    ("NJU", "mirror.nju.edu.cn", "https://mirror.nju.edu.cn/configs/tunasync.json"),
    ("USTC", "mirrors.ustc.edu.cn", "https://mirrors.ustc.edu.cn/status/json"),
]

resolved = {}
for label, host, url in pools:
    names = {x["name"] for x in load(url)}
    resolved[host] = (label, names)
    print(f"上游 {label}: {len(names)} 个镜像")

lookup = {}
for label, host, _ in pools:
    lookup[host] = (label, resolved[host][1])

missing = []
rows = [r.strip() for r in sys.argv[1].splitlines() if r.strip()]

print()
print(f"{'key':<24}{'type':<15}{'上游':<7}{'存在?'}")
print("-" * 56)

for line in rows:
    key, mtype, source = line.split("|")
    host = source.split("//")[-1].split("/")[0]
    label, pool = lookup.get(host, ("未知", set()))
    found = key in pool
    if not found:
        missing.append(key)
    print(f"{key:<24}{mtype:<15}{label:<7}{'OK' if found else 'MISSING'}")

print()
print(f"共 {len(rows)} 条；上游文档里不存在的: {missing or '无'}")
PYAUDIT
    ;;

  drop)
    log "dropping the backend's Redis caches"
    (cd "$BACKEND_DIR" && go run . -drop-caches)
    ;;

  status)
    require_docker
    docker ps -a --filter "name=$PG_CONTAINER" --filter "name=$REDIS_CONTAINER" \
      --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
    if curl -fsS --max-time 2 "http://127.0.0.1:$PROXY_PORT/_cache/stats" >/dev/null 2>&1; then
      echo "httpcached: running on 127.0.0.1:$PROXY_PORT"
    elif pgrep -f "$HTTPCACHED_BIN" >/dev/null 2>&1; then
      echo "httpcached: a process exists but nothing answers on 127.0.0.1:$PROXY_PORT"
    else
      echo "httpcached: stopped"
    fi
    ;;

  logs)
    require_docker
    docker logs -f "$PG_CONTAINER"
    ;;

  *)
    warn "unknown command: $1"
    echo "usage: $0 {up|down|destroy|reset|status|logs|proxy|probe|drop|audit}" >&2
    exit 1
    ;;
esac
