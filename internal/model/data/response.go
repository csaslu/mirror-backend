package data

type ResponseContent struct {
	Code int
	Msg  *string
}

type MirrorListStatus struct {
	ID         int     `json:"id"`
	Status     *string `json:"status"`
	Size       *int64  `json:"size"`
	LastUpdate *int64  `json:"last_update"`
}
