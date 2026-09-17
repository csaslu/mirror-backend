package response

// MirrorListResponse is one row of GET /api/v1/mirrors.json.
//
// Status, Size and LastUpdate are pointers on purpose: `null` means "not known
// yet" (cold cache, or an upstream we cannot read), which the frontend renders
// differently from a real zero.
type MirrorListResponse struct {
	ID         int     `json:"id"`
	Key        string  `json:"key"`     // directory name on this mirror, e.g. "ubuntu"
	Comment    string  `json:"comment"` // optional human description (Chinese)
	Type       string  `json:"type"`    // mirror_type enum: reverse_proxy | rsync | http | https | ftp | s3
	Source     string  `json:"source"`  // upstream base URL or rsync URL
	Status     *string `json:"status"`
	Size       *int64  `json:"size"`        // in bytes
	LastUpdate *int64  `json:"last_update"` // in Unix timestamp (seconds)
}
