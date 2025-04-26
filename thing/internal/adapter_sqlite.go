package internal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"thing" // Import the main package - module path is 'thing'

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// Compile-time check to ensure SQLiteAdapter implements the thing.DBAdapter interface.
var _ thing.DBAdapter = (*SQLiteAdapter)(nil)

// Compile-time check to ensure SQLiteTx implements the thing.Tx interface.
var _ thing.Tx = (*SQLiteTx)(nil)

// SQLiteAdapter implements the thing.DBAdapter interface for SQLite.
type SQLiteAdapter struct {
	db *sqlx.DB
}

// NewSQLiteAdapter creates a new SQLite adapter instance.
// It establishes a connection pool to the SQLite database specified by the DSN.
func NewSQLiteAdapter(dsn string) (*SQLiteAdapter, error) {
	// Use sqlx.Connect for convenience (combines sql.Open and Ping)
	db, err := sqlx.Connect("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite database (%s): %w", dsn, err)
	}

	// Configure connection pool settings (optional, defaults are often okay)
	// db.SetMaxOpenConns(10)
	// db.SetMaxIdleConns(5)
	// db.SetConnMaxLifetime(time.Hour)

	fmt.Printf("Successfully connected to SQLite: %s\n", dsn) // TODO: Replace with proper logging

	return &SQLiteAdapter{db: db}, nil
}

// Close closes the underlying database connection pool.
func (a *SQLiteAdapter) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// --- Implement other DBAdapter methods ---

// Get executes a query expected to return a single row and scans it into dest.
// It maps sql.ErrNoRows to thing.ErrNotFound.
func (a *SQLiteAdapter) Get(ctx context.Context, dest thing.Model, query string, args ...interface{}) error {
	if a.db == nil {
		return errors.New("db adapter not initialized")
	}
	err := a.db.GetContext(ctx, dest, query, args...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return thing.ErrNotFound // Use the package-defined error
		}
		// TODO: Add logging for the error?
		return fmt.Errorf("sqlite Get failed: %w", err)
	}
	return nil
}

// Select executes a query expected to return multiple rows and scans them into dest.
// dest must be a pointer to a slice of models (e.g., *[]UserModel).
func (a *SQLiteAdapter) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if a.db == nil {
		return errors.New("db adapter not initialized")
	}
	err := a.db.SelectContext(ctx, dest, query, args...)
	if err != nil {
		// SelectContext returns nil, nil if no rows are found, so no need to map ErrNoRows.
		// TODO: Add logging for the error?
		return fmt.Errorf("sqlite Select failed: %w", err)
	}
	return nil
}

// Exec executes a query that doesn't return rows (INSERT, UPDATE, DELETE).
func (a *SQLiteAdapter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if a.db == nil {
		return nil, errors.New("db adapter not initialized")
	}
	result, err := a.db.ExecContext(ctx, query, args...)
	if err != nil {
		// TODO: Add logging for the error?
		return nil, fmt.Errorf("sqlite Exec failed: %w", err)
	}
	return result, nil
}

// BeginTx starts a transaction.
func (a *SQLiteAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (thing.Tx, error) {
	if a.db == nil {
		return nil, errors.New("db adapter not initialized")
	}
	// Use sqlx's BeginTxx which returns a *sqlx.Tx
	txx, err := a.db.BeginTxx(ctx, opts)
	if err != nil {
		// TODO: Add logging?
		return nil, fmt.Errorf("sqlite BeginTx failed: %w", err)
	}
	// Wrap the *sqlx.Tx in our SQLiteTx struct
	return &SQLiteTx{tx: txx}, nil
}

// --- Transaction Implementation ---

// SQLiteTx implements the thing.Tx interface for SQLite transactions.
type SQLiteTx struct {
	tx *sqlx.Tx // Embed sqlx.Tx to delegate calls
}

// Get executes a query expected to return a single row within the transaction.
func (t *SQLiteTx) Get(ctx context.Context, dest thing.Model, query string, args ...interface{}) error {
	if t.tx == nil {
		return errors.New("transaction is nil")
	}
	err := t.tx.GetContext(ctx, dest, query, args...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return thing.ErrNotFound
		}
		return fmt.Errorf("sqlite Tx Get failed: %w", err)
	}
	return nil
}

// Select executes a query expected to return multiple rows within the transaction.
func (t *SQLiteTx) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if t.tx == nil {
		return errors.New("transaction is nil")
	}
	err := t.tx.SelectContext(ctx, dest, query, args...)
	if err != nil {
		return fmt.Errorf("sqlite Tx Select failed: %w", err)
	}
	return nil
}

// Exec executes a query that doesn't return rows within the transaction.
func (t *SQLiteTx) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if t.tx == nil {
		return nil, errors.New("transaction is nil")
	}
	result, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite Tx Exec failed: %w", err)
	}
	return result, nil
}

// Commit commits the transaction.
func (t *SQLiteTx) Commit() error {
	if t.tx == nil {
		return errors.New("transaction is nil")
	}
	err := t.tx.Commit()
	if err != nil {
		return fmt.Errorf("sqlite Tx Commit failed: %w", err)
	}
	return nil
}

// Rollback rolls back the transaction.
func (t *SQLiteTx) Rollback() error {
	if t.tx == nil {
		return errors.New("transaction is nil")
	}
	err := t.tx.Rollback()
	if err != nil {
		// It's common practice to check if the error is sql.ErrTxDone
		if errors.Is(err, sql.ErrTxDone) {
			// If already committed or rolled back, it might not be an application error
			log.Printf("Warning: Rollback called on already completed transaction: %v", err)
			return nil // Or return the error depending on desired strictness
		}
		return fmt.Errorf("sqlite Tx Rollback failed: %w", err)
	}
	return nil
}
