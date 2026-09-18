# mirror-backend

[English](./README.md) · [简体中文](./README.zh-CN.md)

Lida Mirror（上海立达学院开源镜像站）的后端：镜像列表与同步状态聚合、前端静态托管、
镜像目录代理（经 [httpcached](https://github.com/ghink/httpcached) 按需缓存）、
以及上游目录页解析。

## 技术栈

Go 1.27 · [Fiber v3](https://gofiber.io/) · [Xorm](https://xorm.io/)（PostgreSQL）·
Redis（经 `go.gh.ink/cask`）· zap · gocron

## 目录结构

```
main.go                     进程入口：配置 → 日志 → 数据库 → 缓存 → cron → HTTP
config.yaml                 静态配置（server / log / database / cache / web / mirror）
database.sql                PostgreSQL 表结构
internal/
  adapter/                  上游同步状态适配层
    adapter.go              ParseStatus：先查上游注册表再取状态
    parser/upstream.go      上游注册表：每个站点的 host + 状态文档路径
    parser/tunasync.go      tunasync.json 解码 + 文档级缓存
    parser/status_format.go 按文档格式解码（tunasync / USTC）
    parser/utils.go         人类可读大小 → 字节
  cli/                      一次性运维命令（-dump-cache-config / -drop-caches）
  cacheclient/              与 httpcached 通信的 HTTP 客户端（唯一出网入口）
  cron/cron.go              每小时刷新一次同步状态
  handler/
    fiber.go                Fiber 应用装配、中间件、根路由
    route.go                /api/v1 路由注册
    v1/v1.go                API v1 端点
    web.go                  前端静态托管（读 web.dir，SPA 兜底，缓存头）
    mirror_proxy.go         /{key}/... 镜像内容代理（流式、Range、Location 重写）
  infra/{cache,config,database,logger}   基础设施
  infra/cacheproxy/         httpcached 客户端的进程级持有者
  mirror/                   source + key → 上游目录；代理路由表生成
  parser/directory/         上游目录页解析（nginx fancyindex / Apache autoindex）
  proxyconfig/              从数据库生成 httpcached 配置
  middleware/header.go      自定义响应头
  meta/                     版本、UA、站点名等常量
  model/                    请求/响应/数据库模型与统一响应信封
  repository/               数据访问（PostgreSQL + Redis 双层）
  service/v1/               镜像列表、状态刷新、目录列表的业务逻辑
scripts/dev.sh              本地开发：依赖容器 + 缓存代理
```

## 快速开始

```bash
# 1) 依赖：PostgreSQL 与 Redis 均可连
# 2) 建表
psql -U <user> -d <db> -f database.sql

# 3) 配置
cp config.yaml config_debug.yaml     # 本地调试用；被 .gitignore 忽略
#    填写 database.host/port/user/name/pass 与 cache.host/port
#    web.dir 指向前端构建产物目录（默认 "web"，相对工作目录）

# 4) 插入镜像（示例）
psql -U <user> -d <db> -c "INSERT INTO mirror_list (key, comment, type, source)
  VALUES ('ubuntu', 'Ubuntu 发行版', 'reverse_proxy', 'https://mirrors.tuna.tsinghua.edu.cn');"

# 5) 运行
go run .

# 6) 验证
curl -s localhost:8000/api/v1/mirrors.json | head -c 400
```

未填写 `database.*` 时进程会以明确的日志退出（`dbname is empty`），这是预期行为。

## 配置说明

| 配置项                      | 默认值               | 说明                                                         |
| --------------------------- | -------------------- | ------------------------------------------------------------ |
| `server.host` / `port`      | `0.0.0.0:8000`       | 监听地址                                                     |
| `log.file.all` / `err`      | —                    | 日志文件；为空则只输出 stdout（本地调试推荐留空）             |
| `database.host` / `port`    | `127.0.0.1:5432`     | PostgreSQL 连接                                              |
| `cache.host` / `port`       | `127.0.0.1:6379`     | Redis 连接                                                   |
| `web.dir`                   | `web`                | 前端构建产物目录，相对工作目录；不存在时只在日志里警告         |
| `mirror.proxy`              | `true`               | 是否启用 `/{key}/...` 镜像代理与目录浏览                       |
| `mirror.cache_addr`         | `127.0.0.1:8001`     | httpcached 的地址                                            |
| `mirror.cache_host`         | 空（用 `cache_addr`）| 访问缓存时的 Host 头，必须在其 `sites[].hosts` 中             |
| `mirror.cache_scheme`       | `http`               | 与缓存通信的协议                                              |
| `mirror.cache_dir`          | `./cache_data`       | 缓存块存放目录；应放在大容量磁盘上                              |
| `mirror.cache_max_size`     | `200GB`              | 缓存软上限，超过后开始淘汰                                     |
| `mirror.cache_config`       | 空                   | 代理配置文件的路径；设了它生成命令可省略参数，启动时会检查其是否存在 |
| `mirror.host`               | 空                   | 本站对外域名；用于生成代理 Host 列表                           |
| `mirror.cors_allow_origins` | 空                   | 允许跨源调用 API 的浏览器来源；`pnpm dev` 时需放行其端口       |

存在 `config_debug.yaml` 时它会覆盖 `config.yaml` 并打开 debug 日志；
`SIGHUP` 会重新加载配置。

## 接口

```
GET /api/v1/mirrors.json                      镜像列表 + 同步状态（ETag / 304）
GET /api/v1/mirrors/:id/status.json           单镜像状态（缓存未命中时回源查询）
GET /api/v1/list/:key/mirrors.json            镜像根目录的文件列表
GET /api/v1/list/:key/<sub/dir>/mirrors.json  子目录列表（任意层级）
GET /ping                                     存活检查
```

请求路径的分工：

| 路径                       | 归属         | 说明                                          |
| -------------------------- | ------------ | --------------------------------------------- |
| `/mirror/{key}/{...path}`  | 前端浏览页   | 我们的样式；数据取自 `/api/v1/list/...`        |
| `/{key}/{...path}`         | 缓存代理     | 真实文件；经 httpcached，支持 Range 与 304     |
| `/api/**`                  | JSON 接口    | 永远不会被前端或代理吃掉                       |

字段与状态枚举见 [`../docs/README.md`](../docs/README.md)。

## 命令行

```bash
go run .                                   # 启动服务
go run . -dump-cache-config proxy.yaml     # 由数据库生成缓存代理配置后退出
go run . -drop-caches                      # 清空 Redis 缓存（列表 + 状态）后退出
```

两个命令定义在 `internal/cli`，`main` 只按命令声明的依赖（`Needs`）初始化：
生成代理配置只读数据库、不需要 Redis，清缓存两者都要。因此 `main` 保持为一条直线的
初始化序列，新增命令也不用改 flag 解析代码。

`mirror_list` 修改后需要 `-drop-caches` 才会立刻生效：镜像列表缓存 24 小时，
状态快照由 cron 每小时重写。

## 开发与测试

```bash
./scripts/dev.sh up        # 起 PostgreSQL + Redis（Docker，带样例数据）
./scripts/dev.sh proxy     # 起缓存代理（路由由数据库生成）
go run .                   # 起后端

go build ./... && go vet ./... && go test ./...
gofmt -l .                 # 应无输出
```

scripts/dev.sh 的完整命令：`up | down | destroy | reset | status | logs | proxy |
probe | drop | audit`。数据库端口 55433、Redis 56380、缓存代理 8001。

前端构建产物放到 `web.dir`（见 [`../docs/README.md`](../docs/README.md)）。

## License

见 [LICENSE](./LICENSE)。
