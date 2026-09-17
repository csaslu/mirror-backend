// Package directory parses upstream directory listings into a neutral model.
//
// Mirror sites answer a directory request with HTML produced by whatever
// listing module their web server runs: nginx fancyindex, Apache mod_autoindex,
// or a custom page. This package turns that HTML into the same structure for
// every source, so the API and the frontend never need to know which server
// they are talking to.
//
// Parsing is deliberately conservative. A page that does not look like a
// listing of the requested directory yields ErrNotAListing, and the caller
// falls back to proxying the upstream response untouched; a parse failure must
// never take the site down or invent entries.
package directory

import (
	"errors"
	"net/url"
	"path"
	"strings"
	"time"
)

// EntryKind distinguishes the three things a listing row can be.
type EntryKind string

const (
	// KindDirectory is a subdirectory of the requested path.
	KindDirectory EntryKind = "directory"

	// KindFile is a regular file.
	KindFile EntryKind = "file"

	// KindParent is the ".." row, kept so the frontend can render a parent
	// link without inventing one.
	KindParent EntryKind = "parent"
)

// Entry is one row of a directory listing.
type Entry struct {
	// Name is the display name, without a trailing slash.
	Name string

	// Href is the URL-encoded path relative to the requested directory
	// ("dists/", "ls-lR.gz"). It never contains a scheme or host.
	Href string

	Kind EntryKind

	// Size is the object size in bytes, nil when the listing does not say
	// (directories, or a server that omits sizes).
	Size *int64

	// SizeText is the raw size cell, kept for display when it cannot be parsed.
	SizeText string

	// ModTime is the last modification time, nil when absent or unparseable.
	ModTime *time.Time

	// ModText is the raw date cell.
	ModText string

	// Title is the link's title attribute, when the server provides one.
	Title string
}

// Directory is a parsed listing.
type Directory struct {
	// Path is the path of the directory that was requested, relative to the
	// mirror root ("/"), always starting with "/" and never ending with one
	// unless it is the root itself.
	Path string

	// Entries excludes the parent row, which is exposed as Parent instead.
	Entries []Entry

	// Parent is the ".." entry when the listing had one and the directory is
	// not the mirror root.
	Parent *Entry

	// Parser names the implementation that produced this result, for logs and
	// for the API response.
	Parser string
}

// ErrNotAListing means the response is not a directory listing of the requested
// path. Callers treat it as "proxy the upstream response as-is".
var ErrNotAListing = errors.New("response is not a directory listing")

// MinEntries is the number of usable rows a parser must find before its result
// is trusted. A page with one link is far more likely to be an error page than
// a directory.
const MinEntries = 1

// baseName resolves the directory name a listing claims to be showing.
func baseName(rawURL string) string {
	trimmed := strings.TrimSuffix(rawURL, "/")
	if trimmed == "" {
		return "/"
	}

	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 {
		return "/"
	}

	return trimmed[idx:]
}

// cleanHref normalises a listing href and rejects anything that is not a plain
// relative path inside the directory.
//
// This is the security boundary of the whole feature: the href is attacker
// controlled (it comes from upstream HTML) and ends up in URLs we hand to the
// browser, so absolute URLs, other schemes and parent traversal are all
// refused here rather than sanitised later.
func cleanHref(href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" {
		return "", false
	}

	// Strip a query or fragment, but remember whether the link was a sort link
	// (Apache/nginx add ?C=N&O=A to their column headers).
	if idx := strings.IndexAny(href, "?#"); idx >= 0 {
		if idx == 0 {
			return "", false // pure query/fragment: a sort link or anchor
		}
		href = href[:idx]
	}

	decoded, err := url.PathUnescape(href)
	if err != nil {
		// Fall back to the raw value; a malformed escape is not a reason to
		// drop a visible entry.
		decoded = href
	}

	if strings.Contains(decoded, "://") || strings.HasPrefix(decoded, "//") {
		return "", false
	}
	if strings.Contains(decoded, ":") && !strings.HasPrefix(decoded, "./") {
		// "javascript:", "data:", "mailto:" and friends.
		if scheme := decoded[:strings.Index(decoded, ":")]; !strings.Contains(scheme, "/") {
			return "", false
		}
	}
	if strings.HasPrefix(decoded, "/") {
		// Absolute paths are produced by some servers for the parent link and
		// for icon links; they are not usable as entries.
		return "", false
	}

	// Reject traversal in any form.
	segments := strings.SplitSeq(strings.TrimSuffix(decoded, "/"), "/")
	for segment := range segments {
		if segment == ".." {
			return "", false
		}
	}

	return href, true
}

// entryFromHref fills in the fields that only depend on the href itself.
func entryFromHref(href string, text string, title string) (Entry, bool) {
	cleaned, ok := cleanHref(href)
	if !ok {
		return Entry{}, false
	}

	isDir := strings.HasSuffix(cleaned, "/")
	name := strings.TrimSuffix(cleaned, "/")

	// Prefer the link text, which is what the user sees, but fall back to the
	// href: some servers render icons instead of text.
	display := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "/"))
	if display == "" || display == name {
		if decoded, err := url.PathUnescape(name); err == nil {
			display = path.Base(decoded)
		} else {
			display = path.Base(name)
		}
	} else if decoded, err := url.PathUnescape(display); err == nil {
		display = decoded
	}

	display = strings.TrimSpace(display)
	if display == "" || display == ".." {
		return Entry{}, false
	}

	if title == "" {
		title = display
	}

	kind := KindFile
	if isDir {
		kind = KindDirectory
	}

	return Entry{
		Name:  display,
		Href:  cleaned,
		Kind:  kind,
		Title: strings.TrimSpace(title),
	}, true
}
