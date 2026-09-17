package data

// TunaSync is one mirror entry of a status document.
//
// It is the neutral form every upstream decoder produces: the tunasync schema
// happens to match it field for field, and other formats (see the USTC decoder)
// are mapped onto it. Size carries both the human-readable original and, when
// the document provides it, an exact byte count.
type TunaSync struct {
	Name           string `json:"name"`
	IsMaster       bool   `json:"is_master"`
	Status         string `json:"status"`
	LastUpdate     string `json:"last_update"`
	LastUpdateTS   int64  `json:"last_update_ts"`
	LastStarted    string `json:"last_started"`
	LastStartedTS  int64  `json:"last_started_ts"`
	LastEnded      string `json:"last_ended"`
	LastEndedTS    int64  `json:"last_ended_ts"`
	NextSchedule   string `json:"next_schedule"`
	NextScheduleTS int64  `json:"next_schedule_ts"`
	Upstream       string `json:"upstream"`
	Size           string `json:"size"`

	// SizeBytes is set only by decoders whose document states an exact size.
	SizeBytes *int64 `json:"-"`
}
