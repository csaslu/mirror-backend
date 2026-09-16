package parser

import (
	"mirror/internal/model/data"
	"strings"
)

func TsinghuaCatch(source string) bool {
	return strings.Contains(source, "mirrors.tuna.tsinghua.edu.cn")
}

func TsinghuaParse(key string) (data.MirrorListStatus, error) {
	return TunaSyncParse("https://mirrors.tuna.tsinghua.edu.cn/static/tunasync.json", key)
}
