package database

import (
	"mirror/internal/infra/logger"

	"go.uber.org/zap"
	"xorm.io/xorm"
)

func NewSession() *xorm.Session {
	return E.NewSession()
}

// Close is a public method to close a database session
func Close(databaseSession *xorm.Session) {
	_ = databaseSession.Close()
}

// Rollback is a public method to roll back a transaction
func Rollback(session *xorm.Session) {
	if err := session.Rollback(); err != nil {
		logger.L.Error("failed to rollback transaction", zap.Error(err))
	}
}

// RollbackError is a public method to roll back a transaction with errors returned
func RollbackError(session *xorm.Session, err error) error {
	Rollback(session)
	return err
}

// Commit a transaction by condition
func Commit(disableCommit bool, session *xorm.Session) error {
	if !disableCommit {
		// Commit transaction
		return session.Commit()
	}
	return nil
}
