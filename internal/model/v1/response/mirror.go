package response

type MirrorListResponse struct {
	Key        string  `json:"key"`
	Comment    string  `json:"comment"`
	Type       string  `json:"type"`
	Source     string  `json:"source"`
	Status     *string `json:"status"`
	Size       *int64  `json:"size"`        // in bytes
	LastUpdate *int64  `json:"last_update"` // in Unix timestamp
}
