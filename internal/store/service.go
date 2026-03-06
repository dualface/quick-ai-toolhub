package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
)

const defaultSchemaPath = "sql/schema.sql"

var ErrNotOpen = errors.New("store is not open")

type Service struct {
	logger *slog.Logger

	mu     sync.RWMutex
	db     *bun.DB
	dbPath string
}

type Dependencies struct {
	Logger *slog.Logger
}

type OpenOptions struct {
	ConfigPath   string
	DatabasePath string
	SchemaPath   string
}

func New(deps Dependencies) *Service {
	return &Service{
		logger: componentLogger(deps.Logger),
	}
}

func (s *Service) Name() string {
	return "store"
}

func (s *Service) Open(ctx context.Context, opts OpenOptions) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	databasePath := strings.TrimSpace(opts.DatabasePath)
	if databasePath == "" {
		return errors.New("database path is required")
	}

	baseDir, err := resolveBaseDir(opts.ConfigPath)
	if err != nil {
		return err
	}
	repoRoot, err := findRepoRoot(baseDir)
	if err != nil {
		return err
	}

	resolvedSchemaPath, err := resolveSchemaPath(repoRoot, opts.SchemaPath)
	if err != nil {
		return err
	}
	resolvedDatabasePath, err := resolveDatabasePath(repoRoot, databasePath)
	if err != nil {
		return err
	}

	if err := ensureDatabaseDir(resolvedDatabasePath); err != nil {
		return err
	}

	sqlDB := sql.OpenDB(newForeignKeysConnector(sqliteshim.Driver(), resolvedDatabasePath))
	bunDB := bun.NewDB(sqlDB, sqlitedialect.New())

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = bunDB.Close()
		return fmt.Errorf("open sqlite database %s: %w", resolvedDatabasePath, err)
	}

	if err := applySchema(ctx, sqlDB, resolvedSchemaPath); err != nil {
		_ = bunDB.Close()
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		_ = bunDB.Close()
		return errors.New("store is already open")
	}

	s.db = bunDB
	s.dbPath = resolvedDatabasePath
	s.logger.Info(
		"sqlite store ready",
		slog.String("database_path", resolvedDatabasePath),
		slog.String("schema_path", resolvedSchemaPath),
	)
	return nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil
	}

	db := s.db
	s.db = nil
	s.dbPath = ""
	return db.Close()
}

func (s *Service) DB() (*bun.DB, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.db == nil {
		return nil, ErrNotOpen
	}
	return s.db, nil
}

func (s *Service) BaseStore() (BaseStore, error) {
	db, err := s.DB()
	if err != nil {
		return BaseStore{}, err
	}
	return NewBaseStore(db), nil
}

func (s *Service) RunInTx(ctx context.Context, fn func(context.Context, BaseStore) error) error {
	if fn == nil {
		return errors.New("nil transaction function")
	}

	base, err := s.BaseStore()
	if err != nil {
		return err
	}
	return base.RunInTx(ctx, fn)
}

func componentLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default().With("component", "store")
	}
	return logger.With("component", "store")
}

func resolveBaseDir(configPath string) (string, error) {
	if strings.TrimSpace(configPath) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
		return filepath.Abs(cwd)
	}

	return filepath.Abs(filepath.Dir(configPath))
}

func findRepoRoot(startDir string) (string, error) {
	current := startDir
	for {
		schemaPath := filepath.Join(current, defaultSchemaPath)
		info, err := os.Stat(schemaPath)
		if err == nil {
			if info.IsDir() {
				return "", fmt.Errorf("schema path %s is a directory", schemaPath)
			}
			return current, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat schema %s: %w", schemaPath, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("locate repository root from %s: %s not found", startDir, defaultSchemaPath)
		}
		current = parent
	}
}

func resolveSchemaPath(repoRoot, schemaPath string) (string, error) {
	if strings.TrimSpace(schemaPath) == "" {
		schemaPath = defaultSchemaPath
	}
	if filepath.IsAbs(schemaPath) {
		return schemaPath, nil
	}
	return filepath.Abs(filepath.Join(repoRoot, schemaPath))
}

func resolveDatabasePath(repoRoot, databasePath string) (string, error) {
	if isSQLiteMemoryDSN(databasePath) || strings.HasPrefix(strings.TrimSpace(databasePath), "file:") {
		return databasePath, nil
	}
	if filepath.IsAbs(databasePath) {
		return databasePath, nil
	}
	return filepath.Abs(filepath.Join(repoRoot, databasePath))
}

func ensureDatabaseDir(databasePath string) error {
	if isSQLiteMemoryDSN(databasePath) || strings.HasPrefix(databasePath, "file:") {
		return nil
	}

	dir := filepath.Dir(databasePath)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create database directory %s: %w", dir, err)
	}
	return nil
}

func applySchema(ctx context.Context, db *sql.DB, schemaPath string) error {
	schemaSQL, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("read schema %s: %w", schemaPath, err)
	}

	if _, err := db.ExecContext(ctx, string(schemaSQL)); err != nil {
		return fmt.Errorf("apply schema %s: %w", schemaPath, err)
	}
	return nil
}

func isSQLiteMemoryDSN(databasePath string) bool {
	switch strings.TrimSpace(databasePath) {
	case ":memory:", "file::memory:":
		return true
	default:
		return strings.HasPrefix(strings.TrimSpace(databasePath), "file::memory:")
	}
}
