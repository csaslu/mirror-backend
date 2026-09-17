package parser

import (
	"strconv"
	"strings"
)

// sizeUnits maps a normalised unit suffix to its multiplier in bytes.
// TunaSync reports sizes such as "69.98G", "549G" or "1.39T".
var sizeUnits = map[string]float64{
	"":  1,
	"B": 1,

	"K":   1 << 10,
	"KB":  1 << 10,
	"KIB": 1 << 10,

	"M":   1 << 20,
	"MB":  1 << 20,
	"MIB": 1 << 20,

	"G":   1 << 30,
	"GB":  1 << 30,
	"GIB": 1 << 30,

	"T":   1 << 40,
	"TB":  1 << 40,
	"TIB": 1 << 40,

	"P":   1 << 50,
	"PB":  1 << 50,
	"PIB": 1 << 50,
}

// UnifySize converts a human readable size (e.g. "10M", "1.5 GB", "549G",
// "1024") into bytes. Unknown or malformed input yields 0, which callers
// render as "unknown size".
func UnifySize(size string) int64 {
	// Normalise: uppercase, drop inner spaces and a trailing quality suffix.
	size = strings.ToUpper(strings.TrimSpace(size))
	size = strings.ReplaceAll(size, " ", "")

	// Split the numeric prefix from the unit suffix.
	index := 0
	for index < len(size) {
		c := size[index]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' {
			index++
			continue
		}
		break
	}

	value := size[:index]
	unit := size[index:]

	// A bare number is bytes, and an unknown unit means we cannot tell.
	multiplier, ok := sizeUnits[unit]
	if !ok {
		return 0
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return 0
	}

	return int64(parsed * multiplier)
}
