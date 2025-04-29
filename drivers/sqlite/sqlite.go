package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	// "github.com/jmoiron/sqlx" // Removed sqlx import
	_ "github.com/mattn/go-sqlite3" // SQLite driver

	// Import the core package (now at module root)
	thing "thing"
)

// SQLiteAdapter implements the thing.DBAdapter interface for SQLite.
type SQLiteAdapter struct {
	db      *sql.DB // Changed from *sqlx.DB
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
	// db, err := sqlx.Connect("sqlite3", dsn) // Replaced sqlx.Connect
	db, err := sql.Open("sqlite3", dsn) // Use sql.Open
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite connection: %w", err)
	}

	// Configure connection pool settings (optional but recommended)
	db.SetMaxOpenConns(25)                 // Example value
	db.SetMaxIdleConns(5)                  // Example value
	db.SetConnMaxLifetime(5 * time.Minute) // Example value

	// Ping the database to verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // Add timeout for ping
	defer cancel()
	if err := db.PingContext(ctx); err != nil { // Use PingContext explicitly
		db.Close() // Close the pool if ping fails
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	log.Println("SQLite adapter initialized successfully.")
	return &SQLiteAdapter{db: db, dsn: dsn}, nil
}

// Get retrieves a single row and scans it into the destination struct.
// Uses standard sql.DB.QueryRowContext and Scan.
func (a *SQLiteAdapter) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if a.isClosed() {
		return fmt.Errorf("adapter is closed")
	}
	log.Printf("DB Get (sql): %s [%v]", query, args) // Add logging for the raw SQL
	start := time.Now()

	row := a.db.QueryRowContext(ctx, query, args...)

	// Use the new getFieldPointers to get scan destinations
	pointers, err := getFieldPointers(dest) // Pass dest directly
	if err != nil {
		log.Printf("DB Get Error (Pointer Setup): %s [%v] - %v", query, args, err)
		return fmt.Errorf("sqlite Get setup error: %w", err)
	}

	// Scan the row directly into the pointers
	err = row.Scan(pointers...)
	duration := time.Since(start)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("DB Get (No Rows): %s [%v] (%s)", query, args, duration)
			return thing.ErrNotFound // Use error from parent package
		}
		log.Printf("DB Get Error (Scan): %s [%v] (%s) - %v", query, args, duration, err) // Clarify error source
		return fmt.Errorf("sqlite Get scan error: %w", err)
	}
	log.Printf("DB Get (Success): %s [%v] (%s)", query, args, duration) // Clarify success log
	// Remove duplicate log line
	return nil
}

// Select executes a query and scans the results into a slice.
// The 'dest' argument must be a pointer to a slice.
// Uses standard sql.DB.QueryContext and manual row scanning.
func (a *SQLiteAdapter) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if a.isClosed() {
		return fmt.Errorf("adapter is closed")
	}

	log.Printf("DB Select (sql): %s [%v]", query, args) // Log the query first
	start := time.Now()                                 // Keep track of time

	// --- Destination Type Check ---
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("Select: destination must be a pointer to a slice, got %T", dest)
	}
	sliceVal := destVal.Elem()         // The slice itself
	elemType := sliceVal.Type().Elem() // The type of elements in the slice

	isBasicTypeSlice := false
	switch elemType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String,
		reflect.Bool:
		isBasicTypeSlice = true
	}
	// --- End Destination Type Check ---

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		// Log error before returning
		duration := time.Since(start)
		log.Printf("DB Select Query Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Select query error: %w", err)
	}
	defer rows.Close()

	rowCount := 0

	if isBasicTypeSlice {
		// --- Handle Slice of Basic Types (e.g., []int64, []string) ---
		for rows.Next() {
			// Create a pointer to a new value of the slice's element type
			elemPtr := reflect.New(elemType) // e.g., *int64

			// Scan directly into the pointer
			if err := rows.Scan(elemPtr.Interface()); err != nil {
				duration := time.Since(start)
				log.Printf("DB Select Scan Error (Basic Type): %s [%v] (%s) - %v", query, args, duration, err)
				return fmt.Errorf("sqlite Select scan (basic type) error: %w", err)
			}

			// Append the actual value (not the pointer) to the slice
			sliceVal.Set(reflect.Append(sliceVal, elemPtr.Elem())) // Append the dereferenced value
			rowCount++
		}
	} else {
		// --- Handle Slice of Structs or Pointers to Structs ---
		isPtrElem := elemType.Kind() == reflect.Ptr
		baseElemType := elemType
		if isPtrElem {
			baseElemType = elemType.Elem() // Get the underlying struct type if slice elements are pointers
		}
		if baseElemType.Kind() != reflect.Struct {
			return fmt.Errorf("Select: destination slice element type must be a struct or pointer to struct (or basic type), got %s", elemType.String())
		}

		for rows.Next() {
			// Create a new element (pointer to struct) of the base type
			newElemPtrVal := reflect.New(baseElemType) // e.g. *User

			pointers, err := getFieldPointers(newElemPtrVal.Interface()) // Get pointers to fields of *User
			if err != nil {
				duration := time.Since(start)
				log.Printf("DB Select Error (Pointer Setup): %s [%v] (%s) - %v", query, args, duration, err)
				return fmt.Errorf("sqlite Select row setup error: %w", err)
			}

			if err := rows.Scan(pointers...); err != nil {
				duration := time.Since(start)
				log.Printf("DB Select Scan Error (Struct): %s [%v] (%s) - %v", query, args, duration, err)
				return fmt.Errorf("sqlite Select scan (struct) error: %w", err)
			}

			// Append the new element (pointer or value) to the destination slice
			var appendErr error
			if isPtrElem { // If dest is *[]*User
				appendErr = appendValueToSlice(dest, newElemPtrVal.Interface()) // Append *User
			} else { // If dest is *[]User
				appendErr = appendValueToSlice(dest, newElemPtrVal.Elem().Interface()) // Append User
			}
			if appendErr != nil {
				duration := time.Since(start)
				log.Printf("DB Select Append Error: %s [%v] (%s) - %v", query, args, duration, appendErr)
				return fmt.Errorf("sqlite Select: failed to append value: %w", appendErr)
			}
			rowCount++
		}
	}

	// --- Check for errors during iteration ---
	if err = rows.Err(); err != nil {
		duration := time.Since(start)
		log.Printf("DB Select Rows Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Select rows error: %w", err)
	}

	duration := time.Since(start)
	log.Printf("DB Select OK: %s [%v] (%d rows, %s)", query, args, rowCount, duration) // Log success
	return nil
}

// Exec executes a query that doesn't return rows (INSERT, UPDATE, DELETE).
// Uses standard sql.DB.ExecContext
func (a *SQLiteAdapter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if a.isClosed() {
		return nil, fmt.Errorf("adapter is closed")
	}
	// TODO: Query logging?
	start := time.Now()
	result, err := a.db.ExecContext(ctx, query, args...) // Already compatible
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
// TODO: Needs reimplementation using QueryRowContext and Scan.
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

	log.Printf("DB GetCount (sql): %s [%v]", query, args)
	start := time.Now()
	// err := a.db.GetContext(ctx, &count, query, args...) // Original sqlx call
	row := a.db.QueryRowContext(ctx, query, args...)
	err := row.Scan(&count) // Direct scan for single value
	duration := time.Since(start)

	if err != nil {
		// Standard QueryRowContext().Scan() returns sql.ErrNoRows if the query itself returns no rows,
		// which shouldn't happen for COUNT(*). So, any error here is likely a real issue.
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("WARN: GetCount query returned no rows unexpectedly for query: %s [%v] (%s)", query, args, duration)
			return 0, nil // Interpret as 0 count if ErrNoRows occurs
		}
		log.Printf("DB GetCount Error: %s [%v] (%s) - %v", query, args, duration, err)
		return 0, fmt.Errorf("sqlite GetCount error: %w", err)
	}

	log.Printf("DB GetCount Result: %d (%s)", count, duration)
	return count, nil
}

// SelectPaginated executes a query including WHERE, ORDER BY, LIMIT, and OFFSET clauses.
// TODO: Needs reimplementation using QueryContext and manual row iteration/Scan.
func (a *SQLiteAdapter) SelectPaginated(ctx context.Context, dest interface{}, info *thing.ModelInfo, params thing.QueryParams, offset int, limit int) error {
	if a.isClosed() {
		return fmt.Errorf("adapter is closed")
	}
	if info == nil || info.TableName == "" || len(info.Columns) == 0 {
		return errors.New("SelectPaginated: model info, table name, or columns list is missing")
	}

	// Build base SELECT part (duplicate logic from thing.buildSelectSQL)
	// IMPORTANT: The order of columns here MUST match the field order in the struct
	// for the manual Scan to work correctly. Using info.Columns assumes this is the case.
	quotedColumns := make([]string, len(info.Columns))
	for i, col := range info.Columns {
		quotedColumns[i] = fmt.Sprintf("\"%s\"", col) // Assuming SQLite quoting needed
	}
	// Ensure we select the primary key if it's not already included, needed for scanning potentially?
	// pkCol := fmt.Sprintf("\"%s\"", info.PrimaryKeyColumn()) // Assuming method exists
	// if !slices.Contains(quotedColumns, pkCol) {
	// 	quotedColumns = append(quotedColumns, pkCol)
	// }
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
		queryBuilder.WriteString(" LIMIT -1 OFFSET ?")
		args = append(args, offset)
	}

	query := queryBuilder.String()

	log.Printf("DB SelectPaginated (sql): %s [%v]", query, args)
	start := time.Now()
	// err := a.db.SelectContext(ctx, dest, query, args...) // Original sqlx call
	// --- Manual Scan Logic Start ---
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("DB SelectPaginated Error: %s [%v] - %v", query, args, err)
		return fmt.Errorf("sqlite SelectPaginated error: %w", err)
	}
	defer rows.Close()
	// TODO: Implement slice element creation and getFieldPointers
	// TODO: Implement appendValueToSlice
	// for rows.Next() { ... rows.Scan(...) ... append ... }
	// if err = rows.Err(); err != nil { ... }
	// --- Manual Scan Logic End ---
	err = errors.New("SelectPaginated method not fully implemented after removing sqlx") // Temporary error
	rows, err = a.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("DB SelectPaginated Query Error: %s [%v] - %v", query, args, err)
		return fmt.Errorf("sqlite SelectPaginated query error: %w", err)
	}
	defer rows.Close()

	// Determine the element type of the destination slice
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("SelectPaginated: destination must be a pointer to a slice, got %T", dest)
	}
	sliceType := destVal.Elem().Type()
	elemType := sliceType.Elem()
	isPtrElem := elemType.Kind() == reflect.Ptr
	baseElemType := elemType
	if isPtrElem {
		baseElemType = elemType.Elem()
	}

	for rows.Next() {
		// Create a new element (pointer or struct) of the correct type
		newElemPtrVal := reflect.New(baseElemType)

		pointers, err := getFieldPointers(newElemPtrVal.Interface())
		if err != nil {
			log.Printf("DB SelectPaginated Error (Pointer Setup): %s [%v] - %v", query, args, err)
			return fmt.Errorf("sqlite SelectPaginated row setup error: %w", err)
		}

		if err := rows.Scan(pointers...); err != nil {
			log.Printf("DB SelectPaginated Scan Error: %s [%v] - %v", query, args, err)
			return fmt.Errorf("sqlite SelectPaginated scan error: %w", err)
		}

		// Append the new element (pointer or value) to the destination slice
		if isPtrElem {
			if err := appendValueToSlice(dest, newElemPtrVal.Interface()); err != nil {
				return fmt.Errorf("SelectPaginated: failed to append pointer value: %w", err)
			}
		} else {
			if err := appendValueToSlice(dest, newElemPtrVal.Elem().Interface()); err != nil {
				return fmt.Errorf("SelectPaginated: failed to append value: %w", err)
			}
		}
	}

	if err = rows.Err(); err != nil {
		log.Printf("DB SelectPaginated Rows Error: %s [%v] - %v", query, args, err)
		return fmt.Errorf("sqlite SelectPaginated rows error: %w", err)
	}
	duration := time.Since(start)

	if err != nil {
		log.Printf("DB SelectPaginated Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite SelectContext (paginated) error: %w", err)
	}

	log.Printf("DB SelectPaginated Success (%s)", duration)
	log.Printf("DB SelectPaginated Success (%s)", duration)
	return nil
}

// BeginTx starts a new transaction.
func (a *SQLiteAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (thing.Tx, error) { // Return interface type
	if a.isClosed() {
		return nil, fmt.Errorf("adapter is closed")
	}
	// tx, err := a.db.BeginTxx(ctx, opts) // Replaced BeginTxx
	tx, err := a.db.BeginTx(ctx, opts) // Use standard BeginTx
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
	err := a.db.Close() // Already compatible
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

// SQLiteTx wraps sql.Tx to implement the thing.Tx interface.
type SQLiteTx struct {
	tx *sql.Tx // Changed from *sqlx.Tx
}

// Ensure SQLiteTx implements thing.Tx.
var _ thing.Tx = (*SQLiteTx)(nil)

// Get executes a query within the transaction, scanning into dest (which must be a pointer).
// TODO: Needs reimplementation using QueryRowContext and manual Scan.
func (t *SQLiteTx) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// Placeholder for manual scanning logic
	log.Printf("DB Tx Get (sql): %s [%v]", query, args)
	// err := t.tx.GetContext(ctx, dest, query, args...) // Original sqlx call
	// --- Manual Scan Logic Start ---
	row := t.tx.QueryRowContext(ctx, query, args...)
	_ = row // Assign to blank identifier to silence linter temporarily
	// TODO: Implement getFieldPointers(dest) using reflection
	// pointers := getFieldPointers(dest)
	// err := row.Scan(pointers...)
	// --- Manual Scan Logic End ---
	err := errors.New("Tx.Get method not fully implemented after removing sqlx") // Temporary error
	start := time.Now()                                                          // Keep track of time
	row = t.tx.QueryRowContext(ctx, query, args...)

	pointers, err := getFieldPointers(dest)
	if err != nil {
		log.Printf("DB Tx Get Error (Pointer Setup): %s [%v] - %v", query, args, err)
		return fmt.Errorf("sqlite Tx Get setup error: %w", err)
	}

	err = row.Scan(pointers...)
	duration := time.Since(start)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("DB Tx Get (No Rows): %s [%v] (%s)", query, args, duration)
			return thing.ErrNotFound // Use error from parent package
		}
		log.Printf("DB Tx Get Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Tx Get error: %w", err)
	}
	log.Printf("DB Tx Get: %s [%v] (%s)", query, args, duration) // Duration removed
	log.Printf("DB Tx Get: %s [%v] (%s)", query, args, duration)
	return nil
}

// Select executes a query within the transaction.
// TODO: Needs reimplementation using QueryContext and manual row iteration/Scan.
func (t *SQLiteTx) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	// Placeholder for manual scanning logic
	log.Printf("DB Tx Select (sql): %s [%v]", query, args)
	// err := t.tx.SelectContext(ctx, dest, query, args...) // Original sqlx call
	// --- Manual Scan Logic Start ---
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("DB Tx Select Error: %s [%v] - %v", query, args, err)
		return fmt.Errorf("sqlite Tx Select error: %w", err)
	}
	defer rows.Close()
	// TODO: Implement slice element creation and getFieldPointers
	// TODO: Implement appendValueToSlice
	// for rows.Next() { ... rows.Scan(...) ... append ... }
	// if err = rows.Err(); err != nil { ... }
	// --- Manual Scan Logic End ---
	err = errors.New("Tx.Select method not fully implemented after removing sqlx") // Temporary error
	start := time.Now()                                                            // Keep track of time
	rows, err = t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("DB Tx Select Query Error: %s [%v] - %v", query, args, err)
		return fmt.Errorf("sqlite Tx Select query error: %w", err)
	}
	defer rows.Close()

	// Determine the element type of the destination slice
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("Tx Select: destination must be a pointer to a slice, got %T", dest)
	}
	sliceType := destVal.Elem().Type()
	elemType := sliceType.Elem()
	isPtrElem := elemType.Kind() == reflect.Ptr
	baseElemType := elemType
	if isPtrElem {
		baseElemType = elemType.Elem()
	}

	for rows.Next() {
		// Create a new element (pointer or struct) of the correct type
		newElemPtrVal := reflect.New(baseElemType)

		pointers, err := getFieldPointers(newElemPtrVal.Interface())
		if err != nil {
			log.Printf("DB Tx Select Error (Pointer Setup): %s [%v] - %v", query, args, err)
			return fmt.Errorf("sqlite Tx Select row setup error: %w", err)
		}

		if err := rows.Scan(pointers...); err != nil {
			log.Printf("DB Tx Select Scan Error: %s [%v] - %v", query, args, err)
			return fmt.Errorf("sqlite Tx Select scan error: %w", err)
		}

		// Append the new element (pointer or value) to the destination slice
		if isPtrElem {
			if err := appendValueToSlice(dest, newElemPtrVal.Interface()); err != nil {
				return fmt.Errorf("Tx Select: failed to append pointer value: %w", err)
			}
		} else {
			if err := appendValueToSlice(dest, newElemPtrVal.Elem().Interface()); err != nil {
				return fmt.Errorf("Tx Select: failed to append value: %w", err)
			}
		}
	}

	if err = rows.Err(); err != nil {
		log.Printf("DB Tx Select Rows Error: %s [%v] - %v", query, args, err)
		return fmt.Errorf("sqlite Tx Select rows error: %w", err)
	}
	duration := time.Since(start)
	if err != nil {
		log.Printf("DB Tx Select Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Tx Select error: %w", err)
	}
	log.Printf("DB Tx Select: %s [%v] (%s)", query, args, duration) // Duration removed
	log.Printf("DB Tx Select: %s [%v] (%s)", query, args, duration)
	return nil
}

// Exec executes a statement within the transaction.
// Uses standard sql.Tx.ExecContext
func (t *SQLiteTx) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	// TODO: Query logging?
	start := time.Now()
	result, err := t.tx.ExecContext(ctx, query, args...) // Already compatible
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
// Uses standard sql.Tx.Commit
func (t *SQLiteTx) Commit() error {
	err := t.tx.Commit() // Already compatible
	if err != nil {
		log.Printf("DB Transaction Commit Error: %v", err)
		return fmt.Errorf("sqlite Tx Commit error: %w", err)
	}
	log.Println("DB Transaction Committed")
	return nil
}

// Rollback rolls back the transaction.
// Uses standard sql.Tx.Rollback
func (t *SQLiteTx) Rollback() error {
	err := t.tx.Rollback() // Already compatible
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

// --- Helper Functions ---

// getFieldPointers recursively extracts pointers to the fields of a struct,
// handling nested and embedded structs. It skips fields tagged with `db:"-"`.
// It returns a slice of interface{} pointers suitable for sql.Rows.Scan.
func getFieldPointers(destPtr interface{}) ([]interface{}, error) {
	val := reflect.ValueOf(destPtr)
	if val.Kind() != reflect.Ptr || val.IsNil() {
		return nil, fmt.Errorf("destination must be a non-nil pointer, got %T", destPtr)
	}

	elem := val.Elem()
	if elem.Kind() != reflect.Struct {
		return nil, fmt.Errorf("destination must point to a struct, got %T", destPtr)
	}

	pointers := make([]interface{}, 0)
	typ := elem.Type()

	for i := 0; i < elem.NumField(); i++ {
		fieldVal := elem.Field(i)
		fieldType := typ.Field(i)

		// Skip unexported fields
		if !fieldType.IsExported() {
			log.Printf("Skipping unexported field: %s", fieldType.Name)
			continue
		}

		// Check for db:"-" tag
		dbTag := fieldType.Tag.Get("db")
		if dbTag == "-" {
			log.Printf("Skipping field with db:\"-\" tag: %s", fieldType.Name)
			continue
		}

		// Handle embedded structs recursively
		if fieldType.Anonymous && fieldVal.Kind() == reflect.Struct {
			log.Printf("Recursing into embedded struct: %s", fieldType.Name)
			// We need to pass the pointer to the embedded struct field
			if fieldVal.CanAddr() {
				nestedPointers, err := getFieldPointers(fieldVal.Addr().Interface())
				if err != nil {
					return nil, fmt.Errorf("error getting pointers for embedded struct %s: %w", fieldType.Name, err)
				}
				pointers = append(pointers, nestedPointers...)
			} else {
				// This case might occur if the embedded struct itself wasn't addressable,
				// which could be unusual but possible depending on how it's defined.
				log.Printf("Warning: Cannot get address of embedded struct field %s", fieldType.Name)
				// Optionally return an error or handle differently
				return nil, fmt.Errorf("cannot get address of embedded struct field %s", fieldType.Name)
			}
			continue // Move to the next field after handling the embedded struct
		}

		// Handle regular fields (including nested structs that are NOT embedded)
		if fieldVal.CanAddr() {
			log.Printf("Adding pointer for field: %s", fieldType.Name)
			pointers = append(pointers, fieldVal.Addr().Interface())
		} else {
			log.Printf("Warning: Cannot get address of field %s", fieldType.Name)
			// This might happen for unexported fields within exported structs,
			// but we should have skipped unexported fields earlier.
			// It might also indicate other complex scenarios.
			return nil, fmt.Errorf("cannot get address of field %s", fieldType.Name)
		}
	}

	if len(pointers) == 0 {
		log.Printf("Warning: No scannable fields found in struct type %s", typ.Name())
		// Depending on requirements, this might be an error or just an empty result.
		// return nil, fmt.Errorf("no scannable fields found in struct type %s", typ.Name())
	}

	log.Printf("getFieldPointers finished for type %s. Found %d fields.", typ.Name(), len(pointers))
	return pointers, nil
}

// appendValueToSlice appends a value (which can be a struct or a pointer to a struct)
// to a slice (passed as a pointer). It handles creating pointers if the slice element type is a pointer.
func appendValueToSlice(slicePtr interface{}, value interface{}) error {
	sliceVal := reflect.ValueOf(slicePtr)
	if sliceVal.Kind() != reflect.Ptr {
		return fmt.Errorf("appendValueToSlice: expected a pointer to a slice, got %T", slicePtr)
	}

	slice := sliceVal.Elem()
	if slice.Kind() != reflect.Slice {
		return fmt.Errorf("appendValueToSlice: expected a pointer to a slice, got pointer to %s", slice.Kind())
	}

	valueVal := reflect.ValueOf(value)

	// Ensure the value type matches the slice element type
	if slice.Type().Elem() != valueVal.Type() {
		// Allow appending *T to []T if needed? No, let's keep it strict for now.
		return fmt.Errorf("appendValueToSlice: value type %s does not match slice element type %s", valueVal.Type(), slice.Type().Elem())
	}

	// Append the value to the slice
	newSlice := reflect.Append(slice, valueVal)

	// Update the original slice via the pointer
	if slice.CanSet() {
		slice.Set(newSlice)
		return nil
	}
	return fmt.Errorf("appendValueToSlice: cannot set value on slice %T", slicePtr)
}
