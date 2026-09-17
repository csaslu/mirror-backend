package directory

import (
	"strconv"
	"strings"
	"time"
)

// sizeUnits maps a listing's unit suffix to a multiplier. Servers write both
// decimal (kB) and binary (KiB) forms, and nginx fancyindex emits the
// IEC-looking "MiB"/"GiB" while dividing by 1024.
var sizeUnits = map[string]float64{
	"":  1,
	"B": 1,

	"K": 1024, "KB": 1024, "KIB": 1024,
	"M": 1 << 20, "MB": 1 << 20, "MIB": 1 << 20,
	"G": 1 << 30, "GB": 1 << 30, "GIB": 1 << 30,
	"T": 1 << 40, "TB": 1 << 40, "TIB": 1 << 40,
	"P": 1 << 50, "PB": 1 << 50, "PIB": 1 << 50,
}

// ParseSize converts a listing size cell into bytes.
//
// Returns nil for the many shapes that mean "no size": "-", "", "&mdash;", a
// directory marker ("[DIR]"), or anything unrecognised. Callers keep the raw
// text so the UI can still show what the server said.
func ParseSize(text string) *int64 {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return nil
	}

	// Numeric part first, so thousands separators can be dropped safely.
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")

	if cleaned == "-" || cleaned == "—" || cleaned == "–" {
		return nil
	}

	index := 0
	for index < len(cleaned) {
		c := cleaned[index]
		if (c >= '0' && c <= '9') || c == '.' {
			index++
			continue
		}
		break
	}
	if index == 0 {
		return nil
	}

	value, err := strconv.ParseFloat(cleaned[:index], 64)
	if err != nil || value < 0 {
		return nil
	}

	unit := strings.ToUpper(cleaned[index:])
	multiplier, ok := sizeUnits[unit]
	if !ok {
		// An unknown unit means we cannot claim to know the size.
		return nil
	}

	bytes := int64(value * multiplier)
	return &bytes
}

// dateLayouts are the formats mirror sites actually emit, most common first.
var dateLayouts = []string{
	// nginx fancyindex, with the offset the server prints.
	"02 Jan 2006 15:04:05 -0700",
	"02 Jan 2006 15:04:05 MST",
	// Apache mod_autoindex.
	"02-Jan-2006 15:04",
	"2006-01-02 15:04",
	// ISO variants.
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
	// A few servers print a single-digit day or a full month name.
	"2 Jan 2006 15:04:05 -0700",
	"Jan 2 15:04",
	"Jan 2 2006",
}

// ParseDate converts a listing date cell into a time.
//
// The offset matters: listings usually carry the upstream's own zone, and
// rendering a relative age from a wrong zone is off by hours.
func ParseDate(text string) *time.Time {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" || cleaned == "-" || cleaned == "—" {
		return nil
	}

	// Collapse the runs of whitespace some servers emit between columns.
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	for _, layout := range dateLayouts {
		if parsed, err := time.Parse(layout, cleaned); err == nil {
			return &parsed
		}
	}

	return nil
}
