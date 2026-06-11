package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"time"

	log "github.com/burugo/thing/internal/logging"

	"github.com/burugo/thing"
	"github.com/burugo/thing/common"
	driversSchema "github.com/burugo/thing/drivers/schema"
	"github.com/burugo/thing/internal/dbscan"
	"github.com/burugo/thing/internal/sqlbuilder"

	_ "github.com/go-sql-driver/mysql" // MySQL driver
)

// MySQLDialector implements the sqlbuilder.Dialector interface for MySQL.
type MySQLDialector struct{}

func (d MySQLDialector) Quote(identifier string) string {
	return "`" + identifier + "`"
}

func (d MySQLDialector) Placeholder(_ int) string {
	return "?"
}

// MySQLAdapter implements the DBAdapter interface for MySQL.
type MySQLAdapter struct {
	db      *sql.DB
	builder thing.SQLBuilder // Added SQLBuilder field
}

// MySQLTx implements the Tx interface for MySQL.
type MySQLTx struct {
	tx *sql.Tx
}

// Compile-time checks to ensure interfaces are implemented.
var (
	_ thing.DBAdapter = (*MySQLAdapter)(nil)
	_ thing.Tx        = (*MySQLTx)(nil)
)

// --- Constructor ---

// NewMySQLAdapter creates a new MySQL adapter instance.
func NewMySQLAdapter(dsn string) (thing.DBAdapter, error) {
	// return nil, fmt.Errorf("NewMySQLAdapter not yet implemented") // Remove placeholder

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql connection: %w", err)
	}

	// Verify the connection
	if err := db.Ping(); err != nil {
		db.Close() // Close the connection if Ping fails
		return nil, fmt.Errorf("failed to ping mysql database: %w", err)
	}

	// Set reasonable default connection pool settings
	db.SetMaxOpenConns(25)           // Example value
	db.SetMaxIdleConns(10)           // Example value
	db.SetConnMaxLifetime(time.Hour) // Example value

	// Create a SQLBuilder with MySQL dialect
	builder := thing.NewSQLBuilder(MySQLDialector{})

	log.Println("MySQL adapter initialized successfully.")
	return &MySQLAdapter{
		db:      db,
		builder: builder,
	}, nil
}

// --- DBAdapter Methods ---

func (a *MySQLAdapter) Close() error {
	log.Println("MySQL adapter: Closing connection")
	// return fmt.Errorf("MySQLAdapter.Close not implemented") // Remove placeholder
	if a.db != nil {
		err := a.db.Close()
		if err != nil {
			log.Printf("Error closing MySQL adapter: %v", err)
			return fmt.Errorf("error closing mysql connection: %w", err)
		}
		log.Println("MySQL adapter closed.")
		return nil
	}
	return errors.New("mysql adapter is nil or already closed")
}

// Get retrieves a single row and scans it into the destination struct.
// Uses QueryContext and prepares scan destinations based on returned columns.
// MySQL uses '\' placeholders, which is the default for database/sql.
func (a *MySQLAdapter) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// TODO: Implement using db.QueryRowContext and Scan
	// TODO: Handle sql.ErrNoRows
	// IMPORTANT: Use '?' placeholders
	// return fmt.Errorf("MySQLAdapter.Get not implemented") // Remove placeholder

	log.Printf("DB Get (MySQL): %s [%v]", query, args)
	start := time.Now()

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Get Error (Query - MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Get query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Get Error (Columns - MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Get failed fetching columns: %w", err)
	}

	// Prepare scan destinations based on destination struct type
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("get: destination must be a non-nil pointer to a struct, got %T", dest)
	}
	structVal := destVal.Elem() // The actual struct value

	scanDest, err := dbscan.PrepareScanDest(structVal, cols) // Reusable helper
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Get Error (Prepare Scan - MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Get setup error: %w", err)
	}

	// Expect exactly one row
	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > 1 {
			duration := time.Since(start)
			log.Errorf("DB Get Error (Multiple Rows - MySQL) (%s)", duration)
			return fmt.Errorf("mysql Get error: expected 1 row, got multiple")
		}
		err = rows.Scan(scanDest...)
		if err != nil {
			duration := time.Since(start)
			log.Errorf("DB Get Error (Scan - MySQL) (%s): %v", duration, err)
			return fmt.Errorf("mysql Get scan error: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		duration := time.Since(start)
		log.Errorf("DB Get Error (Rows Iteration - MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Get rows error: %w", err)
	}

	// Check if any row was found
	if rowCount == 0 {
		duration := time.Since(start)
		log.Printf("DB Get (No Rows - MySQL): %s [%v] (%s)", query, args, duration)
		return common.ErrNotFound // Use common.ErrNotFound
	}

	duration := time.Since(start)
	log.Printf("DB Get (Success - MySQL): %s [%v] (%s)", query, args, duration)
	return nil
}

// Select executes a query and scans the results into a slice.
// MySQL uses '?' placeholders.
func (a *MySQLAdapter) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	log.Printf("DB Select (MySQL): %s [%v]", query, args)
	start := time.Now()

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("select: destination must be a pointer to a slice, got %T", dest)
	}
	sliceVal := destVal.Elem()
	elemType := sliceVal.Type().Elem()

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Select Query Error (MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Select query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Errorf("DB Select Error (Fetching Columns - MySQL): %v", err)
		return fmt.Errorf("mysql Select failed fetching columns: %w", err)
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
		err := a.scanAndAppendRowForSelect(rows, &sliceVal, isBasicTypeSlice, elemType, baseElemType, isPtrElem, cols)
		if err != nil {
			duration := time.Since(start)
			log.Errorf("DB Select Error (Scan Row - MySQL) (%s): %v", duration, err)
			return fmt.Errorf("mysql Select scan row error: %w", err)
		}
		rowCount++
	}

	if err = rows.Err(); err != nil {
		duration := time.Since(start)
		log.Errorf("DB Select Rows Error (MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Select rows error: %w", err)
	}

	duration := time.Since(start)
	log.Printf("DB Select OK (MySQL): %s [%v] (%d rows, %s)", query, args, rowCount, duration)
	return nil
}

// scanAndAppendRowForSelect 扫描单行并追加到 slice
func (a *MySQLAdapter) scanAndAppendRowForSelect(rows *sql.Rows, sliceVal *reflect.Value, isBasicTypeSlice bool, elemType, baseElemType reflect.Type, isPtrElem bool, cols []string) error {
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
			return setupErr
		}
	}

	if err := rows.Scan(scanDest...); err != nil {
		return err
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
	sliceVal.Set(reflect.Append(*sliceVal, valToAppend))
	return nil
}

// Exec executes a query that doesn't return rows (INSERT, UPDATE, DELETE).
// MySQL uses '?' placeholders.
func (a *MySQLAdapter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	// TODO: Implement using db.ExecContext
	// IMPORTANT: Use '?' placeholders
	// return nil, fmt.Errorf("MySQLAdapter.Exec not implemented") // Remove placeholder

	start := time.Now()
	result, err := a.db.ExecContext(ctx, query, args...)
	duration := time.Since(start)
	if err != nil {
		log.Errorf("DB Exec Error (MySQL) (%s): %v", duration, err)
		return nil, fmt.Errorf("mysql ExecContext error: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()
	log.Printf("DB Exec (MySQL): %s [%v] (Affected: %d, LastInsertID: %d) (%s)", query, args, rowsAffected, lastInsertID, duration)
	return result, nil
}

// GetCount executes a SELECT COUNT(*) query based on the provided parameters.
func (a *MySQLAdapter) GetCount(ctx context.Context, tableName string, where string, args []interface{}) (int64, error) {
	if tableName == "" {
		return 0, errors.New("getCount: table name is missing")
	}
	if where != "" {
		where, args = sqlbuilder.ExpandInClauses(MySQLDialector{}, where, args)
	}
	query := a.builder.BuildCountSQL(tableName, where)
	log.Printf("DB GetCount (MySQL): %s [%v]", query, args)
	row := a.db.QueryRowContext(ctx, query, args...)
	var count int64
	err := row.Scan(&count)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("mysql GetCount scan error: %w", err)
	}
	return count, nil
}

// BeginTx starts a transaction.
func (a *MySQLAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (thing.Tx, error) {
	// TODO: Implement using db.BeginTx
	// TODO: Wrap the sql.Tx in MySQLTx
	// return nil, fmt.Errorf("MySQLAdapter.BeginTx not implemented") // Remove placeholder

	log.Println("DB Transaction Started (MySQL)")
	tx, err := a.db.BeginTx(ctx, opts)
	if err != nil {
		log.Errorf("DB BeginTx Error (MySQL): %v", err)
		return nil, fmt.Errorf("mysql BeginTx error: %w", err)
	}
	return &MySQLTx{tx: tx}, nil
}

// --- DBTransaction Methods (now Tx Methods) ---

// Commit commits the transaction.
func (tx *MySQLTx) Commit() error {
	// TODO: Implement using tx.tx.Commit()
	// return fmt.Errorf("MySQLTx.Commit not implemented") // Remove placeholder

	log.Println("DB Transaction Committing (MySQL)")
	err := tx.tx.Commit()
	if err != nil {
		log.Errorf("DB Tx Commit Error (MySQL): %v", err)
		return fmt.Errorf("mysql Tx Commit error: %w", err)
	}
	log.Println("DB Transaction Committed (MySQL)")
	return nil
}

// Rollback rolls back the transaction.
func (tx *MySQLTx) Rollback() error {
	// TODO: Implement using tx.tx.Rollback()
	// return fmt.Errorf("MySQLTx.Rollback not implemented") // Remove placeholder

	log.Println("DB Transaction Rolling Back (MySQL)")
	err := tx.tx.Rollback()
	if err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Errorf("DB Tx Rollback Error (MySQL): %v", err)
		return fmt.Errorf("mysql Tx Rollback error: %w", err)
	}
	log.Println("DB Transaction Rolled Back (MySQL)")
	return nil
}

// Get executes a query within the transaction.
func (tx *MySQLTx) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// TODO: Implement using tx.tx.QueryRowContext and Scan
	// IMPORTANT: Use '?' placeholders
	// return fmt.Errorf("MySQLTx.Get not implemented") // Remove placeholder

	log.Printf("DB Tx Get (MySQL): %s [%v]", query, args)
	start := time.Now()

	rows, err := tx.tx.QueryContext(ctx, query, args...)
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Get Error (Query - MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Tx Get query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Get Error (Columns - MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Tx Get failed fetching columns: %w", err)
	}

	// Prepare scan destinations
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("tx get: destination must be a non-nil pointer to a struct, got %T", dest)
	}
	structVal := destVal.Elem()
	scanDest, err := dbscan.PrepareScanDest(structVal, cols)
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Get Error (Prepare Scan - MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Tx Get setup error: %w", err)
	}

	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > 1 {
			duration := time.Since(start)
			log.Errorf("DB Tx Get Error (Multiple Rows - MySQL) (%s)", duration)
			return fmt.Errorf("mysql Tx Get error: expected 1 row, got multiple")
		}
		err = rows.Scan(scanDest...)
		if err != nil {
			duration := time.Since(start)
			log.Errorf("DB Tx Get Error (Scan - MySQL) (%s): %v", duration, err)
			return fmt.Errorf("mysql Tx Get scan error: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Get Error (Rows Iteration - MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Tx Get rows error: %w", err)
	}

	if rowCount == 0 {
		duration := time.Since(start)
		log.Printf("DB Tx Get (No Rows - MySQL): %s [%v] (%s)", query, args, duration)
		return common.ErrNotFound
	}

	duration := time.Since(start)
	log.Printf("DB Tx Get (Success - MySQL): %s [%v] (%s)", query, args, duration)
	return nil
}

// Select executes a query within the transaction.
func (tx *MySQLTx) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// TODO: Implement using tx.tx.QueryContext and rows.Scan
	// IMPORTANT: Use '?' placeholders
	// return fmt.Errorf("MySQLTx.Select not implemented") // Remove placeholder

	log.Printf("DB Tx Select (MySQL): %s [%v]", query, args)
	start := time.Now()

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("tx select: destination must be a pointer to a slice, got %T", dest)
	}
	sliceVal := destVal.Elem()
	elemType := sliceVal.Type().Elem()

	rows, err := tx.tx.QueryContext(ctx, query, args...)
	if err != nil {
		duration := time.Since(start)
		log.Errorf("DB Tx Select Query Error (MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Tx Select query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Errorf("DB Tx Select Error (Fetching Columns - MySQL): %v", err)
		return fmt.Errorf("mysql Tx Select failed fetching columns: %w", err)
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
				log.Errorf("DB Tx Select Error (Prepare Scan - MySQL) (%s): %v", duration, setupErr)
				return fmt.Errorf("mysql Tx Select row setup error: %w", setupErr)
			}
		}

		if err := rows.Scan(scanDest...); err != nil {
			duration := time.Since(start)
			log.Errorf("DB Tx Select Scan Error (MySQL) (%s): %v", duration, err)
			return fmt.Errorf("mysql Tx Select scan error: %w", err)
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
		log.Errorf("DB Tx Select Rows Error (MySQL) (%s): %v", duration, err)
		return fmt.Errorf("mysql Tx Select rows error: %w", err)
	}

	duration := time.Since(start)
	log.Printf("DB Tx Select OK (MySQL): %s [%v] (%d rows, %s)", query, args, rowCount, duration)
	return nil
}

// Exec executes a query within the transaction.
func (tx *MySQLTx) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	// TODO: Implement using tx.tx.ExecContext
	// IMPORTANT: Use '?' placeholders
	// return nil, fmt.Errorf("MySQLTx.Exec not implemented") // Remove placeholder

	start := time.Now()
	result, err := tx.tx.ExecContext(ctx, query, args...)
	duration := time.Since(start)
	if err != nil {
		log.Errorf("DB Tx Exec Error (MySQL) (%s): %v", duration, err)
		return nil, fmt.Errorf("mysql Tx ExecContext error: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()
	log.Printf("DB Tx Exec (MySQL): %s [%v] (Affected: %d, LastInsertID: %d) (%s)", query, args, rowsAffected, lastInsertID, duration)
	return result, nil
}

// DB returns the underlying *sql.DB for advanced use cases.
func (a *MySQLAdapter) DB() *sql.DB {
	return a.db
}

func (a *MySQLAdapter) Builder() thing.SQLBuilder {
	return a.builder
}

func (a *MySQLAdapter) DialectName() string {
	return "mysql"
}

// Register the MySQL introspector factory at init time to avoid import cycles.
func init() {
	thing.RegisterIntrospectorFactory("mysql", func(adapter thing.DBAdapter) driversSchema.Introspector {
		db := adapter.DB()
		return &MySQLIntrospector{DB: db}
	})
}
