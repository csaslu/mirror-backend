package v1

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"mirror/internal/cacheclient"
	"mirror/internal/infra/logger"
	"mirror/internal/mirror"
	dbModel "mirror/internal/model/database"
	"mirror/internal/model/v1/response"
	"mirror/internal/parser/directory"
	"mirror/internal/repository"

	"go.uber.org/zap"
)

const (
	// listingSizeLimit caps how much of an upstream listing we are willing to
	// read. Real listings are tens of kilobytes; the cap stops a hostile or
	// broken upstream from making us buffer a huge document.
	listingSizeLimit = 8 << 20 // 8 MiB
)

// ErrListingUnavailable means the upstream did not answer with a directory
// listing we can render: a file was requested, the server has listing disabled,
// or it uses a format we do not parse. The frontend turns this into a link to
// the original upstream page.
var ErrListingUnavailable = errors.New("upstream did not return a parseable directory listing")

// ErrMirrorNotFound means the requested key is not a mirror we serve.
var ErrMirrorNotFound = errors.New("mirror not found")

// ErrUnsupportedType means the mirror is not served over HTTP at all
// (an rsync-only mirror has no pages to browse).
var ErrUnsupportedType = errors.New("mirror type has no browsable content")

// DirectoryListing reads one directory of a mirror through the caching proxy
// and parses it into a neutral listing.
//
// Everything the browser sees goes through the cache: the listing document is
// fetched from httpcached (so its cache rules apply and the cache stays warm),
// not from the upstream directly.
func DirectoryListing(ctx context.Context, key string, subPath string, cache *cacheclient.Client) (*response.DirectoryListingResponse, error) {
	if cache == nil {
		return nil, cacheclient.ErrNotConfigured
	}

	item, err := findMirror(key)
	if err != nil {
		return nil, err
	}

	if !browsable(item.Type) {
		return nil, ErrUnsupportedType
	}

	upstream, err := mirror.ParseUpstream(item.Source, item.Key)
	if err != nil {
		return nil, err
	}

	// The path is untrusted: JoinPath rejects traversal and normalises the rest.
	directoryPath, ok := mirror.JoinPath("/", subPath)
	if !ok {
		return nil, ErrListingUnavailable
	}
	directoryPath = directory.NormalizePath(directoryPath)

	// Ask the cache for the directory itself, with a trailing slash so the
	// upstream serves the index rather than redirecting.
	cachePath := upstream.CachePath() + directoryPath
	if !strings.HasSuffix(cachePath, "/") {
		cachePath += "/"
	}

	body, upstreamResponse, err := cache.ReadBody(ctx, cachePath, "text/html,application/xhtml+xml", listingSizeLimit)
	if err != nil {
		return nil, err
	}

	contentType := ""
	if upstreamResponse != nil {
		contentType = upstreamResponse.Header.Get("Content-Type")
	}

	if upstreamResponse != nil && upstreamResponse.StatusCode != 200 {
		logger.L.Debug(
			"upstream returned a non-200 for a directory request",
			zap.String("mirror", item.Key),
			zap.String("path", directoryPath),
			zap.Int("status", upstreamResponse.StatusCode),
		)
		return nil, ErrListingUnavailable
	}

	// A directory must be HTML. This also keeps the parser away from package
	// files that happen to be huge or binary.
	if contentType != "" && !strings.Contains(strings.ToLower(contentType), "html") {
		return nil, ErrListingUnavailable
	}

	parsed, err := directory.ParseDocument(body, directoryPath)
	if err != nil {
		if errors.Is(err, directory.ErrNotAListing) {
			return nil, ErrListingUnavailable
		}
		return nil, err
	}

	return buildListingResponse(item, upstream, parsed), nil
}

// buildListingResponse converts a parsed listing into the API shape.
func buildListingResponse(
	item dbModel.MirrorList,
	upstream mirror.Upstream,
	parsed *directory.Directory,
) *response.DirectoryListingResponse {
	displayPath := "/" + item.Key
	if parsed.Path != "/" {
		displayPath += parsed.Path
	}

	result := &response.DirectoryListingResponse{
		Key:         item.Key,
		Path:        parsed.Path,
		DisplayPath: displayPath,
		// The upstream page for this directory, for the "view original" link.
		// built by trimming any trailing slash first so the root does not come
		// out as "//".
		Source:    strings.TrimSuffix(upstream.BaseURL+parsed.Path, "/") + "/",
		Parser:    parsed.Parser,
		FetchedAt: time.Now().Unix(),
		Entries:   make([]response.DirectoryEntryResponse, 0, len(parsed.Entries)),
	}

	// The parent link points at the directory one level up, in the browser's
	// canonical form: "/ubuntu/" for a mirror root, "/ubuntu/dists" below it.
	if parsed.Path != "/" {
		parentDir := path.Dir(parsed.Path)

		parent := "/" + item.Key
		if parentDir != "/" && parentDir != "." {
			parent += parentDir
		} else {
			parent += "/"
		}

		result.ParentPath = &parent
	}

	for _, entry := range parsed.Entries {
		url := "/" + item.Key + path.Join(parsed.Path, entry.Href)
		if entry.Kind == directory.KindDirectory && !strings.HasSuffix(url, "/") {
			url += "/"
		}

		converted := response.DirectoryEntryResponse{
			Name:     entry.Name,
			URL:      url,
			Kind:     string(entry.Kind),
			Size:     entry.Size,
			SizeText: entry.SizeText,
			ModTime:  nil,
			ModText:  entry.ModText,
			Title:    entry.Title,
		}

		if entry.ModTime != nil {
			stamp := entry.ModTime.Unix()
			converted.ModTime = &stamp
		}

		result.Entries = append(result.Entries, converted)
	}

	// A local listing may not be sorted the way readers expect: directories
	// first, then names in natural order. Sorting here keeps the frontend dumb
	// and the API order stable.
	sortEntries(result.Entries)

	return result
}

// sortEntries orders directories before files, using a natural name order so
// "python3.9" sorts before "python3.10".
func sortEntries(entries []response.DirectoryEntryResponse) {
	// Insertion sort: listings are small, and this keeps the comparison inline.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entryLess(entries[j], entries[j-1]); j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

func entryLess(a, b response.DirectoryEntryResponse) bool {
	aDir := a.Kind == string(directory.KindDirectory)
	bDir := b.Kind == string(directory.KindDirectory)

	if aDir != bDir {
		return aDir
	}

	return naturalLess(a.Name, b.Name)
}

// naturalLess compares two names so embedded numbers order numerically.
func naturalLess(a, b string) bool {
	aLower, bLower := strings.ToLower(a), strings.ToLower(b)

	i, j := 0, 0
	for i < len(aLower) && j < len(bLower) {
		aDigit := aLower[i] >= '0' && aLower[i] <= '9'
		bDigit := bLower[j] >= '0' && bLower[j] <= '9'

		if aDigit && bDigit {
			aStart, bStart := i, j
			for i < len(aLower) && aLower[i] >= '0' && aLower[i] <= '9' {
				i++
			}
			for j < len(bLower) && bLower[j] >= '0' && bLower[j] <= '9' {
				j++
			}

			aNum := strings.TrimLeft(aLower[aStart:i], "0")
			bNum := strings.TrimLeft(bLower[bStart:j], "0")

			if len(aNum) != len(bNum) {
				return len(aNum) < len(bNum)
			}
			if aNum != bNum {
				return aNum < bNum
			}

			continue
		}

		if aLower[i] != bLower[j] {
			return aLower[i] < bLower[j]
		}

		i++
		j++
	}

	if len(aLower)-i != len(bLower)-j {
		return len(aLower)-i < len(bLower)-j
	}

	return a < b
}

// findMirror looks a mirror up by key.
func findMirror(key string) (dbModel.MirrorList, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return dbModel.MirrorList{}, ErrMirrorNotFound
	}

	repo := repository.NewMirrorListRepository()
	mirrors, err := repo.ReadAllMirrors()
	if err != nil {
		return dbModel.MirrorList{}, err
	}

	for _, item := range mirrors {
		if strings.EqualFold(item.Key, key) {
			return item, nil
		}
	}

	return dbModel.MirrorList{}, ErrMirrorNotFound
}

// browsable reports whether a mirror type can be browsed over HTTP.
func browsable(mirrorType dbModel.MirrorType) bool {
	switch mirrorType {
	case dbModel.MirrorTypeReverseProxy, dbModel.MirrorTypeHttp, dbModel.MirrorTypeHttps:
		return true
	default:
		return false
	}
}
