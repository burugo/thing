package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3" // SQLite driver

	// Import the core package (now at module root)
	thing "thing"
)

// SQLiteAdapter implements the thing.DBAdapter interface for SQLite.
type SQLiteAdapter struct {
	db      *sqlx.DB
	dsn     string
	closeMx sync.Mutex
	closed  bool
}

// Ensure SQLiteAdapter implements thing.DBAdapter.
var _ thing.DBAdapter = (*SQLiteAdapter)(nil)

// NewSQLiteAdapter creates a new SQLite database adapter.
// It opens a connection pool and pings the database.
// Returns thing.DBAdapter interface type for broader compatibility.
func NewSQLiteAdapter(dsn string) (thing.DBAdapter, error) { // Return interface type
	log.Printf("Initializing SQLite adapter with DSN: %s", dsn)
	db, err := sqlx.Connect("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite: %w", err)
	}

	// Configure connection pool settings (optional but recommended)
	db.SetMaxOpenConns(25)                 // Example value
	db.SetMaxIdleConns(5)                  // Example value
	db.SetConnMaxLifetime(5 * time.Minute) // Example value

	// Ping the database to verify connection
	if err := db.Ping(); err != nil {
		db.Close() // Close the pool if ping fails
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	log.Println("SQLite adapter initialized successfully.")
	return &SQLiteAdapter{db: db, dsn: dsn}, nil
}

// Get retrieves a single row and scans it into the destination struct (which must be a pointer).
// It uses sqlx.GetContext for convenience.
func (a *SQLiteAdapter) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if a.isClosed() {
		return fmt.Errorf("adapter is closed")
	}
	// TODO: Query logging?
	start := time.Now()
	err := a.db.GetContext(ctx, dest, query, args...)
	duration := time.Since(start)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("DB Get (No Rows): %s [%v] (%s)", query, args, duration)
			return thing.ErrNotFound // Use error from parent package
		}
		log.Printf("DB Get Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite GetContext error: %w", err)
	}
	log.Printf("DB Get: %s [%v] (%s)", query, args, duration)
	return nil
}

// Select retrieves multiple rows and scans them into the destination slice.
// It uses sqlx.SelectContext.
func (a *SQLiteAdapter) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if a.isClosed() {
		return fmt.Errorf("adapter is closed")
	}
	// TODO: Query logging?
	start := time.Now()
	err := a.db.SelectContext(ctx, dest, query, args...)
	duration := time.Since(start)
	if err != nil {
		// sqlx doesn't return ErrNoRows for Select, it returns an empty slice.
		log.Printf("DB Select Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite SelectContext error: %w", err)
	}
	log.Printf("DB Select: %s [%v] (%s)", query, args, duration)
	return nil
}

// Exec executes a query that doesn't return rows (INSERT, UPDATE, DELETE).
func (a *SQLiteAdapter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if a.isClosed() {
		return nil, fmt.Errorf("adapter is closed")
	}
	// TODO: Query logging?
	start := time.Now()
	result, err := a.db.ExecContext(ctx, query, args...)
	duration := time.Since(start)
	if err != nil {
		log.Printf("DB Exec Error: %s [%v] (%s) - %v", query, args, duration, err)
		return nil, fmt.Errorf("sqlite ExecContext error: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()
	log.Printf("DB Exec: %s [%v] (Affected: %d, LastInsertID: %d) (%s)", query, args, rowsAffected, lastInsertID, duration)
	return result, nil
}

// GetCount executes a SELECT COUNT(*) query based on the provided parameters.
func (a *SQLiteAdapter) GetCount(ctx context.Context, info *thing.ModelInfo, params thing.QueryParams) (int64, error) {
	if a.isClosed() {
		return 0, fmt.Errorf("adapter is closed")
	}
	if info == nil || info.TableName == "" {
		return 0, errors.New("GetCount: model info or table name is missing")
	}

	// Build query: SELECT COUNT(*) FROM table WHERE ...
	var queryBuilder strings.Builder
	queryBuilder.WriteString("SELECT COUNT(*) FROM ")
	queryBuilder.WriteString(info.TableName)

	args := params.Args // Keep a copy
	if params.Where != "" {
		queryBuilder.WriteString(" WHERE ")
		queryBuilder.WriteString(params.Where)
	}

	query := queryBuilder.String()
	var count int64

	log.Printf("DB GetCount: %s [%v]", query, args)
	start := time.Now()
	err := a.db.GetContext(ctx, &count, query, args...)
	duration := time.Since(start)

	if err != nil {
		// GetContext might return ErrNoRows if the WHERE clause matches nothing, but COUNT should always return 0 in that case.
		// However, handle potential driver differences or actual errors.
		log.Printf("DB GetCount Error: %s [%v] (%s) - %v", query, args, duration, err)
		return 0, fmt.Errorf("sqlite GetCount error: %w", err)
	}

	log.Printf("DB GetCount Result: %d (%s)", count, duration)
	return count, nil
}

// SelectPaginated executes a query including WHERE, ORDER BY, LIMIT, and OFFSET clauses.
func (a *SQLiteAdapter) SelectPaginated(ctx context.Context, dest interface{}, info *thing.ModelInfo, params thing.QueryParams, offset int, limit int) error {
	if a.isClosed() {
		return fmt.Errorf("adapter is closed")
	}
	if info == nil || info.TableName == "" || len(info.Columns) == 0 {
		return errors.New("SelectPaginated: model info, table name, or columns list is missing")
	}

	// Build base SELECT part (duplicate logic from thing.buildSelectSQL)
	quotedColumns := make([]string, len(info.Columns))
	for i, col := range info.Columns {
		quotedColumns[i] = fmt.Sprintf("\"%s\"", col)
	}
	baseQuery := fmt.Sprintf("SELECT %s FROM %s", strings.Join(quotedColumns, ", "), info.TableName)

	// Build clauses
	var queryBuilder strings.Builder
	queryBuilder.WriteString(baseQuery)

	args := make([]interface{}, len(params.Args))
	copy(args, params.Args)

	if params.Where != "" {
		queryBuilder.WriteString(" WHERE ")
		queryBuilder.WriteString(params.Where)
	}

	if params.Order != "" {
		queryBuilder.WriteString(" ORDER BY ")
		queryBuilder.WriteString(params.Order)
	}

	// Add LIMIT and OFFSET
	if limit > 0 {
		queryBuilder.WriteString(" LIMIT ?")
		args = append(args, limit)
		if offset > 0 {
			queryBuilder.WriteString(" OFFSET ?")
			args = append(args, offset)
		}
	} else if offset > 0 {
		// OFFSET without LIMIT might not be standard or efficient, but handle if needed.
		// SQLite requires LIMIT for OFFSET, use -1 for effectively no limit.
		queryBuilder.WriteString(" LIMIT -1 OFFSET ?")
		args = append(args, offset)
	}

	query := queryBuilder.String()

	log.Printf("DB SelectPaginated: %s [%v]", query, args)
	start := time.Now()
	err := a.db.SelectContext(ctx, dest, query, args...)
	duration := time.Since(start)

	if err != nil {
		log.Printf("DB SelectPaginated Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite SelectContext (paginated) error: %w", err)
	}

	log.Printf("DB SelectPaginated Success (%s)", duration)
	return nil
}

// BeginTx starts a new transaction.
func (a *SQLiteAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (thing.Tx, error) { // Return interface type
	if a.isClosed() {
		return nil, fmt.Errorf("adapter is closed")
	}
	// Pass opts through to sqlx
	tx, err := a.db.BeginTxx(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to begin sqlite transaction: %w", err)
	}
	log.Println("DB Transaction Started")
	// Return our wrapper which implements thing.Tx
	return &SQLiteTx{tx: tx}, nil
}

// Close closes the database connection pool.
func (a *SQLiteAdapter) Close() error {
	a.closeMx.Lock()
	defer a.closeMx.Unlock()
	if a.closed {
		return nil // Already closed
	}
	err := a.db.Close()
	if err == nil {
		a.closed = true
		log.Println("SQLite adapter closed.")
	}
	return err
}

// isClosed checks if the adapter has been closed.
func (a *SQLiteAdapter) isClosed() bool {
	a.closeMx.Lock()
	defer a.closeMx.Unlock()
	return a.closed
}

// --- Transaction Implementation ---

// SQLiteTx wraps sqlx.Tx to implement the thing.Tx interface.
type SQLiteTx struct {
	tx *sqlx.Tx
}

// Ensure SQLiteTx implements thing.Tx.
var _ thing.Tx = (*SQLiteTx)(nil)

// Get executes a query within the transaction, scanning into dest (which must be a pointer).
func (t *SQLiteTx) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// TODO: Query logging?
	start := time.Now()
	err := t.tx.GetContext(ctx, dest, query, args...)
	duration := time.Since(start)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("DB Tx Get (No Rows): %s [%v] (%s)", query, args, duration)
			return thing.ErrNotFound // Use error from parent package
		}
		log.Printf("DB Tx Get Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Tx GetContext error: %w", err)
	}
	log.Printf("DB Tx Get: %s [%v] (%s)", query, args, duration)
	return nil
}

// Select executes a query within the transaction.
func (t *SQLiteTx) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// TODO: Query logging?
	start := time.Now()
	err := t.tx.SelectContext(ctx, dest, query, args...)
	duration := time.Since(start)
	if err != nil {
		log.Printf("DB Tx Select Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Tx SelectContext error: %w", err)
	}
	log.Printf("DB Tx Select: %s [%v] (%s)", query, args, duration)
	return nil
}

// Exec executes a statement within the transaction.
func (t *SQLiteTx) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	// TODO: Query logging?
	start := time.Now()
	result, err := t.tx.ExecContext(ctx, query, args...)
	duration := time.Since(start)
	if err != nil {
		log.Printf("DB Tx Exec Error: %s [%v] (%s) - %v", query, args, duration, err)
		return nil, fmt.Errorf("sqlite Tx ExecContext error: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()
	log.Printf("DB Tx Exec: %s [%v] (Affected: %d, LastInsertID: %d) (%s)", query, args, rowsAffected, lastInsertID, duration)
	return result, nil
}

// Commit commits the transaction.
func (t *SQLiteTx) Commit() error {
	err := t.tx.Commit()
	if err != nil {
		log.Printf("DB Transaction Commit Error: %v", err)
		return fmt.Errorf("sqlite Tx Commit error: %w", err)
	}
	log.Println("DB Transaction Committed")
	return nil
}

// Rollback rolls back the transaction.
func (t *SQLiteTx) Rollback() error {
	err := t.tx.Rollback()
	if err != nil {
		if err == sql.ErrTxDone {
			log.Println("DB Transaction Rollback Warning: Transaction already committed or rolled back")
			return nil // Not considered a fatal error for rollback purpose
		}
		log.Printf("DB Transaction Rollback Error: %v", err)
		return fmt.Errorf("sqlite Tx Rollback error: %w", err)
	}
	log.Println("DB Transaction Rolled Back")
	return nil
}
