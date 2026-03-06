package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestServiceOpenInitializesSchema(t *testing.T) {
	repoRoot := newTestRepoRoot(t)

	service := New(Dependencies{})
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	if err := service.Open(context.Background(), OpenOptions{
		ConfigPath:   filepath.Join(repoRoot, "config", "config.yaml"),
		DatabasePath: ".toolhub/toolhub.db",
	}); err != nil {
		t.Fatalf("open store: %v", err)
	}

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	var count int
	if err := db.QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		"sync_state",
	).Scan(&count); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected sync_state table to exist, got %d matches", count)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, ".toolhub", "toolhub.db")); err != nil {
		t.Fatalf("stat database file: %v", err)
	}
}

func TestServiceOpenEnablesForeignKeysOnEachNewConnection(t *testing.T) {
	repoRoot := newTestRepoRoot(t)

	service := New(Dependencies{})
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	if err := service.Open(context.Background(), OpenOptions{
		ConfigPath:   filepath.Join(repoRoot, "config", "config.yaml"),
		DatabasePath: ".toolhub/toolhub.db",
	}); err != nil {
		t.Fatalf("open store: %v", err)
	}

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(0)

	for i := 0; i < 2; i++ {
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatalf("open sql conn %d: %v", i, err)
		}

		var enabled int
		if err := conn.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&enabled); err != nil {
			_ = conn.Close()
			t.Fatalf("query foreign_keys on conn %d: %v", i, err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("close sql conn %d: %v", i, err)
		}
		if enabled != 1 {
			t.Fatalf("expected foreign_keys=1 on conn %d, got %d", i, enabled)
		}
	}
}

func TestBaseStoreRunInTxRollsBackAndCommits(t *testing.T) {
	repoRoot := newTestRepoRoot(t)

	service := New(Dependencies{})
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	if err := service.Open(context.Background(), OpenOptions{
		ConfigPath:   filepath.Join(repoRoot, "config", "config.yaml"),
		DatabasePath: ".toolhub/toolhub.db",
	}); err != nil {
		t.Fatalf("open store: %v", err)
	}

	base, err := service.BaseStore()
	if err != nil {
		t.Fatalf("base store: %v", err)
	}

	rollbackErr := errors.New("rollback")
	err = base.RunInTx(context.Background(), func(ctx context.Context, tx BaseStore) error {
		if _, err := tx.DB().ExecContext(
			ctx,
			"INSERT INTO sync_state (name, value_json, updated_at) VALUES (?, ?, ?)",
			"rolled_back",
			`{"status":"pending"}`,
			"2026-03-07T00:00:00Z",
		); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected rollback error, got %v", err)
	}

	if got := countSyncStateRows(t, service, "rolled_back"); got != 0 {
		t.Fatalf("expected rollback to discard row, got %d rows", got)
	}

	if err := service.RunInTx(context.Background(), func(ctx context.Context, tx BaseStore) error {
		_, err := tx.DB().ExecContext(
			ctx,
			"INSERT INTO sync_state (name, value_json, updated_at) VALUES (?, ?, ?)",
			"committed",
			`{"status":"ready"}`,
			"2026-03-07T00:00:00Z",
		)
		return err
	}); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	if got := countSyncStateRows(t, service, "committed"); got != 1 {
		t.Fatalf("expected committed row to persist, got %d rows", got)
	}
}

func countSyncStateRows(t *testing.T, service *Service, name string) int {
	t.Helper()

	db, err := service.DB()
	if err != nil {
		t.Fatalf("store db: %v", err)
	}

	var count int
	if err := db.QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM sync_state WHERE name = ?",
		name,
	).Scan(&count); err != nil {
		t.Fatalf("count sync_state rows: %v", err)
	}
	return count
}

func newTestRepoRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeTestFile(t, root, filepath.Join("config", "config.yaml"), "store: test\n")
	writeTestFile(t, root, filepath.Join("sql", "schema.sql"), loadRepoSchema(t))
	return root
}

func loadRepoSchema(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}

	schemaPath := filepath.Join(filepath.Dir(file), "..", "..", "sql", "schema.sql")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read repo schema: %v", err)
	}
	return string(data)
}

func writeTestFile(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
