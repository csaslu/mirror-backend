package adapter

import (
	"mirror/internal/adapter/parser"
	"mirror/internal/model/data"
)

func ParseStatus(source string, key string) (data.MirrorListStatus, error) {
	switch {
	case parser.NJUCatch(source):
		return parser.NJUParse(key)
	case parser.TsinghuaCatch(source):
		return parser.TsinghuaParse(key)
	default:
		// Cannot parse, return empty status
		return data.MirrorListStatus{}, nil
	}
}
