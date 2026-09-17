package data

// MirrorListStatus is the cached sync status of one mirror.
//
// Status and Size are pointers on purpose: a missing field means "unknown",
// which the frontend renders differently from a real zero. LastUpdate is always
// present because a mirror with no known sync time is not cached at all.
type MirrorListStatus struct {
	Status     *string `json:"status"`
	Size       *int64  `json:"size"`
	LastUpdate *int64  `json:"last_update"`
}
