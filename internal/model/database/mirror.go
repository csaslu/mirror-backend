package database

// MirrorType enum 'mirror_type'
type MirrorType = string

const (
	MirrorTypeReverseProxy MirrorType = "reverse_proxy"
	MirrorTypeRsync                   = "rsync"
	MirrorTypeHttp                    = "http"
	MirrorTypeHttps                   = "https"
	MirrorTypeFtp                     = "ftp"
	MirrorTypeS3                      = "s3"
)

// MirrorList table 'mirror_list'
type MirrorList struct {
	ID      int        `xorm:"pk autoincr 'id'"`
	Key     string     `xorm:"text notnull 'key'"`
	Comment string     `xorm:"text 'comment'"`
	Type    MirrorType `xorm:"text notnull 'type'"`
	Source  string     `xorm:"text notnull 'source'"`
}

func (MirrorList) TableName() string {
	return "mirror_list"
}
