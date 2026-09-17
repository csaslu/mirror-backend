package parser

import (
	"encoding/json"
	"strconv"
	"strings"

	"mirror/internal/model/data"
)

// statusDecoder converts one upstream's status document into the neutral
// TunaSync form, so everything downstream (matching, caching, the refresh
// cycle) is independent of which site published it.
//
// This is the extension point for "our upstream keeps its status somewhere
// else, in a different shape": implement this interface, register the shape on
// the Upstream entry, and nothing else changes.
type statusDecoder interface {
	// Decode parses the document body.
	Decode(body []byte) ([]data.TunaSync, error)
}

// decoderFor returns the decoder for a document shape.
func decoderFor(format StatusFormat) statusDecoder {
	switch format {
	case FormatUSTC:
		return ustcDecoder{}
	default:
		return tunaSyncDecoder{}
	}
}

// tunaSyncDecoder reads the tunasync schema, which TUNA and NJU both publish.
type tunaSyncDecoder struct{}

func (tunaSyncDecoder) Decode(body []byte) ([]data.TunaSync, error) {
	items := make([]data.TunaSync, 0)
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}

	return items, nil
}

// ustcDocument is the shape published at https://mirrors.ustc.edu.cn/status/json.
//
// It states state as flags plus an exit code rather than as a word, and gives
// the size as exact bytes:
//
//	{
//	  "name": "adoptium.apt",
//	  "disable": false,
//	  "syncing": false,
//	  "exitCode": 0,
//	  "lastSuccess": 1789511201,
//	  "size": 236419689472,
//	  "upstream": "https://packages.adoptium.net/artifactory/"
//	}
type ustcDocument struct {
	Name        string `json:"name"`
	Disable     bool   `json:"disable"`
	Syncing     bool   `json:"syncing"`
	ExitCode    int    `json:"exitCode"`
	LastSuccess int64  `json:"lastSuccess"`
	PrevRun     int64  `json:"prevRun"`
	Size        int64  `json:"size"`
	Upstream    string `json:"upstream"`
}

// ustcDecoder maps USTC's document onto the neutral form.
type ustcDecoder struct{}

func (ustcDecoder) Decode(body []byte) ([]data.TunaSync, error) {
	var document []ustcDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, err
	}

	items := make([]data.TunaSync, 0, len(document))

	for _, entry := range document {
		size := entry.Size

		items = append(items, data.TunaSync{
			Name:         entry.Name,
			Status:       ustcStatus(entry),
			LastUpdateTS: entry.LastSuccess,
			LastEndedTS:  entry.PrevRun,
			Upstream:     entry.Upstream,
			// Some sites write a human-readable size in the same field; keep it
			// consistent with the tunasync behaviour for any consumer that only
			// looks at the string.
			Size:      formatBytes(entry.Size),
			SizeBytes: &size,
		})
	}

	return items, nil
}

// ustcStatus turns USTC's flags and exit code into the status vocabulary the
// rest of the system uses.
//
// The mapping follows what the site itself shows: a mirror that is disabled or
// currently syncing is neither a success nor a failure, and a non-zero exit
// code means the last run failed.
func ustcStatus(entry ustcDocument) string {
	switch {
	case entry.Disable:
		return "disabled"
	case entry.Syncing:
		return "syncing"
	case entry.ExitCode == 0:
		return "success"
	default:
		return "failed"
	}
}

// formatBytes renders a byte count the way tunasync writes sizes ("1.5G"),
// so both document shapes produce comparable text for consumers that only read
// the string form.
func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0"
	}

	const unit = 1024

	units := []string{"", "K", "M", "G", "T", "P"}
	value := float64(bytes)
	index := 0

	for value >= unit && index < len(units)-1 {
		value /= unit
		index++
	}

	if index == 0 {
		return strconv.FormatInt(bytes, 10)
	}

	// Two decimals at most, without trailing zeros: 1.5G, not 1.50G.
	return trimDecimal(value) + units[index]
}

// trimDecimal renders a value with at most two decimals and no trailing zeros,
// which is how tunasync writes sizes ("1.5G", "2G").
func trimDecimal(value float64) string {
	text := strconv.FormatFloat(value, 'f', 2, 64)
	text = strings.TrimRight(text, "0")
	text = strings.TrimSuffix(text, ".")

	return text
}
