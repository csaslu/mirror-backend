package parser

import (
	"mirror/internal/model/data"
	"strings"
)

func NJUCatch(source string) bool {
	return strings.Contains(source, "mirror.nju.edu.cn")
}

func NJUParse(key string) (data.MirrorListStatus, error) {
	return TunaSyncParse("https://mirror.nju.edu.cn/static/tunasync.json", key)
}
