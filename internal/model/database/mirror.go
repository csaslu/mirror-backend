package database

// MirrorType is the `mirror_type` PostgreSQL enum: how this mirror is served
// to clients.
type MirrorType = string

const (
	// MirrorTypeReverseProxy caches upstream on demand through httpcached.
	MirrorTypeReverseProxy MirrorType = "reverse_proxy"
	MirrorTypeRsync                   = "rsync"
	MirrorTypeHttp                    = "http"
	MirrorTypeHttps                   = "https"
	MirrorTypeFtp                     = "ftp"
	MirrorTypeS3                      = "s3"
)

// MirrorList table 'mirror_list'.
//
// The json tags matter: this struct is marshalled into the Redis cache, and
// without tags the cached document would carry Go field names.
type MirrorList struct {
	ID      int        `xorm:"pk autoincr 'id'" json:"id"`
	Key     string     `xorm:"text notnull 'key'" json:"key"`
	Comment string     `xorm:"text 'comment'" json:"comment"`
	Type    MirrorType `xorm:"enum notnull 'type'" json:"type"`
	Source  string     `xorm:"text notnull 'source'" json:"source"`
}

func (MirrorList) TableName() string {
	return "mirror_list"
}
