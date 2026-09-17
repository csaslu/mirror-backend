package directory

// Parser turns an upstream listing document into the neutral Directory model.
//
// Implementations must be pure: no network access, no global state, and they
// must not modify the caller's slice.
type Parser interface {
	// Name identifies the parser in API responses and logs.
	Name() string

	// Detect reports whether this parser recognises the document. Detection
	// must be cheap and must not panic on malformed input.
	Detect(body []byte) bool

	// Parse extracts the listing, or returns ErrNotAListing when the document
	// is not a listing of requestPath.
	Parse(body []byte, requestPath string) (*Directory, error)
}

// Parsers is the registry, tried in order.
//
// Every entry is a different server module rather than a different site,
// because the same site can answer with different modules depending on the
// deployment. Adding support for another listing shape means adding one
// implementation here — nothing else in the codebase changes.
var Parsers = []Parser{
	NginxFancyIndexParser{},
	ApacheAutoIndexParser{},
}

// LooksLikeListing reports whether any parser recognises the document. It lets
// callers decide early (before parsing) whether to serve a page themselves or
// pass the upstream response through untouched.
func LooksLikeListing(body []byte) bool {
	for _, parser := range Parsers {
		if parser.Detect(body) {
			return true
		}
	}

	return false
}

// ParseDocument runs the first parser that both recognises the document and
// produces a usable listing.
//
// Returning ErrNotAListing means "not a directory listing we understand"; the
// caller must then fall back to proxying the upstream response rather than
// showing an empty page.
func ParseDocument(body []byte, requestPath string) (*Directory, error) {
	if len(body) == 0 {
		return nil, ErrNotAListing
	}

	for _, parser := range Parsers {
		if !parser.Detect(body) {
			continue
		}

		parsed, err := parser.Parse(body, requestPath)
		if err != nil {
			// A recognised but unparseable document is not a reason to try a
			// different parser: the shapes do not overlap.
			return nil, err
		}

		return parsed, nil
	}

	return nil, ErrNotAListing
}
