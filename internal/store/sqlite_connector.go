package store

import (
	"context"
	"database/sql/driver"
	"fmt"
)

type foreignKeysConnector struct {
	driver driver.Driver
	dsn    string
}

func newForeignKeysConnector(drv driver.Driver, dsn string) driver.Connector {
	return &foreignKeysConnector{
		driver: drv,
		dsn:    dsn,
	}
}

func (c *foreignKeysConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if opener, ok := c.driver.(driver.DriverContext); ok {
		connector, err := opener.OpenConnector(c.dsn)
		if err != nil {
			return nil, err
		}
		conn, err := connector.Connect(ctx)
		if err != nil {
			return nil, err
		}
		if err := enableForeignKeys(ctx, conn); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return conn, nil
	}

	conn, err := c.driver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	if err := enableForeignKeys(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (c *foreignKeysConnector) Driver() driver.Driver {
	return c.driver
}

func enableForeignKeys(ctx context.Context, conn driver.Conn) error {
	const pragma = "PRAGMA foreign_keys = ON"

	if execer, ok := conn.(driver.ExecerContext); ok {
		if _, err := execer.ExecContext(ctx, pragma, nil); err != nil {
			return fmt.Errorf("enable foreign keys: %w", err)
		}
		return nil
	}

	stmt, err := prepareForeignKeysStmt(ctx, conn, pragma)
	if err != nil {
		return err
	}
	defer func() {
		_ = stmt.Close()
	}()

	execer, ok := stmt.(driver.StmtExecContext)
	if !ok {
		return fmt.Errorf("enable foreign keys: prepared statement does not support ExecContext")
	}
	if _, err := execer.ExecContext(ctx, nil); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	return nil
}

func prepareForeignKeysStmt(ctx context.Context, conn driver.Conn, pragma string) (driver.Stmt, error) {
	if preparer, ok := conn.(driver.ConnPrepareContext); ok {
		stmt, err := preparer.PrepareContext(ctx, pragma)
		if err != nil {
			return nil, fmt.Errorf("prepare foreign key pragma: %w", err)
		}
		return stmt, nil
	}

	stmt, err := conn.Prepare(pragma)
	if err != nil {
		return nil, fmt.Errorf("prepare foreign key pragma: %w", err)
	}
	return stmt, nil
}
