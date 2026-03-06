package store

import (
	"context"
	"errors"

	"github.com/uptrace/bun"
)

type BaseStore struct {
	db bun.IDB
}

func NewBaseStore(db bun.IDB) BaseStore {
	return BaseStore{db: db}
}

func (s BaseStore) DB() bun.IDB {
	return s.db
}

func (s BaseStore) RunInTx(ctx context.Context, fn func(context.Context, BaseStore) error) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if s.db == nil {
		return ErrNotOpen
	}
	if fn == nil {
		return errors.New("nil transaction function")
	}

	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(ctx, NewBaseStore(tx))
	})
}
