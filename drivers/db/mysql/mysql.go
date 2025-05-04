package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/burugo/thing"
	"github.com/burugo/thing/common"

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
var _ thing.DBAdapter = (*MySQLAdapter)(nil)
var _ thing.Tx = (*MySQLTx)(nil)

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
		log.Printf("DB Get Error (Query - MySQL): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("mysql Get query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Get Error (Columns - MySQL): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("mysql Get failed fetching columns: %w", err)
	}

	// Prepare scan destinations based on destination struct type
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("get: destination must be a non-nil pointer to a struct, got %T", dest)
	}
	structVal := destVal.Elem() // The actual struct value

	scanDest, err := prepareScanDest(structVal, cols) // Reusable helper
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Get Error (Prepare Scan - MySQL): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("mysql Get setup error: %w", err)
	}

	// Expect exactly one row
	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > 1 {
			duration := time.Since(start)
			log.Printf("DB Get Error (Multiple Rows - MySQL): %s [%v] (%s)", query, args, duration)
			return fmt.Errorf("mysql Get error: expected 1 row, got multiple")
		}
		err = rows.Scan(scanDest...)
		if err != nil {
			duration := time.Since(start)
			log.Printf("DB Get Error (Scan - MySQL): %s [%v] (%s) - %v", query, args, duration, err)
			return fmt.Errorf("mysql Get scan error: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		duration := time.Since(start)
		log.Printf("DB Get Error (Rows Iteration - MySQL): %s [%v] (%s) - %v", query, args, duration, err)
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
	// TODO: Implement using db.QueryContext and loop through rows.Scan
	// TODO: Handle slice of structs, slice of pointers, slice of basic types
	// IMPORTANT: Use '?' placeholders
	// return fmt.Errorf("MySQLAdapter.Select not implemented") // Remove placeholder

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
		log.Printf("DB Select Query Error (MySQL): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("mysql Select query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Printf("DB Select Error (Fetching Columns - MySQL): %s [%v] - %v", query, args, err)
		return fmt.Errorf("mysql Select failed fetching columns: %w", err)
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
				log.Printf("DB Select Error (Prepare Scan - MySQL): %s [%v] (%s) - %v", query, args, duration, setupErr)
				return fmt.Errorf("mysql Select row setup error: %w", setupErr)
			}
		}

		if err := rows.Scan(scanDest...); err != nil {
			duration := time.Since(start)
			log.Printf("DB Select Scan Error (MySQL): %s [%v] (%s) - %v", query, args, duration, err)
			return fmt.Errorf("mysql Select scan error: %w", err)
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
		log.Printf("DB Select Rows Error (MySQL): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("mysql Select rows error: %w", err)
	}

	duration := time.Since(start)
	log.Printf("DB Select OK (MySQL): %s [%v] (%d rows, %s)", query, args, rowCount, duration)
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
		log.Printf("DB Exec Error (MySQL): %s [%v] (%s) - %v", query, args, duration, err)
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
		log.Printf("DB BeginTx Error (MySQL): %v", err)
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
		log.Printf("DB Tx Commit Error (MySQL): %v", err)
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
		log.Printf("DB Tx Rollback Error (MySQL): %v", err)
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
		log.Printf("DB Tx Get Error (Query - MySQL): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("mysql Tx Get query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Tx Get Error (Columns - MySQL): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("mysql Tx Get failed fetching columns: %w", err)
	}

	// Prepare scan destinations
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("tx get: destination must be a non-nil pointer to a struct, got %T", dest)
	}
	structVal := destVal.Elem()
	scanDest, err := prepareScanDest(structVal, cols)
	if err != nil {
		duration := time.Since(start)
		log.Printf("DB Tx Get Error (Prepare Scan - MySQL): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("mysql Tx Get setup error: %w", err)
	}

	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > 1 {
			duration := time.Since(start)
			log.Printf("DB Tx Get Error (Multiple Rows - MySQL): %s [%v] (%s)", query, args, duration)
			return fmt.Errorf("mysql Tx Get error: expected 1 row, got multiple")
		}
		err = rows.Scan(scanDest...)
		if err != nil {
			duration := time.Since(start)
			log.Printf("DB Tx Get Error (Scan - MySQL): %s [%v] (%s) - %v", query, args, duration, err)
			return fmt.Errorf("mysql Tx Get scan error: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		duration := time.Since(start)
		log.Printf("DB Tx Get Error (Rows Iteration - MySQL): %s [%v] (%s) - %v", query, args, duration, err)
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
		log.Printf("DB Tx Select Query Error (MySQL): %s [%v] (%s) - %v", query, args, duration, err)
		return fmt.Errorf("mysql Tx Select query error: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		log.Printf("DB Tx Select Error (Fetching Columns - MySQL): %s [%v] - %v", query, args, err)
		return fmt.Errorf("mysql Tx Select failed fetching columns: %w", err)
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
			scanDest, setupErr = prepareScanDest(newElemPtrVal.Elem(), cols)
			if setupErr != nil {
				duration := time.Since(start)
				log.Printf("DB Tx Select Error (Prepare Scan - MySQL): %s [%v] (%s) - %v", query, args, duration, setupErr)
				return fmt.Errorf("mysql Tx Select row setup error: %w", setupErr)
			}
		}

		if err := rows.Scan(scanDest...); err != nil {
			duration := time.Since(start)
			log.Printf("DB Tx Select Scan Error (MySQL): %s [%v] (%s) - %v", query, args, duration, err)
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
		log.Printf("DB Tx Select Rows Error (MySQL): %s [%v] (%s) - %v", query, args, duration, err)
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
		log.Printf("DB Tx Exec Error (MySQL): %s [%v] (%s) - %v", query, args, duration, err)
		return nil, fmt.Errorf("mysql Tx ExecContext error: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()
	log.Printf("DB Tx Exec (MySQL): %s [%v] (Affected: %d, LastInsertID: %d) (%s)", query, args, rowsAffected, lastInsertID, duration)
	return result, nil
}

// --- Helper Functions (Copied from SQLite adapter - might need consolidation) ---

// prepareScanDest creates a slice of pointers for scanning based on struct fields and column names.
func prepareScanDest(structVal reflect.Value, cols []string) ([]interface{}, error) {
	if structVal.Kind() != reflect.Struct {
		return nil, fmt.Errorf("prepareScanDest: input must be a struct value, got %s", structVal.Kind())
	}

	fieldMap, err := getStructFieldMap(structVal)
	if err != nil {
		return nil, fmt.Errorf("prepareScanDest: failed to get struct field map: %w", err)
	}

	dest := make([]interface{}, len(cols))
	for i, colName := range cols {
		if field, ok := fieldMap[colName]; ok {
			if !field.CanAddr() {
				return nil, fmt.Errorf("prepareScanDest: cannot take address of field for column %s", colName)
			}
			dest[i] = field.Addr().Interface() // Get pointer to the field
		} else {
			// If a column is returned that doesn't map to a field, scan into sql.RawBytes
			// log.Printf("Warning: Column '%s' not found in destination struct fields, scanning into RawBytes.", colName)
			dest[i] = new(sql.RawBytes)
		}
	}
	return dest, nil
}

// getStructFieldMap is a helper for prepareScanDest to recursively build the field map, including embedded structs.
// It maps the database column name (from db tag or field name) to the reflect.Value of the field.
// Prioritizes outer fields over embedded fields if names clash.
func getStructFieldMap(structVal reflect.Value) (map[string]reflect.Value, error) {
	if structVal.Kind() != reflect.Struct {
		return nil, fmt.Errorf("getStructFieldMap: input must be a struct value, got %s", structVal.Kind())
	}

	typ := structVal.Type()
	fieldMap := make(map[string]reflect.Value)
	// log.Printf("DEBUG: getStructFieldMap processing type: %s", typ.Name()) // Added log

	// First pass: handle embedded structs recursively
	for i := 0; i < structVal.NumField(); i++ {
		field := structVal.Field(i)
		structField := typ.Field(i)

		if structField.Anonymous && field.Kind() == reflect.Struct && field.Type() != reflect.TypeOf(time.Time{}) {
			// log.Printf("DEBUG: getStructFieldMap recursing into anonymous field: %s", structField.Name) // Added log
			embeddedMap, err := getStructFieldMap(field) // Recursive call
			if err != nil {
				return nil, fmt.Errorf("error processing embedded struct field %s: %w", structField.Name, err)
			}
			// Merge embedded fields. Outer fields will overwrite later.
			for k, v := range embeddedMap {
				fieldMap[k] = v
			}
			// log.Printf("DEBUG: getStructFieldMap merged %d fields from embedded %s", len(embeddedMap), structField.Name) // Added log
		}
	}

	// Second pass: handle direct fields (overwrites embedded fields if names clash)
	for i := 0; i < structVal.NumField(); i++ {
		field := structVal.Field(i)
		structField := typ.Field(i)

		if !structField.IsExported() {
			continue
		}

		// Skip anonymous structs processed in the first pass
		if structField.Anonymous && field.Kind() == reflect.Struct && field.Type() != reflect.TypeOf(time.Time{}) {
			continue
		}

		dbTag := structField.Tag.Get("db")
		if dbTag == "-" {
			continue // Skip fields explicitly ignored
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
		if field.IsValid() && field.CanAddr() {
			fieldMap[mapKey] = field
			// log.Printf("DEBUG: getStructFieldMap added/updated mapKey: '%s' for field: %s", mapKey, fieldName) // Added log
		} else {
			// Log or return an error if the field cannot be addressed
			log.Printf("WARN: Field %s (mapKey %s) is not addressable or invalid.", fieldName, mapKey)
			// return nil, fmt.Errorf("field %s is not addressable", fieldName)
		}
	}

	// log.Printf("DEBUG: getStructFieldMap finished processing type: %s, map size: %d", typ.Name(), len(fieldMap)) // Added log
	return fieldMap, nil
}

// isBasicType checks if a reflect.Type represents a basic Go type suitable for direct scanning.
func isBasicType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true
	case reflect.Slice:
		// Handle []byte specifically
		return t.Elem().Kind() == reflect.Uint8
	case reflect.Struct:
		// Handle time.Time
		return t == reflect.TypeOf(time.Time{})
	case reflect.Ptr:
		// Handle pointers to basic types or time.Time
		return isBasicType(t.Elem())
	default:
		return false
	}
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
