package parser

import (
	"strconv"
	"strings"
)

// UnifySize converts a size string (e.g., "10M", "1.5G")
// to its equivalent size in bytes as an int64.
func UnifySize(size string) int64 {
	// Pre-process of letter capital
	size = strings.ToUpper(size)
	result := int64(0)

	switch {
	case strings.HasSuffix(size, "B"):
		sizeF64, err := strconv.ParseFloat(strings.TrimSuffix(size, "B"), 64)
		if err != nil {
			return 0
		}
		result = int64(sizeF64)
	case strings.HasSuffix(size, "K"):
		sizeF64, err := strconv.ParseFloat(strings.TrimSuffix(size, "K"), 64)
		if err != nil {
			return 0
		}
		result = int64(sizeF64 * 1024)
	case strings.HasSuffix(size, "M"):
		sizeF64, err := strconv.ParseFloat(strings.TrimSuffix(size, "M"), 64)
		if err != nil {
			return 0
		}
		result = int64(sizeF64 * 1024 * 1024)
	case strings.HasSuffix(size, "G"):
		sizeF64, err := strconv.ParseFloat(strings.TrimSuffix(size, "G"), 64)
		if err != nil {
			return 0
		}
		result = int64(sizeF64 * 1024 * 1024 * 1024)
	case strings.HasSuffix(size, "T"):
		sizeF64, err := strconv.ParseFloat(strings.TrimSuffix(size, "T"), 64)
		if err != nil {
			return 0
		}
		result = int64(sizeF64 * 1024 * 1024 * 1024 * 1024)
	default:
		return 0
	}
	return result
}
