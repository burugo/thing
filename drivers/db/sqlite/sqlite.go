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

	"github.com/burugo/thing"
	"github.com/burugo/thing/common"
	"github.com/burugo/thing/drivers/schema"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

const (
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 5 * time.Minute
)

// SQLiteDialector implements the sqlbuilder.Dialector interface for SQLite.
type SQLiteDialector struct{}

func (d SQLiteDialector) Quote(identifier string) string {
	return `"` + identifier + `"`
}

func (d SQLiteDialector) Placeholder(_ int) string {
	return "?"
}

// SQLiteAdapter implements the thing.DBAdapter interface for SQLite.
type SQLiteAdapter struct {
	db      *sql.DB // Changed from *sqlx.DB
	dsn     string
	closeMx sync.Mutex
	closed  bool
	builder thing.SQLBuilder // Added SQLBuilder field
}

// NewSQLiteAdapter creates a new SQLite database adapter.
// Implements the public DBAdapter interface.
func NewSQLiteAdapter(dsn string) (*SQLiteAdapter, error) {
	log.Printf("Initializing SQLite adapter with DSN: %s", dsn)
	// db, err := sqlx.Connect("sqlite3", dsn) // Replaced sqlx.Connect
	db, err := sql.Open("sqlite3", dsn) // Use sql.Open
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite connection: %w", err)
	}

	// Configure connection pool settings (optional but recommended)
	db.SetMaxOpenConns(defaultMaxOpenConns)       // Example value
	db.SetMaxIdleConns(defaultMaxIdleConns)       // Example value
	db.SetConnMaxLifetime(defaultConnMaxLifetime) // Example value

	// Ping the database to verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // Add timeout for ping
	defer cancel()
	if err := db.PingContext(ctx); err != nil { // Use PingContext explicitly
		db.Close() // Close the pool if ping fails
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	// Create a SQLBuilder with SQLite dialect
	builder := thing.NewSQLBuilder(SQLiteDialector{})

	log.Println("SQLite adapter initialized successfully.")
	return &SQLiteAdapter{
		db:      db,
		dsn:     dsn,
		builder: builder,
	}, nil
}

// Get retrieves a single row and scans it into the destination struct.
// Uses QueryContext and prepares scan destinations based on returned columns.
func (a *SQLiteAdapter) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if a.isClosed() {
		return fmt.Errorf("adapter is closed")
	}
	log.Printf("DB Get (sql): %s [%v]", query, args)
	start := time.Now()

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Get Error (Query): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Get query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Get Error (Columns): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Get failed fetching columns: %w", err)
	}

	// Prepare scan destinations based on destination struct type
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("get: destination must be a non-nil pointer to a struct, got %T", dest)
	}
	structVal := destVal.Elem() // The actual struct value

	scanDest, err := prepareScanDest(structVal, cols) // Pass struct value
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Get Error (Prepare Scan): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Get setup error: %w", err)
	}

	// Expect exactly one row
	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > 1 {
			duration := time.Since(start)
			log.Printf("DB Get Error (Multiple Rows): %s [%v] (%s)", query, args, duration)
			return fmt.Errorf("sqlite Get error: expected 1 row, got multiple")
		}
		err = rows.Scan(scanDest...)
		if err != nil {
			duration := time.Since(start)
			log.Printf("DB Get Error (Scan): %s [%v] (%s) - %v", query, args, duration, err)
			return fmt.Errorf("sqlite Get scan error: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		duration := time.Since(start)
		log.Printf("DB Get Error (Rows Iteration): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Get rows error: %w", err)
	}

	// Check if any row was found
	if rowCount == 0 {
		duration := time.Since(start)
		log.Printf("DB Get (No Rows): %s [%v] (%s)", query, args, duration)
		return common.ErrNotFound // Use common.ErrNotFound
	}

	duration := time.Since(start)
	log.Printf("DB Get (Success): %s [%v] (%s)", query, args, duration)
	return nil
}

// Select executes a query and scans the results into a slice.
// Dynamically creates scan destinations based on query columns.
func (a *SQLiteAdapter) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	if a.isClosed() {
		return fmt.Errorf("adapter is closed")
	}

	log.Printf("DB Select (sql): %s [%v]", query, args)
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
		log.Printf("DB Select Query Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Select query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Printf("DB Select Error (Fetching Columns): %s [%v] - %v", query, args, err)
		return fmt.Errorf("sqlite Select failed fetching columns: %w", err)
	}

	isBasicTypeSlice := isBasicType(elemType)
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
			// Use the dynamic scanner preparation for structs
			scanDest, setupErr = prepareScanDest(newElemPtrVal.Elem(), cols) // Pass struct value
			if setupErr != nil {
				duration := time.Since(start)
				log.Printf("DB Select Error (Prepare Scan): %s [%v] (%s) - %v", query, args, duration, setupErr)
				return fmt.Errorf("sqlite Select row setup error: %w", setupErr)
			}
		}

		if err := rows.Scan(scanDest...); err != nil {
			duration := time.Since(start)
			log.Printf("DB Select Scan Error: %s [%v] (%s) - %v", query, args, duration, err)
			return fmt.Errorf("sqlite Select scan error: %w", err)
		}

		var valToAppend reflect.Value
		if isBasicTypeSlice {
			valToAppend = elemToScan.Elem()
		} else {
			if isPtrElem {
				valToAppend = elemToScan // Append the pointer (*User)
			} else {
				valToAppend = elemToScan.Elem() // Append the value (User)
			}
		}
		sliceVal.Set(reflect.Append(sliceVal, valToAppend))
		rowCount++
	}

	if err = rows.Err(); err != nil {
		duration := time.Since(start)
		log.Printf("DB Select Rows Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Select rows error: %w", err)
	}

	duration := time.Since(start)
	log.Printf("DB Select OK: %s [%v] (%d rows, %s)", query, args, rowCount, duration)
	return nil
}

// Exec executes a query that doesn't return rows (INSERT, UPDATE, DELETE).
// Uses standard sql.DB.ExecContext
func (a *SQLiteAdapter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if a.isClosed() {
		return nil, fmt.Errorf("adapter is closed")
	}
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
func (a *SQLiteAdapter) GetCount(ctx context.Context, tableName string, where string, args []interface{}) (int64, error) {
	if a.isClosed() {
		return 0, fmt.Errorf("adapter is closed")
	}
	query := a.builder.BuildCountSQL(tableName, where)
	row := a.db.QueryRowContext(ctx, query, args...)
	var count int64
	err := row.Scan(&count)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("sqlite GetCount scan error: %w", err)
	}
	return count, nil
}

// BeginTx starts a new database transaction.
func (a *SQLiteAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (thing.Tx, error) { // Return public interface type
	if a.isClosed() {
		return nil, fmt.Errorf("adapter is closed")
	}
	log.Println("DB Transaction Started")
	tx, err := a.db.BeginTx(ctx, opts) // Use standard BeginTx
	if err != nil {
		return nil, fmt.Errorf("failed to begin sqlite transaction: %w", err)
	}
	return &SQLiteTx{tx: tx}, nil
}

// Close closes the database connection pool.
func (a *SQLiteAdapter) Close() error {
	a.closeMx.Lock()
	defer a.closeMx.Unlock()
	if a.closed {
		return nil
	}
	log.Println("SQLite adapter closed.")
	a.closed = true
	return a.db.Close()
}

func (a *SQLiteAdapter) isClosed() bool {
	a.closeMx.Lock()
	defer a.closeMx.Unlock()
	return a.closed
}

// --- Transaction Wrapper ---

// SQLiteTx wraps a standard sql.Tx to implement the interfaces.Tx interface.
type SQLiteTx struct {
	tx *sql.Tx // Changed from *sqlx.Tx
}

// Get retrieves a single row within the transaction.
// Uses QueryContext and prepares scan destinations based on returned columns.
func (t *SQLiteTx) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	log.Printf("DB Tx Get (sql): %s [%v]", query, args)
	start := time.Now()

	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Tx Get Error (Query): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Tx Get query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Tx Get Error (Columns): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Tx Get failed fetching columns: %w", err)
	}

	// Prepare scan destinations based on destination struct type
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("tx get: destination must be a non-nil pointer to a struct, got %T", dest)
	}
	structVal := destVal.Elem() // The actual struct value

	scanDest, err := prepareScanDest(structVal, cols) // Pass struct value
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Tx Get Error (Prepare Scan): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Tx Get setup error: %w", err)
	}

	// Expect exactly one row
	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > 1 {
			duration := time.Since(start)
			log.Printf("DB Tx Get Error (Multiple Rows): %s [%v] (%s)", query, args, duration)
			return fmt.Errorf("sqlite Tx Get error: expected 1 row, got multiple")
		}
		err = rows.Scan(scanDest...)
		if err != nil {
			duration := time.Since(start)
			log.Printf("DB Tx Get Error (Scan): %s [%v] (%s) - %v", query, args, duration, err)
			return fmt.Errorf("sqlite Tx Get scan error: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		duration := time.Since(start)
		log.Printf("DB Tx Get Error (Rows Iteration): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Tx Get rows error: %w", err)
	}

	// Check if any row was found
	if rowCount == 0 {
		duration := time.Since(start)
		log.Printf("DB Tx Get (No Rows): %s [%v] (%s)", query, args, duration)
		return common.ErrNotFound // Use common.ErrNotFound
	}

	duration := time.Since(start)
	log.Printf("DB Tx Get (Success): %s [%v] (%s)", query, args, duration)
	return nil
}

// Select executes a query within the transaction and scans results into a slice.
// Dynamically creates scan destinations based on query columns.
func (t *SQLiteTx) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	log.Printf("DB Tx Select (sql): %s [%v]", query, args)
	start := time.Now()

	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("tx select: destination must be a pointer to a slice, got %T", dest)
	}
	sliceVal := destVal.Elem()
	elemType := sliceVal.Type().Elem()

	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Tx Select Query Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Tx Select query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Printf("DB Tx Select Error (Fetching Columns): %s [%v] - %v", query, args, err)
		return fmt.Errorf("sqlite Tx Select failed fetching columns: %w", err)
	}

	isBasicTypeSlice := isBasicType(elemType)
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
			// Use the dynamic scanner preparation for structs
			scanDest, setupErr = prepareScanDest(newElemPtrVal.Elem(), cols) // Pass struct value
			if setupErr != nil {
				duration := time.Since(start)
				log.Printf("DB Tx Select Error (Prepare Scan): %s [%v] (%s) - %v", query, args, duration, setupErr)
				return fmt.Errorf("sqlite Tx Select row setup error: %w", setupErr)
			}
		}

		if err := rows.Scan(scanDest...); err != nil {
			duration := time.Since(start)
			log.Printf("DB Tx Select Scan Error: %s [%v] (%s) - %v", query, args, duration, err)
			return fmt.Errorf("sqlite Tx Select scan error: %w", err)
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
		log.Printf("DB Tx Select Rows Error: %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("sqlite Tx Select rows error: %w", err)
	}

	duration := time.Since(start)
	log.Printf("DB Tx Select OK: %s [%v] (%d rows, %s)", query, args, rowCount, duration)
	return nil
}

// Exec executes a query within the transaction that doesn't return rows.
func (t *SQLiteTx) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
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
		return fmt.Errorf("sqlite commit error: %w", err)
	}
	log.Println("DB Transaction Committed")
	return nil
}

// Rollback rolls back the transaction.
func (t *SQLiteTx) Rollback() error {
	err := t.tx.Rollback()
	if err != nil {
		// It's conventional not to wrap sql.ErrTxDone
		if errors.Is(err, sql.ErrTxDone) {
			log.Printf("DB Transaction Rollback Warning: %v", err)
			return err // Return original error
		}
		log.Printf("DB Transaction Rollback Error: %v", err)
		return fmt.Errorf("sqlite rollback error: %w", err)
	}
	log.Println("DB Transaction Rolled Back")
	return nil
}

// --- Helper Functions ---

// prepareScanDest creates a slice of pointers suitable for sql.Rows.Scan,
// mapping columns returned by the query to fields in the target struct.
// structVal should be the reflect.Value of the struct itself (not a pointer to it).
// cols is the slice of column names returned by rows.Columns().
func prepareScanDest(structVal reflect.Value, cols []string) ([]interface{}, error) {
	if structVal.Kind() != reflect.Struct {
		return nil, fmt.Errorf("prepareScanDest: input must be a struct value, got %s", structVal.Kind())
	}

	fieldMap, err := getStructFieldMap(structVal) // Build map from db tag -> field value
	if err != nil {
		return nil, fmt.Errorf("prepareScanDest: failed to build field map: %w", err)
	}

	// Create scan destinations based on returned columns
	scanDest := make([]interface{}, len(cols))
	for i, colName := range cols {
		if fieldVal, ok := fieldMap[colName]; ok {
			// Ensure the field we found is addressable before getting its address
			if fieldVal.CanAddr() {
				scanDest[i] = fieldVal.Addr().Interface() // Get pointer to the field
			} else {
				// This case might happen if the field originates from an unaddressable embedded struct
				// within an addressable outer struct. Log and discard.
				log.Printf("Warning: Field found for column '%s' but it is not addressable in struct type %s, discarding.", colName, structVal.Type().Name())
				scanDest[i] = new(sql.RawBytes)
			}
		} else {
			// Column doesn't map to a field, use RawBytes to discard
			scanDest[i] = new(sql.RawBytes)
			log.Printf("Warning: Column '%s' not found in destination struct type %s, discarding.", colName, structVal.Type().Name())
		}
	}

	return scanDest, nil
}

// getStructFieldMap is a helper for prepareScanDest to recursively build the field map, including embedded structs.
// It maps the database column name (from db tag or field name) to the reflect.Value of the field.
func getStructFieldMap(structVal reflect.Value) (map[string]reflect.Value, error) {
	if structVal.Kind() != reflect.Struct {
		return nil, fmt.Errorf("getStructFieldMap: input must be a struct value, got %s", structVal.Kind())
	}

	typ := structVal.Type()
	fieldMap := make(map[string]reflect.Value)
	// log.Printf("DEBUG: getStructFieldMap processing type: %s", typ.Name()) // Added log

	for i := 0; i < structVal.NumField(); i++ {
		field := structVal.Field(i)
		structField := typ.Field(i)

		if !structField.IsExported() {
			continue
		}

		dbTag := structField.Tag.Get("db")
		if dbTag == "-" {
			continue
		}

		// If it's an anonymous embedded struct, recurse and merge its fields first.
		if structField.Anonymous && field.Kind() == reflect.Struct {
			// Ensure we can work with the struct's fields
			if !field.CanAddr() {
				// This might happen with unexported anonymous fields, although we filter exported above.
				// Or non-addressable ones for other reasons. Log and skip recursion.
				log.Printf("Skipping recursion into non-addressable anonymous struct field %s", structField.Name)
				continue
			}

			// log.Printf("DEBUG: getStructFieldMap recursing into anonymous field: %s", structField.Name) // Added log
			embeddedMap, err := getStructFieldMap(field) // Recursive call
			if err != nil {
				return nil, fmt.Errorf("error processing embedded struct field %s: %w", structField.Name, err)
			}
			// Merge embedded fields. Outer fields processed later will overwrite if names clash.
			// log.Printf("DEBUG: getStructFieldMap merging %d fields from embedded %s", len(embeddedMap), structField.Name) // Added log
			for k, v := range embeddedMap {
				fieldMap[k] = v
			}
			// Continue to the next field after processing the embedded struct's fields.
			continue // Important: skip the dbTag handling below for the anonymous struct itself.
		}

		// Handle regular (non-anonymous or non-struct) fields with a db tag, or use field name.
		// Note: This block now also handles named embedded structs if they have a db tag.
		fieldName := structField.Name
		mapKey := dbTag
		if mapKey != "" {
			// Use only the part before the first comma as the column name
			parts := strings.Split(mapKey, ",")
			mapKey = parts[0]
		}

		if mapKey == "" {
			mapKey = strings.ToLower(fieldName) // Default to lower-case field name if no db tag or after splitting
		}

		// Add/overwrite the field in the map. Outer fields overwrite embedded fields naturally
		fieldMap[mapKey] = field
		// log.Printf("DEBUG: getStructFieldMap added/updated mapKey: '%s' for field: %s", mapKey, fieldName) // Added log
	}
	// log.Printf("DEBUG: getStructFieldMap finished processing type: %s, map size: %d", typ.Name(), len(fieldMap)) // Added log
	return fieldMap, nil
}

// isBasicType checks if a reflect.Type represents a basic Go type suitable for direct scanning.
func isBasicType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String,
		reflect.Bool:
		return true
	case reflect.Invalid, reflect.Uintptr, reflect.Complex64, reflect.Complex128, reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.Struct, reflect.UnsafePointer:
		return false
	default:
		return false
	}
}

// DB returns the underlying *sql.DB for advanced use cases.
func (a *SQLiteAdapter) DB() *sql.DB {
	return a.db
}

func (a *SQLiteAdapter) Builder() thing.SQLBuilder {
	return a.builder
}

func (a *SQLiteAdapter) DialectName() string {
	return "sqlite"
}

// Register the SQLite introspector factory at init time to avoid import cycles.
func init() {
	// Use a type assertion to get the underlying *sql.DB from the adapter
	// and return a new SQLiteIntrospector
	importThingRegister := func() {
		// Import only for registration
		importThing := struct{}{}
		_ = importThing
	}
	_ = importThingRegister // avoid unused warning
	thing.RegisterIntrospectorFactory("sqlite", func(adapter thing.DBAdapter) schema.Introspector {
		db := adapter.DB()
		return &SQLiteIntrospector{DB: db}
	})
}
