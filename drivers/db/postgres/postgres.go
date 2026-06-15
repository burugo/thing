// Package postgres provides a Thing DBAdapter implementation backed by
// PostgreSQL via the lib/pq driver.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	log "github.com/burugo/thing/internal/logging"

	"github.com/burugo/thing"
	"github.com/burugo/thing/common"
	driversSchema "github.com/burugo/thing/drivers/schema"
	"github.com/burugo/thing/internal/dbscan"
	"github.com/burugo/thing/internal/sqlbuilder"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// PostgresDialector implements the sqlbuilder.Dialector interface for PostgreSQL.
type Dialector struct{}

func (d Dialector) Quote(identifier string) string {
	return `"` + identifier + `"`
}

func (d Dialector) Placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

// PostgreSQLAdapter implements the DBAdapter interface for PostgreSQL.
type Adapter struct {
	db      *sql.DB
	builder thing.SQLBuilder
}

// PostgreSQLTx implements the Tx interface for PostgreSQL.
type Tx struct {
	tx      *sql.Tx
	builder thing.SQLBuilder
}

// Compile-time checks to ensure interfaces are implemented.
var (
	_ thing.DBAdapter = (*Adapter)(nil)
	_ thing.Tx        = (*Tx)(nil)
)

// --- Constructor ---

// NewPostgreSQLAdapter creates a new PostgreSQL adapter instance.
func NewPostgreSQLAdapter(dsn string) (thing.DBAdapter, error) {
	// return nil, fmt.Errorf("NewPostgreSQLAdapter not yet implemented") // Remove placeholder

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Verify the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping postgres database: %w", err)
	}

	// Set reasonable default connection pool settings
	const (
		defaultMaxOpenConns = 25
		defaultMaxIdleConns = 10
	)
	db.SetMaxOpenConns(defaultMaxOpenConns)
	db.SetMaxIdleConns(defaultMaxIdleConns)
	db.SetConnMaxLifetime(time.Hour)

	// Create a SQLBuilder with PostgreSQL dialect
	builder := thing.NewSQLBuilder(Dialector{})

	log.Println("PostgreSQL adapter initialized successfully.")
	return &Adapter{
		db:      db,
		builder: builder,
	}, nil
}

// --- DBAdapter Methods ---

func (a *Adapter) Close() error {
	log.Println("PostgreSQL adapter: Closing connection")
	// return fmt.Errorf("PostgreSQLAdapter.Close not implemented") // Remove placeholder
	if a.db != nil {
		err := a.db.Close()
		if err != nil {
			log.Printf("Error closing PostgreSQL adapter: %v", err)
			return fmt.Errorf("error closing postgres connection: %w", err)
		}
		log.Println("PostgreSQL adapter closed.")
		return nil
	}
	return errors.New("postgres adapter is nil or already closed")
}

// Get retrieves a single row and scans it into the destination struct.
// Uses QueryContext and prepares scan destinations based on returned columns.
// PostgreSQL uses '$N' placeholders.
func (a *Adapter) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// TODO: Handle sql.ErrNoRows
	// return fmt.Errorf("PostgreSQLAdapter.Get not implemented") // Remove placeholder

	reboundQuery := a.builder.Rebind(query)
	log.Printf("DB Get (PostgreSQL): %s [%v] (Original: %s)", reboundQuery, args, query)
	start := time.Now()

	rows, err := a.db.QueryContext(ctx, reboundQuery, args...)
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Get Error (Query - PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Get query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Get Error (Columns - PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Get failed fetching columns: %w", err)
	}

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("get: destination must be a non-nil pointer to a struct, got %T", dest)
	}
	structVal := destVal.Elem()
	scanDest, err := dbscan.PrepareScanDest(structVal, cols) // Reusable helper
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Get Error (Prepare Scan - PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Get setup error: %w", err)
	}

	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > 1 {
			duration := time.Since(start)
			log.Errorf("DB Get Error (Multiple Rows - PostgreSQL) (%s)", duration)
			return fmt.Errorf("postgres Get error: expected 1 row, got multiple")
		}
		err = rows.Scan(scanDest...)
		if err != nil {
			duration := time.Since(start)
			log.Errorf("DB Get Error (Scan - PostgreSQL) (%s): %v", duration, err)
			return fmt.Errorf("postgres Get scan error: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		duration := time.Since(start)
		log.Errorf("DB Get Error (Rows Iteration - PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Get rows error: %w", err)
	}

	if rowCount == 0 {
		duration := time.Since(start)
		log.Printf("DB Get (No Rows - PostgreSQL): %s [%v] (%s)", reboundQuery, args, duration)
		return common.ErrNotFound
	}

	duration := time.Since(start)
	log.Printf("DB Get (Success - PostgreSQL): %s [%v] (%s)", reboundQuery, args, duration)
	return nil
}

// Select executes a query and scans the results into a slice.
// PostgreSQL uses '$N' placeholders.
func (a *Adapter) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// return fmt.Errorf("PostgreSQLAdapter.Select not implemented") // Remove placeholder

	reboundQuery := a.builder.Rebind(query)
	log.Printf("DB Select (PostgreSQL): %s [%v] (Original: %s)", reboundQuery, args, query)
	start := time.Now()

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("select: destination must be a pointer to a slice, got %T", dest)
	}
	sliceVal := destVal.Elem()
	elemType := sliceVal.Type().Elem()

	rows, err := a.db.QueryContext(ctx, reboundQuery, args...)
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Select Query Error (PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Select query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Errorf("DB Select Error (Fetching Columns - PostgreSQL): %v", err)
		return fmt.Errorf("postgres Select failed fetching columns: %w", err)
	}

	isBasicTypeSlice := dbscan.IsBasicType(elemType)
	isPtrElem := elemType.Kind() == reflect.Ptr
	baseElemType := elemType
	if isPtrElem {
		baseElemType = elemType.Elem()
	}

	if !isBasicTypeSlice && baseElemType.Kind() != reflect.Struct {
		return fmt.Errorf("select: destination slice element type must be struct, pointer to struct, or basic type, got %s", elemType.String())
	}

	rowCount := 0
	for rows.Next() {
		var elemToScan reflect.Value
		var scanDest []interface{}
		var setupErr error

		if isBasicTypeSlice {
			elemPtr := reflect.New(elemType)
			elemToScan = elemPtr
			scanDest = []interface{}{elemToScan.Interface()}
		} else {
			newElemPtrVal := reflect.New(baseElemType)
			elemToScan = newElemPtrVal
			scanDest, setupErr = dbscan.PrepareScanDest(newElemPtrVal.Elem(), cols)
			if setupErr != nil {
				duration := time.Since(start)
				log.Errorf("DB Select Error (Prepare Scan - PostgreSQL) (%s): %v", duration, setupErr)
				return fmt.Errorf("postgres Select row setup error: %w", setupErr)
			}
		}

		if err := rows.Scan(scanDest...); err != nil {
			duration := time.Since(start)
			log.Errorf("DB Select Scan Error (PostgreSQL) (%s): %v", duration, err)
			return fmt.Errorf("postgres Select scan error: %w", err)
		}

		var valToAppend reflect.Value
		if isBasicTypeSlice {
			valToAppend = elemToScan.Elem()
		} else {
			if isPtrElem {
				valToAppend = elemToScan
			} else {
				valToAppend = elemToScan.Elem()
			}
		}
		sliceVal.Set(reflect.Append(sliceVal, valToAppend))
		rowCount++
	}

	if err = rows.Err(); err != nil {
		duration := time.Since(start)
		log.Errorf("DB Select Rows Error (PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Select rows error: %w", err)
	}

	duration := time.Since(start)
	log.Printf("DB Select OK (PostgreSQL): %s [%v] (%d rows, %s)", reboundQuery, args, rowCount, duration)
	return nil
}

// Exec executes a query that doesn't return rows.
// PostgreSQL uses '$N' placeholders.
func (a *Adapter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	reboundQuery := a.builder.Rebind(query)
	start := time.Now()

	// Detect INSERT and add RETURNING id if not present
	isInsert := strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "INSERT")
	if isInsert && !strings.Contains(strings.ToUpper(query), "RETURNING") {
		reboundQuery += " RETURNING id"
		var lastInsertID int64
		row := a.db.QueryRowContext(ctx, reboundQuery, args...)
		err := row.Scan(&lastInsertID)
		duration := time.Since(start)
		if err != nil {
			log.Errorf("DB Exec Error (PostgreSQL INSERT RETURNING) (%s): %v", duration, err)
			return nil, fmt.Errorf("postgres ExecContext error (insert returning): %w", err)
		}
		log.Printf("DB Exec (PostgreSQL INSERT RETURNING): %s [%v] (LastInsertID: %d, %s)", reboundQuery, args, lastInsertID, duration)
		return &pgResult{lastInsertID: lastInsertID, rowsAffected: 1}, nil
	}

	result, err := a.db.ExecContext(ctx, reboundQuery, args...)
	duration := time.Since(start)
	if err != nil {
		log.Errorf("DB Exec Error (PostgreSQL) (%s): %v", duration, err)
		return nil, fmt.Errorf("postgres ExecContext error: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	log.Printf("DB Exec (PostgreSQL): %s [%v] (Affected: %d) (%s)", reboundQuery, args, rowsAffected, duration)
	return result, nil
}

// GetCount executes a SELECT COUNT(*) query.
// PostgreSQL uses '$N' placeholders.
func (a *Adapter) GetCount(ctx context.Context, tableName string, where string, args []interface{}) (int64, error) {
	if tableName == "" {
		return 0, errors.New("getCount: table name is missing")
	}
	if where != "" {
		where, args = sqlbuilder.ExpandInClauses(Dialector{}, where, args)
	}
	query := a.builder.BuildCountSQL(tableName, where)
	reboundQuery := a.builder.Rebind(query)
	log.Printf("DB GetCount (Postgres): %s [%v]", reboundQuery, args)
	row := a.db.QueryRowContext(ctx, reboundQuery, args...)
	var count int64
	err := row.Scan(&count)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("postgres GetCount scan error: %w", err)
	}
	return count, nil
}

// BeginTx starts a transaction.
func (a *Adapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (thing.Tx, error) {
	// return nil, fmt.Errorf("PostgreSQLAdapter.BeginTx not implemented") // Remove placeholder

	log.Println("DB Transaction Started (PostgreSQL)")
	tx, err := a.db.BeginTx(ctx, opts)
	if err != nil {
		log.Errorf("DB BeginTx Error (PostgreSQL): %v", err)
		return nil, fmt.Errorf("postgres BeginTx error: %w", err)
	}
	return &Tx{tx: tx, builder: a.builder}, nil
}

// DB returns the underlying *sql.DB for advanced use cases.
func (a *Adapter) DB() *sql.DB {
	return a.db
}

// Builder returns the SQLBuilder associated with the PostgreSQLAdapter.
func (a *Adapter) Builder() thing.SQLBuilder {
	return a.builder
}

// DialectName returns the name of the database dialect.
func (a *Adapter) DialectName() string {
	return "postgres"
}

// --- Tx Methods ---

// Commit commits the transaction.
func (tx *Tx) Commit() error {
	// return fmt.Errorf("PostgreSQLTx.Commit not implemented") // Remove placeholder

	log.Println("DB Transaction Committing (PostgreSQL)")
	err := tx.tx.Commit()
	if err != nil {
		log.Errorf("DB Tx Commit Error (PostgreSQL): %v", err)
		return fmt.Errorf("postgres Tx Commit error: %w", err)
	}
	log.Println("DB Transaction Committed (PostgreSQL)")
	return nil
}

// Rollback rolls back the transaction.
func (tx *Tx) Rollback() error {
	// return fmt.Errorf("PostgreSQLTx.Rollback not implemented") // Remove placeholder

	log.Println("DB Transaction Rolling Back (PostgreSQL)")
	err := tx.tx.Rollback()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Errorf("DB Tx Rollback Error (PostgreSQL): %v", err)
		return fmt.Errorf("postgres Tx Rollback error: %w", err)
	}
	log.Println("DB Transaction Rolled Back (PostgreSQL)")
	return nil
}

// Get executes a query within the transaction.
func (tx *Tx) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// return fmt.Errorf("PostgreSQLTx.Get not implemented") // Remove placeholder

	reboundQuery := tx.builder.Rebind(query)
	log.Printf("DB Tx Get (PostgreSQL): %s [%v] (Original: %s)", reboundQuery, args, query)
	start := time.Now()

	rows, err := tx.tx.QueryContext(ctx, reboundQuery, args...)
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Get Error (Query - PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Tx Get query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Get Error (Columns - PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Tx Get failed fetching columns: %w", err)
	}

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("tx get: destination must be a non-nil pointer to a struct, got %T", dest)
	}
	structVal := destVal.Elem()
	scanDest, err := dbscan.PrepareScanDest(structVal, cols)
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Get Error (Prepare Scan - PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Tx Get setup error: %w", err)
	}

	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > 1 {
			duration := time.Since(start)
			log.Errorf("DB Tx Get Error (Multiple Rows - PostgreSQL) (%s)", duration)
			return fmt.Errorf("postgres Tx Get error: expected 1 row, got multiple")
		}
		err = rows.Scan(scanDest...)
		if err != nil {
			duration := time.Since(start)
			log.Errorf("DB Tx Get Error (Scan - PostgreSQL) (%s): %v", duration, err)
			return fmt.Errorf("postgres Tx Get scan error: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Get Error (Rows Iteration - PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Tx Get rows error: %w", err)
	}

	if rowCount == 0 {
		duration := time.Since(start)
		log.Printf("DB Tx Get (No Rows - PostgreSQL): %s [%v] (%s)", reboundQuery, args, duration)
		return common.ErrNotFound
	}

	duration := time.Since(start)
	log.Printf("DB Tx Get (Success - PostgreSQL): %s [%v] (%s)", reboundQuery, args, duration)
	return nil
}

// Select executes a query within the transaction.
func (tx *Tx) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// return fmt.Errorf("PostgreSQLTx.Select not implemented") // Remove placeholder

	reboundQuery := tx.builder.Rebind(query)
	log.Printf("DB Tx Select (PostgreSQL): %s [%v] (Original: %s)", reboundQuery, args, query)
	start := time.Now()

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("tx select: destination must be a pointer to a slice, got %T", dest)
	}
	sliceVal := destVal.Elem()
	elemType := sliceVal.Type().Elem()

	rows, err := tx.tx.QueryContext(ctx, reboundQuery, args...)
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Select Query Error (PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Tx Select query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Errorf("DB Tx Select Error (Fetching Columns - PostgreSQL): %v", err)
		return fmt.Errorf("postgres Tx Select failed fetching columns: %w", err)
	}

	isBasicTypeSlice := dbscan.IsBasicType(elemType)
	isPtrElem := elemType.Kind() == reflect.Ptr
	baseElemType := elemType
	if isPtrElem {
		baseElemType = elemType.Elem()
	}

	if !isBasicTypeSlice && baseElemType.Kind() != reflect.Struct {
		return fmt.Errorf("tx select: destination slice element type must be struct, pointer to struct, or basic type, got %s", elemType.String())
	}

	rowCount := 0
	for rows.Next() {
		var elemToScan reflect.Value
		var scanDest []interface{}
		var setupErr error

		if isBasicTypeSlice {
			elemPtr := reflect.New(elemType)
			elemToScan = elemPtr
			scanDest = []interface{}{elemToScan.Interface()}
		} else {
			newElemPtrVal := reflect.New(baseElemType)
			elemToScan = newElemPtrVal
			scanDest, setupErr = dbscan.PrepareScanDest(newElemPtrVal.Elem(), cols)
			if setupErr != nil {
				duration := time.Since(start)
				log.Errorf("DB Tx Select Error (Prepare Scan - PostgreSQL) (%s): %v", duration, setupErr)
				return fmt.Errorf("postgres Tx Select row setup error: %w", setupErr)
			}
		}

		if err := rows.Scan(scanDest...); err != nil {
			duration := time.Since(start)
			log.Errorf("DB Tx Select Scan Error (PostgreSQL) (%s): %v", duration, err)
			return fmt.Errorf("postgres Tx Select scan error: %w", err)
		}

		var valToAppend reflect.Value
		if isBasicTypeSlice {
			valToAppend = elemToScan.Elem()
		} else {
			if isPtrElem {
				valToAppend = elemToScan
			} else {
				valToAppend = elemToScan.Elem()
			}
		}
		sliceVal.Set(reflect.Append(sliceVal, valToAppend))
		rowCount++
	}

	if err = rows.Err(); err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Select Rows Error (PostgreSQL) (%s): %v", duration, err)
		return fmt.Errorf("postgres Tx Select rows error: %w", err)
	}

	duration := time.Since(start)
	log.Printf("DB Tx Select OK (PostgreSQL): %s [%v] (%d rows, %s)", reboundQuery, args, rowCount, duration)
	return nil
}

// Exec executes a query within the transaction.
func (tx *Tx) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	reboundQuery := tx.builder.Rebind(query)
	start := time.Now()

	isInsert := strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "INSERT")
	if isInsert && !strings.Contains(strings.ToUpper(query), "RETURNING") {
		reboundQuery += " RETURNING id"
		var lastInsertID int64
		row := tx.tx.QueryRowContext(ctx, reboundQuery, args...)
		err := row.Scan(&lastInsertID)
		duration := time.Since(start)
		if err != nil {
			log.Errorf("DB Tx Exec Error (PostgreSQL INSERT RETURNING) (%s): %v", duration, err)
			return nil, fmt.Errorf("postgres Tx ExecContext error (insert returning): %w", err)
		}
		log.Printf("DB Tx Exec (PostgreSQL INSERT RETURNING): %s [%v] (LastInsertID: %d, %s)", reboundQuery, args, lastInsertID, duration)
		return &pgResult{lastInsertID: lastInsertID, rowsAffected: 1}, nil
	}

	result, err := tx.tx.ExecContext(ctx, reboundQuery, args...)
	duration := time.Since(start)
	if err != nil {
		log.Errorf("DB Tx Exec Error (PostgreSQL) (%s): %v", duration, err)
		return nil, fmt.Errorf("postgres Tx ExecContext error: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	log.Printf("DB Tx Exec (PostgreSQL): %s [%v] (Affected: %d) (%s)", reboundQuery, args, rowsAffected, duration)
	return result, nil
}

// --- Placeholder Rebinding Helper ---

// pgResult implements sql.Result for PostgreSQL INSERT RETURNING id
// to support LastInsertID in ORM logic.
type pgResult struct {
	lastInsertID int64
	rowsAffected int64
}

// Implement both LastInsertId (for sql.Result) and LastInsertID (for custom usage)
func (r *pgResult) LastInsertId() (int64, error) { return r.lastInsertID, nil }
func (r *pgResult) LastInsertID() (int64, error) { return r.lastInsertID, nil }
func (r *pgResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

// Register the PostgreSQL introspector factory at init time to avoid import cycles.
func init() {
	thing.RegisterIntrospectorFactory("postgres", func(adapter thing.DBAdapter) driversSchema.Introspector {
		db := adapter.DB()
		return &PostgreSQLIntrospector{DB: db}
	})
}
