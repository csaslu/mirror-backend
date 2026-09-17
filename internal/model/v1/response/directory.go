package response

// DirectoryListingResponse is the body of
// GET /api/v1/list/{key}/{path...}/mirrors.json
type DirectoryListingResponse struct {
	// Key is the mirror this listing belongs to ("ubuntu").
	Key string `json:"key"`

	// Path is the directory path relative to the mirror root, canonical form:
	// "/" for the mirror root, otherwise no trailing slash.
	Path string `json:"path"`

	// DisplayPath is the full path shown to the user ("/ubuntu/dists/noble").
	DisplayPath string `json:"display_path"`

	// Source is the upstream URL this listing was read from, so a user can
	// compare with the original page.
	Source string `json:"source"`

	// Parser names the listing implementation that produced this result.
	Parser string `json:"parser"`

	// FetchedAt is when the listing was retrieved (Unix seconds). Note that the
	// caching proxy may have answered from its cache.
	FetchedAt int64 `json:"fetched_at"`

	// ParentPath is the parent directory, or null at the mirror root.
	ParentPath *string `json:"parent_path"`

	// Entries excludes the parent row, which is exposed as ParentPath.
	Entries []DirectoryEntryResponse `json:"entries"`
}

// DirectoryEntryResponse is one row of a listing.
type DirectoryEntryResponse struct {
	Name string `json:"name"`

	// URL is an absolute path on this site ("/ubuntu/dists/"), ready to use as
	// a link or to copy.
	URL string `json:"url"`

	// Kind is "directory" or "file".
	Kind string `json:"kind"`

	// Size is bytes, null for directories and for servers that omit sizes.
	Size *int64 `json:"size"`

	// SizeText is the upstream's own text, shown when Size cannot be parsed.
	SizeText string `json:"size_text,omitempty"`

	// ModTime is the last modification time (Unix seconds), null when unknown.
	ModTime *int64 `json:"mod_time"`

	// ModText is the upstream's own date text.
	ModText string `json:"mod_text,omitempty"`

	// EntryCount is the number of items a subdirectory holds, when the server
	// told us; null otherwise. Never guessed.
	EntryCount *int `json:"entry_count"`

	// Title is the upstream link title, when provided.
	Title string `json:"title,omitempty"`
}
