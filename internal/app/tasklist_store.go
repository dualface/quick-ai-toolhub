package app

import (
	"github.com/uptrace/bun"

	"quick-ai-toolhub/internal/store"
)

type taskListStoreAdapter struct {
	service *store.Service
}

func (a taskListStoreAdapter) DB() bun.IDB {
	if a.service == nil {
		return nil
	}

	db, err := a.service.DB()
	if err != nil {
		return nil
	}
	return db
}
