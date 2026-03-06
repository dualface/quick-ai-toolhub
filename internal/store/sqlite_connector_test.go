package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

func TestEnableForeignKeysUsesExecerContext(t *testing.T) {
	conn := &stubForeignKeysConn{
		execContextFn: func(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
			if query != "PRAGMA foreign_keys = ON" {
				t.Fatalf("unexpected query: %s", query)
			}
			if args != nil {
				t.Fatalf("expected nil args, got %#v", args)
			}
			return driver.RowsAffected(0), nil
		},
	}

	if err := enableForeignKeys(context.Background(), conn); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
}

func TestEnableForeignKeysFallsBackToStmtExecContext(t *testing.T) {
	stmt := &stubForeignKeysStmt{
		execContextFn: func(_ context.Context, args []driver.NamedValue) (driver.Result, error) {
			if args != nil {
				t.Fatalf("expected nil args, got %#v", args)
			}
			return driver.RowsAffected(0), nil
		},
	}
	conn := &stubPrepareOnlyConn{
		prepareContextFn: func(_ context.Context, query string) (driver.Stmt, error) {
			if query != "PRAGMA foreign_keys = ON" {
				t.Fatalf("unexpected query: %s", query)
			}
			return stmt, nil
		},
	}

	if err := enableForeignKeys(context.Background(), conn); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if !conn.prepareCalled {
		t.Fatal("expected PrepareContext fallback to be used")
	}
	if !stmt.closeCalled {
		t.Fatal("expected prepared statement to be closed")
	}
}

func TestEnableForeignKeysRejectsStmtWithoutExecContext(t *testing.T) {
	conn := &stubPrepareOnlyConn{
		prepareContextFn: func(_ context.Context, _ string) (driver.Stmt, error) {
			return &stubLegacyStmt{}, nil
		},
	}

	err := enableForeignKeys(context.Background(), conn)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "enable foreign keys: prepared statement does not support ExecContext" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnableForeignKeysWrapsPrepareErrors(t *testing.T) {
	wantErr := errors.New("prepare failed")
	conn := &stubPrepareOnlyConn{
		prepareContextFn: func(_ context.Context, _ string) (driver.Stmt, error) {
			return nil, wantErr
		},
	}

	err := enableForeignKeys(context.Background(), conn)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped prepare error, got %v", err)
	}
}

type stubForeignKeysConn struct {
	execContextFn func(context.Context, string, []driver.NamedValue) (driver.Result, error)
}

func (c *stubForeignKeysConn) Prepare(query string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare call")
}

func (c *stubForeignKeysConn) Close() error {
	return nil
}

func (c *stubForeignKeysConn) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented")
}

func (c *stubForeignKeysConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.execContextFn == nil {
		return nil, errors.New("unexpected ExecContext call")
	}
	return c.execContextFn(ctx, query, args)
}

type stubPrepareOnlyConn struct {
	prepareContextFn func(context.Context, string) (driver.Stmt, error)
	prepareCalled    bool
}

func (c *stubPrepareOnlyConn) Prepare(query string) (driver.Stmt, error) {
	c.prepareCalled = true
	if c.prepareContextFn == nil {
		return nil, errors.New("unexpected Prepare call")
	}
	return c.prepareContextFn(context.Background(), query)
}

func (c *stubPrepareOnlyConn) Close() error {
	return nil
}

func (c *stubPrepareOnlyConn) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented")
}

func (c *stubPrepareOnlyConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.prepareCalled = true
	if c.prepareContextFn == nil {
		return nil, errors.New("unexpected PrepareContext call")
	}
	return c.prepareContextFn(ctx, query)
}

type stubForeignKeysStmt struct {
	execContextFn func(context.Context, []driver.NamedValue) (driver.Result, error)
	closeCalled   bool
}

func (s *stubForeignKeysStmt) Close() error {
	s.closeCalled = true
	return nil
}

func (s *stubForeignKeysStmt) NumInput() int {
	return 0
}

func (s *stubForeignKeysStmt) Exec(_ []driver.Value) (driver.Result, error) {
	return nil, errors.New("unexpected Exec call")
}

func (s *stubForeignKeysStmt) Query(_ []driver.Value) (driver.Rows, error) {
	return nil, errors.New("not implemented")
}

func (s *stubForeignKeysStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if s.execContextFn == nil {
		return nil, errors.New("unexpected ExecContext call")
	}
	return s.execContextFn(ctx, args)
}

func (s *stubForeignKeysStmt) QueryContext(context.Context, []driver.NamedValue) (driver.Rows, error) {
	return nil, errors.New("not implemented")
}

type stubLegacyStmt struct{}

func (s *stubLegacyStmt) Close() error {
	return nil
}

func (s *stubLegacyStmt) NumInput() int {
	return 0
}

func (s *stubLegacyStmt) Exec([]driver.Value) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}

func (s *stubLegacyStmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, errors.New("not implemented")
}
