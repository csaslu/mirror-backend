package database

import (
	"mirror/internal/infra/config"
	"mirror/internal/infra/logger"

	_ "github.com/lib/pq"
	"go.gh.ink/json"
	"go.gh.ink/toolbox/xfmt"
	"go.gh.ink/xormzap"
	"go.uber.org/zap"
	"xorm.io/xorm"
)

var E *xorm.Engine

// Init inits global database connection
func Init() {
	xorm.SetDefaultJSONHandler(JSON{})

	dsn := xfmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		config.Get().Database.User,
		config.Get().Database.Pass,
		config.Get().Database.Host,
		config.Get().Database.Port,
		config.Get().Database.Name,
	)

	var err error
	E, err = xorm.NewEngine("postgres", dsn)
	if err != nil {
		logger.L.Fatal("failed to init database", zap.Error(err))
	}

	E.SetLogger(xormzap.Logger(logger.L))

	if config.Debug {
		E.ShowSQL(true)
	}

	if err = E.Ping(); err != nil {
		logger.L.Fatal("failed to ping database", zap.Error(err))
	}

	logger.L.Debug("postgresSQL initialized")
}

func Cleanup() {
	_ = E.Close()
}

type JSON struct{}

func (JSON) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (JSON) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
