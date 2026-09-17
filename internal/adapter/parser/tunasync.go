package parser

import (
	"strings"
	"sync"
	"time"

	"mirror/internal/infra/logger"
	"mirror/internal/meta"
	"mirror/internal/model/data"
	modelErr "mirror/internal/model/errors"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
)

// statusRequestTimeout bounds a single upstream status fetch so one slow mirror
// site cannot stall the whole refresh cycle.
const statusRequestTimeout = 15 * time.Second

// statusDocumentTTL is how long a fetched status document is reused.
//
// One document lists every mirror of a site (TUNA's is ~74 KB, NJU's ~152 KB),
// so a refresh cycle must fetch it once and answer all of that site's mirrors
// from memory rather than downloading it per mirror.
const statusDocumentTTL = 5 * time.Minute

// statusDocumentCache memoizes status documents by URL.
var (
	statusDocumentMutex sync.RWMutex
	statusDocumentStore = make(map[string]statusDocument)
)

type statusDocument struct {
	items     []data.TunaSync
	fetchedAt time.Time
}

// ResetStatusDocumentCache drops every cached document. Used by tests, and
// available to callers that want a guaranteed fresh read.
func ResetStatusDocumentCache() {
	statusDocumentMutex.Lock()
	defer statusDocumentMutex.Unlock()

	statusDocumentStore = make(map[string]statusDocument)
}

// TunaSyncParse reads one mirror's entry from an upstream status document.
//
// The document shape is declared per site (see Upstreams and statusDecoder), so
// this function only has to match a key and build the result.
//
// It returns modelErr.ErrStatusNotFound when the document does not list the
// requested mirror, and a transport/decoding error otherwise, so callers can
// tell "unknown status" apart from "upstream is broken".
func TunaSyncParse(upstream Upstream, key string) (data.MirrorListStatus, error) {
	items, err := statusDocumentItems(upstream)
	if err != nil {
		return data.MirrorListStatus{}, err
	}

	for _, item := range items {
		if !strings.EqualFold(item.Name, key) {
			continue
		}

		status := item.Status

		// Prefer an exact byte count when the document states one; fall back to
		// parsing the human-readable text.
		size := UnifySize(item.Size)
		if item.SizeBytes != nil {
			size = *item.SizeBytes
		}

		lastUpdate := item.LastUpdateTS

		return data.MirrorListStatus{
			Status:     &status,
			Size:       &size,
			LastUpdate: &lastUpdate,
		}, nil
	}

	// The upstream answered, but does not mirror this key (yet).
	return data.MirrorListStatus{}, modelErr.ErrStatusNotFound
}

// statusDocumentItems returns the parsed status document, from cache when it is
// still fresh.
func statusDocumentItems(upstream Upstream) ([]data.TunaSync, error) {
	metaUrl := upstream.StatusURL()

	statusDocumentMutex.RLock()
	cached, ok := statusDocumentStore[metaUrl]
	statusDocumentMutex.RUnlock()

	if ok && time.Since(cached.fetchedAt) < statusDocumentTTL {
		return cached.items, nil
	}

	items, err := fetchStatusDocument(upstream)
	if err != nil {
		// A failed refresh must not throw away a usable snapshot: keep serving
		// whatever we already have for this site.
		if ok {
			logger.L.Warn(
				"failed to refresh mirror status document, keeping the previous copy",
				zap.String("url", metaUrl),
				zap.Error(err),
			)
			return cached.items, nil
		}
		return nil, err
	}

	statusDocumentMutex.Lock()
	statusDocumentStore[metaUrl] = statusDocument{items: items, fetchedAt: time.Now()}
	statusDocumentMutex.Unlock()

	return items, nil
}

// fetchStatusDocument downloads and decodes one status document.
func fetchStatusDocument(upstream Upstream) ([]data.TunaSync, error) {
	metaUrl := upstream.StatusURL()
	client := resty.New().SetTimeout(statusRequestTimeout)

	// Read the body as bytes: the shape is decided by the decoder, not by the
	// JSON tags of one specific schema.
	resp, err := client.R().
		SetHeader("User-Agent", meta.UserAgent).
		Get(metaUrl)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != 200 {
		logger.L.Warn(
			"failed to fetch mirror status document",
			zap.String("url", metaUrl),
			zap.Int("status_code", resp.StatusCode()),
		)
		return nil, modelErr.ErrStatusNotFound
	}

	items, err := decoderFor(upstream.Format).Decode(resp.Body())
	if err != nil {
		logger.L.Warn(
			"failed to decode mirror status document",
			zap.String("url", metaUrl),
			zap.String("format", string(upstream.Format)),
			zap.Error(err),
		)
		return nil, err
	}

	return items, nil
}
