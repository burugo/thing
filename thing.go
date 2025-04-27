package thing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log" // For placeholder logging
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"crypto/sha256"
	"encoding/hex"
	// We'll need a Redis client library. Using go-redis as an example.
	// "github.com/redis/go-redis/v9"
)

// DefaultThing holds the globally configured Thing instance.
// It must be initialized, typically in main or init, after calling New.
// var DefaultThing *Thing // Commented out: Global DefaultThing is problematic with generic Thing[T]. Use New[T] or Use[T] instead.

// --- Internal Global Configuration (NEW) ---
var (
	globalDB     DBAdapter
	globalCache  CacheClient
	isConfigured bool
	configMutex  sync.RWMutex
)

// Configure sets up the package-level database and cache clients.
// This MUST be called once during application initialization before using Use[T].
func Configure(db DBAdapter, cache CacheClient) error {
	configMutex.Lock()
	defer configMutex.Unlock()
	if !isConfigured { // Prevent re-configuration? Or allow? For now, allow.
		if db == nil {
			return errors.New("DBAdapter must be non-nil")
		}
		if cache == nil {
			return errors.New("CacheClient must be non-nil")
		}
		globalDB = db
		globalCache = cache
		isConfigured = true
		log.Println("Thing package configured globally.")
	} else {
		log.Println("Thing package already configured.")
	}
	return nil
}

// --- Model Metadata Cache ---

// modelInfo holds cached reflection results for a specific Model type.
type modelInfo struct {
	tableName string
	pkName    string   // Database name of the primary key field
	columns   []string // List of all database column names (including PK)
	fields    []string // Corresponding Go struct field names (order matches columns)
	// Map Go field name to its corresponding DB column name
	fieldToColumnMap map[string]string
	// Map DB column name to its corresponding Go field name
	columnToFieldMap map[string]string
}

// modelCache stores modelInfo structs, keyed by reflect.Type.
// Uses sync.Map for thread-safe concurrent access.
var modelCache sync.Map // map[reflect.Type]*modelInfo

// getCachedModelInfo retrieves or computes/caches metadata for a given Model type.
// It focuses on column mapping and PK detection from struct tags.
func getCachedModelInfo(modelType reflect.Type) (*modelInfo, error) {
	// Ensure we are working with the underlying struct type, not a pointer
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct type, got %s", modelType.Kind())
	}

	// Check cache first
	if cached, ok := modelCache.Load(modelType); ok {
		// Type assert to the pointer type we stored
		return cached.(*modelInfo), nil
	}

	// --- Not in cache, perform reflection ---
	info := modelInfo{
		fieldToColumnMap: make(map[string]string),
		columnToFieldMap: make(map[string]string),
		// Table name determined later by calling model.TableName()
	}
	pkDbName := ""

	numFields := modelType.NumField()
	info.columns = make([]string, 0, numFields)
	info.fields = make([]string, 0, numFields)

	var processFields func(structType reflect.Type)
	processFields = func(structType reflect.Type) {
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)
			dbTag := field.Tag.Get("db")

			// Handle embedded structs recursively
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				// Check if it's the BaseModel we specifically handle
				if field.Type == reflect.TypeOf(BaseModel{}) {
					// Directly process BaseModel fields
					baseModelType := field.Type
					for j := 0; j < baseModelType.NumField(); j++ {
						baseField := baseModelType.Field(j)
						baseDbTag := baseField.Tag.Get("db")
						if baseDbTag == "-" || !baseField.IsExported() {
							continue
						}
						colName := baseDbTag
						if colName == "" {
							colName = strings.ToLower(baseField.Name) // Fallback
						}
						info.columns = append(info.columns, colName)
						info.fields = append(info.fields, baseField.Name)
						info.fieldToColumnMap[baseField.Name] = colName
						info.columnToFieldMap[colName] = baseField.Name
						// Assume 'id' tag on BaseModel marks the PK
						if colName == "id" { // TODO: Make PK configurable?
							pkDbName = colName
						}
					}
				} else {
					// Recursively process other embedded structs
					processFields(field.Type)
				}
				continue
			}

			// --- Revised Logic for Regular Fields ---
			// Skip explicitly ignored, unexported, OR fields WITHOUT a db tag.
			if dbTag == "-" || dbTag == "" || !field.IsExported() {
				continue
			}

			// Field has a valid db tag and is exported.
			colName := dbTag // Use the tag value directly

			// Check if this regular field is the PK (if not already found in BaseModel)
			if colName == "id" && pkDbName == "" {
				pkDbName = colName
			}
			info.columns = append(info.columns, colName)
			info.fields = append(info.fields, field.Name) // Store Go field name
			info.fieldToColumnMap[field.Name] = colName
			info.columnToFieldMap[colName] = field.Name
			// --- End Revised Logic ---
		}
	}
	processFields(modelType)

	if pkDbName == "" {
		// Adjust error message as table name is not inferred here anymore
		return nil, fmt.Errorf("primary key column (assumed 'id') not found via 'db:\\\"id\\\"' tag in struct %s or its embedded BaseModel", modelType.Name())
	}
	info.pkName = pkDbName

	// Table name determination removed from here.

	// --- Store in cache ---
	// Store a pointer to the info struct
	actualInfo, _ := modelCache.LoadOrStore(modelType, &info)
	// Type assert to the pointer type
	return actualInfo.(*modelInfo), nil
}

// --- Placeholder Types/Interfaces ---

// Placeholder for a Redis client. Replace with your actual client library (e.g., redis.Client).
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Del(ctx context.Context, keys ...string) error
	// Placeholder for locking
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error)
}

// Placeholder for a database connection/transaction. Replace with *sql.DB, *sql.Tx, or your ORM's equivalent.
// Updated to include SelectContext (commonly used with sqlx)
type DBExecutor interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	// SelectContext executes a query and scans the results into a slice.
	SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	// Add NamedExecContext / NamedQueryContext if using named parameters with sqlx
	// NamedExecContext(ctx context.Context, query string, arg interface{}) (sql.Result, error)
}

// Define common errors
var (
	ErrNotFound        = errors.New("record not found")
	ErrLockNotAcquired = errors.New("could not acquire lock")
	// Use redis.Nil for Redis-specific not found if using go-redis
	RedisNilError = errors.New("redis: nil") // Placeholder if not using go-redis
)

const (
	// Represents a non-existent entry in the cache, similar to PHP's NoneResult
	NoneResult = "NoneResult"
	// Cache duration for non-existent entries to prevent cache penetration
	NoneResultCacheDuration = 1 * time.Minute
	// Default cache duration for existing entries (adjust as needed)
	DefaultCacheDuration = 1 * time.Hour
	// Lock duration
	LockDuration   = 5 * time.Second
	LockRetryDelay = 50 * time.Millisecond
	LockMaxRetries = 5
)

// --- Event System ---

type EventType string

const (
	// Lifecycle Events
	EventTypeBeforeSave   EventType = "BeforeSave"
	EventTypeAfterSave    EventType = "AfterSave"
	EventTypeBeforeCreate EventType = "BeforeCreate"
	EventTypeAfterCreate  EventType = "AfterCreate"
	EventTypeBeforeDelete EventType = "BeforeDelete"
	EventTypeAfterDelete  EventType = "AfterDelete"
)

// EventListener defines the signature for functions that can listen to events.
// It receives the context, the event type, the model instance, and optional data.
// For Save events, eventData will be map[string]interface{} representing changedFields.
type EventListener func(ctx context.Context, eventType EventType, model interface{}, eventData interface{}) error

// listenerRegistry holds the registered listeners for each event type.
var (
	listenerRegistry = make(map[EventType][]EventListener)
	listenerMutex    sync.RWMutex // To protect concurrent access to the registry
)

// RegisterListener adds a listener function for a specific event type.
func RegisterListener(eventType EventType, listener EventListener) {
	listenerMutex.Lock()
	defer listenerMutex.Unlock()
	listenerRegistry[eventType] = append(listenerRegistry[eventType], listener)
}

// triggerEvent executes all registered listeners for a given event type.
// Model is passed as interface{}.
func triggerEvent(ctx context.Context, eventType EventType, model interface{}, eventData interface{}) error {
	listenerMutex.RLock()
	listeners, ok := listenerRegistry[eventType]
	listenerMutex.RUnlock()

	if !ok || len(listeners) == 0 {
		return nil
	}

	// Get model ID for logging, requires type assertion or reflection
	var modelID int64 = -1             // Default invalid ID
	modelVal := reflect.ValueOf(model) // model is interface{}
	if modelVal.Kind() == reflect.Ptr {
		modelVal = modelVal.Elem()
	}
	if modelVal.Kind() == reflect.Struct {
		idField := modelVal.FieldByName("ID") // Convention
		if idField.IsValid() && idField.Kind() == reflect.Int64 {
			modelID = idField.Int()
		}
	}

	// Execute listeners sequentially.
	for _, listener := range listeners {
		err := listener(ctx, eventType, model, eventData)
		if err != nil {
			log.Printf("Error executing listener for event %s on model %T(%d): %v", eventType, model, modelID, err)
			return fmt.Errorf("event listener for %s failed: %w", eventType, err)
		}
	}
	return nil
}

// --- Thing Core ---

// Thing is the central access point for ORM operations, analogous to gorm.DB.
// It holds database/cache clients and the context for operations.
type Thing[T any] struct {
	db    DBAdapter
	cache CacheClient
	ctx   context.Context
	info  *modelInfo // Pre-computed metadata for type T
}

// New creates a new Thing instance with default context.Background().
// Made generic: requires type parameter T when called, e.g., New[MyModel](...).
func New[T any](db DBAdapter, cache CacheClient) (*Thing[T], error) {
	if db == nil {
		return nil, errors.New("DBAdapter must be non-nil")
	}
	if cache == nil {
		return nil, errors.New("CacheClient must be non-nil")
	}
	log.Println("New Thing instance created.")

	// Pre-compute model info for T
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	info, err := getCachedModelInfo(modelType)
	if err != nil {
		return nil, fmt.Errorf("failed to get model info for type %s: %w", modelType.Name(), err)
	}
	// Attempt to determine table name here. Instance method might override later.
	info.tableName = getTableNameFromType(modelType)
	if info.tableName == "" {
		log.Printf("Warning: Could not determine table name for type %s during New. Relying on instance method.", modelType.Name())
		// Proceed anyway, assume it will be set later if needed or method exists
	}

	return &Thing[T]{
		db:    db,
		cache: cache,
		ctx:   context.Background(), // Default context
		info:  info,                 // Store pre-computed info
	}, nil
}

// WithContext returns a shallow copy of Thing with the context replaced.
// This is used to set the context for a specific chain of operations.
func (t *Thing[T]) WithContext(ctx context.Context) *Thing[T] { // Returns *Thing
	if ctx == nil {
		log.Println("Warning: nil context passed to WithContext, using context.Background()")
		ctx = context.Background()
	}
	// Create a shallow copy and replace the context
	newThing := *t     // Copy struct values (dbAdapter, cacheClient, old ctx)
	newThing.ctx = ctx // Set the new context
	return &newThing   // Return pointer to the copy
}

// --- Public Methods on *Thing[T] ---

// ByID retrieves a single record of type T.
func (t *Thing[T]) ByID(id int64) (*T, error) {
	instance := new(T)
	err := t.byIDInternal(t.ctx, id, instance)
	if err != nil {
		return nil, err
	}
	return instance, nil
}

// Save updates an existing record or creates a new one if its primary key is zero
// or it's explicitly marked as new. Also updates timestamps.
// Returns the saved *T (with ID/timestamps populated/updated) and nil error on success.
// Returns nil and error on failure.
func (t *Thing[T]) Save(value *T) error {
	// Delegate directly to the internal save logic
	return t.saveInternal(t.ctx, value)
}

// Delete removes a record of type T.
func (t *Thing[T]) Delete(value *T) error {
	return t.deleteInternal(t.ctx, value)
}

// Query executes a query and returns results of type []*T.
func (t *Thing[T]) Query(params QueryParams) ([]*T, error) {
	return t.queryInternal(t.ctx, params)
}

// IDs executes a query and returns only IDs for type T.
// Removed modelType parameter, uses t.info instead.
func (t *Thing[T]) IDs(params QueryParams) ([]int64, error) {
	if t.info == nil { // Add check for safety
		return nil, errors.New("IDs: model info not available on Thing instance")
	}
	// Pass the pre-computed info from the Thing instance
	return t.idsInternal(t.ctx, t.info, params)
}

// --- BaseModel Struct ---

// BaseModel provides common fields and functionality for database models.
// It should be embedded into specific model structs.
type BaseModel struct {
	ID        int64     `json:"id" db:"id"`                 // Primary key
	CreatedAt time.Time `json:"created_at" db:"created_at"` // Timestamp for creation
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"` // Timestamp for last update

	// --- Internal ORM state ---
	// These fields should be populated by the ORM functions (ByID, Create, etc.).
	// They are NOT saved to the DB or cache.
	isNewRecord bool `json:"-" db:"-"` // Flag to indicate if the record is new
}

// GetID returns the primary key value.
func (b *BaseModel) GetID() int64 {
	return b.ID
}

// SetID sets the primary key value.
func (b *BaseModel) SetID(id int64) {
	b.ID = id
}

// TableName returns the database table name for the model.
// Default implementation returns empty string, relying on getTableNameFromType.
// Override this method in your specific model struct for custom table names.
func (b *BaseModel) TableName() string {
	// Default implementation, getTableNameFromType will be used if this returns ""
	return ""
}

// IsNewRecord returns whether this is a new record
func (b *BaseModel) IsNewRecord() bool {
	return b.isNewRecord
}

// SetNewRecordFlag sets the internal isNewRecord flag (exported for internal use)
func (b *BaseModel) SetNewRecordFlag(isNew bool) {
	b.isNewRecord = isNew
}

// --- SQL Builder Helpers ---

// buildSelectSQL constructs a SELECT statement for fetching columns for a specific model.
// It no longer includes the WHERE clause.
func buildSelectSQL(info *modelInfo) string {
	if info.tableName == "" || len(info.columns) == 0 {
		log.Printf("Error: buildSelectSQL called with incomplete modelInfo: %+v", info)
		return ""
	}
	// Quote column names for safety/compatibility
	quotedColumns := make([]string, len(info.columns))
	for i, col := range info.columns {
		quotedColumns[i] = fmt.Sprintf("\"%s\"", col)
	}

	return fmt.Sprintf("SELECT %s FROM %s",
		strings.Join(quotedColumns, ", "),
		info.tableName,
	)
}

// getValuesForColumns extracts field values corresponding to a specific list of columns.
func getValuesForColumns(modelPtr interface{}, info *modelInfo, columns []string) ([]interface{}, error) {
	v := reflect.ValueOf(modelPtr)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct pointer, got %T", modelPtr)
	}

	values := make([]interface{}, 0, len(columns))
	for _, colName := range columns {
		fieldName, ok := info.columnToFieldMap[colName]
		if !ok {
			// This should ideally not happen if info is correct
			return nil, fmt.Errorf("column '%s' not found in cached metadata for type %s", colName, v.Type().Name())
		}
		fieldVal := v.FieldByName(fieldName)
		if !fieldVal.IsValid() {
			// Check embedded BaseModel as well? FieldByName should handle it.
			return nil, fmt.Errorf("field '%s' (for column '%s') not found in struct %s", fieldName, colName, v.Type().Name())
		}
		// Allow getting value even if unexported (e.g., from embedded BaseModel)? No, stick to exported.
		if !fieldVal.CanInterface() {
			// This usually means the field is unexported
			return nil, fmt.Errorf("cannot interface field '%s' (for column '%s') in struct %s", fieldName, colName, v.Type().Name())
		}
		values = append(values, fieldVal.Interface())
	}
	return values, nil
}

// buildInsertSQL constructs an INSERT statement.
// Returns the SQL string and the ordered list of columns used (excluding PK).
func buildInsertSQL(info *modelInfo) (string, []string) {
	insertCols := make([]string, 0, len(info.columns))
	placeholders := make([]string, 0, len(info.columns))
	for _, col := range info.columns {
		// Exclude primary key only if it's typically auto-generated (e.g., 'id')
		// Allow explicit PK insertion if needed? For now, assume PK is auto.
		if col != info.pkName {
			insertCols = append(insertCols, col)
			placeholders = append(placeholders, "?")
		}
	}

	if info.tableName == "" || len(insertCols) == 0 {
		log.Printf("Error: buildInsertSQL called with incomplete modelInfo or no insertable columns: %+v", info)
		return "", nil
	}

	sqlStmt := fmt.Sprintf("INSERT INTO %s (\"%s\") VALUES (%s)", // Quote column names
		info.tableName,
		strings.Join(insertCols, "\", \""),
		strings.Join(placeholders, ", "),
	)
	return sqlStmt, insertCols
}

// buildUpdateSQL constructs an UPDATE statement based on changed fields.
func buildUpdateSQL(info *modelInfo, changedFields map[string]interface{}) (string, []interface{}, error) {
	if len(changedFields) == 0 {
		return "", nil, errors.New("no fields provided for update")
	}

	setClauses := make([]string, 0, len(changedFields))
	values := make([]interface{}, 0, len(changedFields))

	// Iterate over info.columns to potentially maintain order (good for testing)
	for _, colName := range info.columns {
		if value, ok := changedFields[colName]; ok {
			if colName == info.pkName {
				continue // Cannot update primary key
			}
			setClauses = append(setClauses, fmt.Sprintf("\"%s\" = ?", colName)) // Quote column name
			values = append(values, value)
		}
	}

	if len(setClauses) == 0 {
		// This might happen if only the PK was in changedFields
		return "", nil, errors.New("no updatable fields provided for update (only PK change attempted?)")
	}

	if info.tableName == "" || info.pkName == "" {
		log.Printf("Error: buildUpdateSQL called with incomplete modelInfo: %+v", info)
		return "", nil, errors.New("cannot build update SQL with missing table or PK name")
	}

	sqlStmt := fmt.Sprintf("UPDATE %s SET %s WHERE \"%s\" = ?", // Quote PK name in WHERE
		info.tableName,
		strings.Join(setClauses, ", "),
		info.pkName,
	)

	return sqlStmt, values, nil
}

// buildDeleteSQL constructs a DELETE statement.
func buildDeleteSQL(info *modelInfo) string {
	if info.tableName == "" || info.pkName == "" {
		log.Printf("Error: buildDeleteSQL called with incomplete modelInfo: %+v", info)
		return ""
	}
	// Quote PK name
	return fmt.Sprintf("DELETE FROM %s WHERE \"%s\" = ?", info.tableName, info.pkName)
}

// buildSelectIDsSQL constructs a SELECT statement to fetch only primary key IDs.
func buildSelectIDsSQL(info *modelInfo, params QueryParams) (string, []interface{}) {
	var query strings.Builder
	args := []interface{}{}

	if info.tableName == "" || info.pkName == "" {
		log.Printf("Error: buildSelectIDsSQL called with incomplete modelInfo: %+v", info)
		return "", nil
	}

	// Quote PK and table name
	query.WriteString(fmt.Sprintf("SELECT \"%s\" FROM %s", info.pkName, info.tableName))

	if params.Where != "" {
		query.WriteString(" WHERE ")
		// IMPORTANT: Assumes params.Where uses correct quoting or placeholders
		query.WriteString(params.Where)
		args = append(args, params.Args...)
	}

	if params.Order != "" {
		query.WriteString(" ORDER BY ")
		// IMPORTANT: Assumes params.Order is safe and uses correct quoting
		query.WriteString(params.Order)
	}

	// Add LIMIT/OFFSET (Syntax might vary slightly between DBs, '?' placeholder is common)
	if params.Limit > 0 {
		query.WriteString(" LIMIT ?")
		args = append(args, params.Limit)
	}
	if params.Start > 0 { // OFFSET usually comes after LIMIT if both are present
		query.WriteString(" OFFSET ?")
		args = append(args, params.Start)
	}

	return query.String(), args
}

// generateQueryCacheKey generates a cache key for a query based on table name and params.
func generateQueryCacheKey(tableName string, params QueryParams) (string, error) {
	// Ensure params are marshalable and deterministic
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query params for cache key: %w", err)
	}

	hasher := sha256.New()
	hasher.Write([]byte(tableName)) // Include table name
	hasher.Write(paramsBytes)       // Include marshaled params
	hash := hex.EncodeToString(hasher.Sum(nil))

	// Consider a prefix, e.g., "query:"
	return fmt.Sprintf("query:%s:%s", tableName, hash), nil
}

// --- Reflection Helpers ---

// getTableName determines the table name for a given model instance (pointer to struct).
// Checks for TableName() method first, then falls back to convention.
func getTableName(value interface{}) string {
	val := reflect.ValueOf(value)
	// Check if the pointer type itself has TableName method
	methPtr := val.MethodByName("TableName")
	if methPtr.IsValid() && methPtr.Type().NumIn() == 0 && methPtr.Type().NumOut() == 1 && methPtr.Type().Out(0) == reflect.TypeOf("") {
		results := methPtr.Call(nil)
		if len(results) > 0 {
			if name := results[0].String(); name != "" {
				return name
			}
		}
	}

	// If pointer doesn't have it, check the underlying struct type
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() == reflect.Struct {
		// Check if the struct type (non-pointer receiver) has TableName method
		methVal := val.Addr().MethodByName("TableName") // Need Addr() to call pointer receiver methods if defined
		if !methVal.IsValid() {
			// Fallback: check for value receiver method if pointer receiver doesn't exist
			methVal = val.MethodByName("TableName")
		}

		if methVal.IsValid() && methVal.Type().NumIn() == 0 && methVal.Type().NumOut() == 1 && methVal.Type().Out(0) == reflect.TypeOf("") {
			results := methVal.Call(nil)
			if len(results) > 0 {
				if name := results[0].String(); name != "" {
					return name
				}
			}
		}
		// Fallback to convention: lowercase struct name
		return strings.ToLower(val.Type().Name()) + "s" // Pluralize convention
	}
	return "" // Could not determine
}

// getTableNameFromType determines table name from reflect.Type.
func getTableNameFromType(modelType reflect.Type) string {
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return ""
	}

	// Instantiate a zero value to check for TableName method
	instPtr := reflect.New(modelType) // Get pointer to check pointer receivers
	meth := instPtr.MethodByName("TableName")
	if meth.IsValid() && meth.Type().NumIn() == 0 && meth.Type().NumOut() == 1 && meth.Type().Out(0) == reflect.TypeOf("") {
		results := meth.Call(nil)
		if len(results) > 0 {
			if name := results[0].String(); name != "" {
				return name
			}
		}
	}
	// Check value receiver if pointer receiver method doesn't exist or returned ""
	instVal := instPtr.Elem()
	methVal := instVal.MethodByName("TableName")
	if methVal.IsValid() && methVal.Type().NumIn() == 0 && methVal.Type().NumOut() == 1 && methVal.Type().Out(0) == reflect.TypeOf("") {
		results := methVal.Call(nil)
		if len(results) > 0 {
			if name := results[0].String(); name != "" {
				return name
			}
		}
	}

	// Fallback convention
	return strings.ToLower(modelType.Name()) + "s" // Pluralize convention
}

// getModelID extracts the ID field value from a model using reflection.
func getModelID(value interface{}) int64 {
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() == reflect.Struct {
		// Prefer ID field from embedded BaseModel if present
		bmField := val.FieldByName("BaseModel")
		if bmField.IsValid() && bmField.Type() == reflect.TypeOf(BaseModel{}) {
			idField := bmField.FieldByName("ID")
			if idField.IsValid() && idField.Kind() == reflect.Int64 {
				return idField.Int()
			}
		}
		// Fallback to top-level ID field
		idField := val.FieldByName("ID")
		if idField.IsValid() && idField.Kind() == reflect.Int64 {
			return idField.Int()
		}
	}
	return 0 // Not found or invalid type
}

// getBaseModelPtr returns a pointer to the embedded BaseModel if it exists and is addressable.
func getBaseModelPtr(value interface{}) *BaseModel {
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() == reflect.Struct {
		bmField := val.FieldByName("BaseModel")
		// Ensure it's the correct type and we can get its address
		if bmField.IsValid() && bmField.Type() == reflect.TypeOf(BaseModel{}) && bmField.CanAddr() {
			return bmField.Addr().Interface().(*BaseModel)
		}
	}
	return nil
}

// setNewRecordFlagIfBaseModel sets the flag if the value embeds BaseModel.
// value must be a pointer to the struct.
func setNewRecordFlagIfBaseModel(value interface{}, isNew bool) {
	if bmPtr := getBaseModelPtr(value); bmPtr != nil {
		bmPtr.SetNewRecordFlag(isNew)
	}
}

// setCreatedAtTimestamp sets the CreatedAt field if it exists.
// value must be a pointer to the struct.
func setCreatedAtTimestamp(value interface{}, t time.Time) {
	if bmPtr := getBaseModelPtr(value); bmPtr != nil {
		bmPtr.CreatedAt = t
		return // Assume BaseModel handles it primarily
	}
	// Fallback to top-level field if no BaseModel
	val := reflect.ValueOf(value).Elem()
	field := val.FieldByName("CreatedAt")
	if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
		field.Set(reflect.ValueOf(t))
	}
}

// setUpdatedAtTimestamp sets the UpdatedAt field if it exists.
// value must be a pointer to the struct.
func setUpdatedAtTimestamp(value interface{}, t time.Time) {
	if bmPtr := getBaseModelPtr(value); bmPtr != nil {
		bmPtr.UpdatedAt = t
		return // Assume BaseModel handles it primarily
	}
	// Fallback to top-level field if no BaseModel
	val := reflect.ValueOf(value).Elem()
	field := val.FieldByName("UpdatedAt")
	if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
		field.Set(reflect.ValueOf(t))
	}
}

// --- Querying Structs ---

// QueryParams defines parameters for list queries.
type QueryParams struct {
	Where string        // Raw WHERE clause (e.g., "status = ? AND name LIKE ?")
	Args  []interface{} // Arguments for the WHERE clause placeholders
	Order string        // Raw ORDER BY clause (e.g., "created_at DESC")
	Start int           // Offset (for pagination)
	Limit int           // Limit (for pagination)
}

// --- Locking Helpers ---

// withLock acquires a lock, executes the action, and releases the lock.
// Requires the CacheClient to perform locking operations.
func withLock(ctx context.Context, cache CacheClient, lockKey string, action func(ctx context.Context) error) error {
	// Check if cache client is provided (it might be nil if caching is disabled)
	if cache == nil {
		log.Printf("Warning: Proceeding without lock for key '%s', cache client is nil", lockKey)
		return action(ctx) // Execute without lock
	}

	var acquired bool
	var err error
	for i := 0; i < LockMaxRetries; i++ {
		acquired, err = cache.AcquireLock(ctx, lockKey, LockDuration)
		if err != nil {
			// Log acquisition attempt error but maybe retry? For now, fail fast on error.
			return fmt.Errorf("failed to attempt lock acquisition for key '%s': %w", lockKey, err)
		}
		if acquired {
			break // Lock acquired
		}
		// Lock not acquired, wait and retry
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled while waiting for lock: %s", lockKey)
			return ctx.Err() // Respect context cancellation
		case <-time.After(LockRetryDelay):
			// Continue loop to retry
		}
	}

	if !acquired {
		return fmt.Errorf("failed to acquire lock for key '%s' after %d retries: %w", lockKey, LockMaxRetries, ErrLockNotAcquired)
	}

	// Defer lock release
	defer func() {
		releaseErr := cache.ReleaseLock(ctx, lockKey)
		if releaseErr != nil {
			// Log error, but don't return it as the main action might have succeeded
			log.Printf("Warning: Failed to release lock '%s': %v", lockKey, releaseErr)
		} else {
			log.Printf("Lock released for key '%s'", lockKey)
		}
	}()

	log.Printf("Lock acquired for key '%s'", lockKey)
	// Execute the action while holding the lock
	return action(ctx)
}

// --- Cache Key Helpers ---

// generateCacheKey creates a Redis key string for a single model.
func generateCacheKey(tableName string, id int64) string {
	// Consider adding a prefix for clarity, e.g., "model:"
	return fmt.Sprintf("model:%s:%d", tableName, id)
}

// --- Internal Implementation Methods (on *Thing, unexported) ---
// These contain the core logic, moved/adapted from the original package-level functions.
// They use t.db, t.cache and the explicitly passed ctx.

// byIDInternal fetches a single record by ID, checking cache first.
func (t *Thing[T]) byIDInternal(ctx context.Context, id int64, dest interface{}) error {
	if t.cache == nil || t.db == nil {
		return errors.New("Thing not properly initialized with DBAdapter and CacheClient")
	}
	// Ensure dest is a pointer to a struct
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("destination must be a pointer to a struct, got %T", dest)
	}

	cacheKey := generateCacheKey(t.info.tableName, id)

	// 1. Check cache
	cacheStart := time.Now()
	cachedErr := t.cache.GetModel(ctx, cacheKey, dest)
	cacheDuration := time.Since(cacheStart)
	if cachedErr == nil {
		log.Printf("CACHE HIT: Key: %s (%s)", cacheKey, cacheDuration)
		// Successfully retrieved from cache, ensure internal flags are set correctly
		setNewRecordFlagIfBaseModel(dest, false)
		return nil // Found in cache
	} else if !errors.Is(cachedErr, ErrNotFound) {
		// Log unexpected cache errors but proceed to DB
		log.Printf("WARN: Cache GetModel error for key %s: %v (%s)", cacheKey, cachedErr, cacheDuration)
	}
	// Log cache miss if it was ErrNotFound or another error occurred
	log.Printf("CACHE MISS: Key: %s (%s) - Error: %v", cacheKey, cacheDuration, cachedErr)

	// 2. Cache miss, query database
	selectSQL := buildSelectSQL(t.info)
	query := fmt.Sprintf("%s WHERE %s = ? LIMIT 1", selectSQL, t.info.pkName)

	dbStart := time.Now()
	dbErr := t.db.Get(ctx, dest, query, id) // Use the DBAdapter's Get method
	dbDuration := time.Since(dbStart)

	if dbErr != nil {
		if errors.Is(dbErr, ErrNotFound) {
			log.Printf("DB MISS: Key: %s (%s)", cacheKey, dbDuration)
			// Optional: Cache the fact that the record doesn't exist (negative caching)
			// To prevent repeated DB lookups for non-existent IDs.
			// Consider a shorter TTL for negative cache entries.
			// errCacheNone := t.cache.SetModel(ctx, cacheKey, &NoneResult, NoneResultCacheDuration) // Example, need NoneResult handling
			// if errCacheNone != nil {
			// 	log.Printf("WARN: Failed to set negative cache for key %s: %v", cacheKey, errCacheNone)
			// }
			return ErrNotFound // Return the standard not found error
		}
		log.Printf("DB ERROR: Key: %s (%s) - %v", cacheKey, dbDuration, dbErr)
		return fmt.Errorf("database query failed: %w", dbErr)
	}
	log.Printf("DB HIT: Key: %s (%s)", cacheKey, dbDuration)

	// 3. Found in DB, store in cache (async?)
	// Ensure internal flags are set after DB fetch
	setNewRecordFlagIfBaseModel(dest, false)
	// Maybe run this in a goroutine if cache write performance is critical?
	cacheSetStart := time.Now()
	errCacheSet := t.cache.SetModel(ctx, cacheKey, dest, DefaultCacheDuration) // Use appropriate TTL
	cacheSetDuration := time.Since(cacheSetStart)
	if errCacheSet != nil {
		// Log error but don't fail the overall operation
		log.Printf("WARN: Failed to cache model for key %s: %v (%s)", cacheKey, errCacheSet, cacheSetDuration)
	}

	return nil // Found in database
}

// saveInternal handles both creating new records and updating existing ones.
func (t *Thing[T]) saveInternal(ctx context.Context, value *T) error {
	if t.db == nil || t.cache == nil {
		return errors.New("Thing not properly initialized with DBAdapter and CacheClient")
	}

	modelValue := reflect.ValueOf(value)
	if modelValue.Kind() != reflect.Ptr || modelValue.IsNil() {
		return errors.New("value must be a non-nil pointer")
	}

	// --- Trigger BeforeSave hook ---
	if err := triggerEvent(ctx, EventTypeBeforeSave, value, nil); err != nil {
		return fmt.Errorf("BeforeSave hook failed: %w", err)
	}

	baseModelPtr := getBaseModelPtr(value)
	if baseModelPtr == nil {
		return errors.New("could not get BaseModel pointer, model must embed thing.BaseModel")
	}

	isNew := baseModelPtr.IsNewRecord() || baseModelPtr.ID == 0
	now := time.Now()
	var query string
	var args []interface{}
	var err error
	var result sql.Result
	var changedFields map[string]interface{} // Only used for update

	if isNew {
		// --- CREATE Path ---
		// Trigger BeforeCreate hook
		if err := triggerEvent(ctx, EventTypeBeforeCreate, value, nil); err != nil {
			return fmt.Errorf("BeforeCreate hook failed: %w", err)
		}

		setCreatedAtTimestamp(value, now)
		setUpdatedAtTimestamp(value, now)

		colsToInsert := []string{}
		placeholders := []string{}
		vals := []interface{}{}

		// Iterate through known columns from cached info
		for _, fieldName := range t.info.fields {
			colName := t.info.fieldToColumnMap[fieldName]
			// Skip PK column during insert (assuming auto-increment)
			if colName == t.info.pkName {
				continue
			}
			// Get field value using reflection
			fieldVal := modelValue.Elem().FieldByName(fieldName)
			if !fieldVal.IsValid() {
				// This shouldn't happen if info.fields is correct
				log.Printf("WARN: Field %s not found in model during insert preparation", fieldName)
				continue
			}
			colsToInsert = append(colsToInsert, colName)
			placeholders = append(placeholders, "?")
			vals = append(vals, fieldVal.Interface())
		}

		if len(colsToInsert) == 0 {
			return errors.New("no columns determined for insert")
		}

		query = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			t.info.tableName,
			strings.Join(colsToInsert, ", "),
			strings.Join(placeholders, ", "),
		)
		args = vals

		log.Printf("DB INSERT: %s [%v]", query, args)
		result, err = t.db.Exec(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("database insert failed: %w", err)
		}

		newID, errId := result.LastInsertId()
		if errId != nil {
			// Don't fail the whole operation, but log it. Cache might be inconsistent.
			log.Printf("WARN: Could not get LastInsertId after insert: %v", errId)
		} else {
			baseModelPtr.SetID(newID)
			// --- Cache Set on Create ---
			cacheKey := generateCacheKey(t.info.tableName, newID)
			cacheSetStart := time.Now()
			errCacheSet := t.cache.SetModel(ctx, cacheKey, value, DefaultCacheDuration)
			cacheSetDuration := time.Since(cacheSetStart)
			if errCacheSet != nil {
				log.Printf("WARN: Failed to cache model after create for key %s: %v (%s)", cacheKey, errCacheSet, cacheSetDuration)
			}
			// --- End Cache Set ---
		}

		// --- Trigger AfterCreate hook ---
		if errHook := triggerEvent(ctx, EventTypeAfterCreate, value, nil); errHook != nil {
			// Log error but don't fail the save operation
			log.Printf("WARN: AfterCreate hook failed: %v", errHook)
		}

	} else {
		// --- UPDATE Path ---
		id := baseModelPtr.ID
		if id == 0 {
			return errors.New("cannot update record with zero ID")
		}

		// Need to fetch the original record to find changed fields
		// This uses the ByID logic which includes caching.
		original := reflect.New(modelValue.Elem().Type()).Interface().(*T)
		if errFetch := t.byIDInternal(ctx, id, original); errFetch != nil {
			return fmt.Errorf("failed to fetch original record for update (ID: %d): %w", id, errFetch)
		}

		// Find changed fields using reflection helper (keys are DB column names)
		changedFields, err = findChangedFieldsReflection(original, value, t.info)
		if err != nil {
			return fmt.Errorf("failed to find changed fields for update: %w", err)
		}

		// If no fields changed (excluding UpdatedAt logic handled in findChangedFieldsReflection), skip DB update
		if len(changedFields) == 0 {
			log.Printf("Skipping update for %s %d: No relevant fields changed", t.info.tableName, id)
			// Still trigger AfterSave hook?
			if errHook := triggerEvent(ctx, EventTypeAfterSave, value, changedFields); errHook != nil {
				log.Printf("WARN: AfterSave hook failed (no changes): %v", errHook)
			}
			baseModelPtr.SetNewRecordFlag(false) // Ensure flag is correct
			return nil
		}

		// Always update the UpdatedAt timestamp in the model
		setUpdatedAtTimestamp(value, now)
		// Ensure updated_at is included in the map for the SQL query
		changedFields["updated_at"] = now

		// Build the UPDATE SQL
		setClauses := []string{}
		updateArgs := []interface{}{}

		// --- Iterate over the CHANGED DB columns ---
		// We iterate over a sorted list of keys for deterministic query generation (good for testing/logging)
		changedCols := make([]string, 0, len(changedFields))
		for colName := range changedFields {
			changedCols = append(changedCols, colName)
		}
		sort.Strings(changedCols) // Sort for deterministic order

		for _, colName := range changedCols {
			// Skip PK column in SET clause (shouldn't be in changedFields anyway, but defensive check)
			if colName == t.info.pkName {
				log.Printf("WARN: Primary key column '%s' found in changedFields map during update. Skipping.", colName)
				continue
			}
			setClauses = append(setClauses, fmt.Sprintf("%s = ?", colName))
			updateArgs = append(updateArgs, changedFields[colName])
		}
		// --- End iteration over changed columns ---

		if len(setClauses) == 0 {
			// This case should theoretically not be reached if len(changedFields) > 0 initially
			// and pkName is skipped correctly.
			log.Printf("ERROR: No SET clauses generated for update on %s %d despite changedFields being non-empty: %v", t.info.tableName, id, changedFields)
			return errors.New("internal error: no fields to update after processing changed fields")
		}

		query = fmt.Sprintf("UPDATE %s SET %s WHERE %s = ?",
			t.info.tableName,
			strings.Join(setClauses, ", "),
			t.info.pkName,
		)
		args = append(updateArgs, id) // Add the ID for the WHERE clause

		// --- Lock for Update and Cache Invalidation ---
		lockKey := fmt.Sprintf("lock:%s:%d", t.info.tableName, id)
		err = withLock(ctx, t.cache, lockKey, func(lctx context.Context) error {
			log.Printf("DB UPDATE: %s [%v]", query, args)
			result, err = t.db.Exec(lctx, query, args...)
			if err != nil {
				return fmt.Errorf("database update failed: %w", err)
			}

			rowsAffected, _ := result.RowsAffected()
			if rowsAffected == 0 {
				log.Printf("WARN: Update affected 0 rows for %s %d. Record might not exist?", t.info.tableName, id)
				// Consider returning ErrNotFound here as well?
			}

			// Invalidate Cache within the lock, after successful DB update
			cacheKey := generateCacheKey(t.info.tableName, id)
			cacheDelStart := time.Now()
			errCacheDel := t.cache.DeleteModel(lctx, cacheKey)
			cacheDelDuration := time.Since(cacheDelStart)
			if errCacheDel != nil {
				// Log failure but don't fail the lock action if DB succeeded
				log.Printf("WARN: Failed to delete model from cache during update lock for key %s: %v (%s)", cacheKey, errCacheDel, cacheDelDuration)
			}
			return nil // Lock action successful
		})
		// --- End Lock ---

		if err != nil {
			// Error occurred during lock acquisition or the action inside the lock
			return fmt.Errorf("save update failed (lock or db/cache exec): %w", err)
		}

		// --- TODO: Query Cache Invalidation ---
		// Deferring for now.
	}

	baseModelPtr.SetNewRecordFlag(false)

	// --- Trigger AfterSave hook (for both create and update) ---
	if errHook := triggerEvent(ctx, EventTypeAfterSave, value, changedFields); errHook != nil {
		// Log error but don't fail the save operation
		log.Printf("WARN: AfterSave hook failed: %v", errHook)
	}

	return nil
}

// deleteInternal removes a record from the database.
// value must be a pointer to a struct embedding BaseModel with a valid ID.
func (t *Thing[T]) deleteInternal(ctx context.Context, value interface{}) error {
	if t.db == nil || t.cache == nil {
		return errors.New("Thing not properly initialized with DBAdapter and CacheClient")
	}

	baseModelPtr := getBaseModelPtr(value)
	if baseModelPtr == nil {
		return errors.New("deleteInternal: value must embed BaseModel")
	}
	id := baseModelPtr.ID
	if id == 0 {
		return errors.New("deleteInternal: cannot delete record with zero ID")
	}

	// --- Trigger BeforeDelete hook ---
	if err := triggerEvent(ctx, EventTypeBeforeDelete, value, nil); err != nil {
		return fmt.Errorf("BeforeDelete hook failed: %w", err)
	}

	// Use the cached model info
	info := t.info
	tableName := info.tableName
	pkName := info.pkName

	// Lock the record during deletion
	lockKey := fmt.Sprintf("lock:%s:%d", tableName, id)
	err := withLock(ctx, t.cache, lockKey, func(lctx context.Context) error {
		// Build and execute the DELETE query
		query := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", tableName, pkName)
		log.Printf("DB DELETE: %s [%d]", query, id)
		result, execErr := t.db.Exec(lctx, query, id)
		if execErr != nil {
			return fmt.Errorf("database delete failed: %w", execErr)
		}
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			log.Printf("WARN: Delete affected 0 rows for %s %d. Record might not exist?", tableName, id)
			// Return ErrNotFound if 0 rows affected, indicating it wasn't there to delete
			return ErrNotFound
		}

		// --- Cache Invalidate within Lock ---
		cacheKey := generateCacheKey(tableName, id)
		cacheDelStart := time.Now()
		errCacheDel := t.cache.DeleteModel(lctx, cacheKey) // Use context from lock
		cacheDelDuration := time.Since(cacheDelStart)
		if errCacheDel != nil {
			// Log error but don't fail the lock action if DB succeeded
			log.Printf("WARN: Failed to delete model from cache during delete lock for key %s: %v (%s)", cacheKey, errCacheDel, cacheDelDuration)
		}
		// --- End Cache Invalidate ---

		return nil // Lock action successful
	})

	if err != nil {
		// Don't trigger AfterDelete hook if the lock or DB/Cache operation failed
		if errors.Is(err, ErrNotFound) {
			log.Printf("Attempted to delete non-existent record %s %d", tableName, id)
			return ErrNotFound // Propagate not found error
		}
		return fmt.Errorf("delete operation failed (lock or db/cache exec): %w", err)
	}

	// --- Trigger AfterDelete hook (only if lock and DB/Cache action succeeded) ---
	if errHook := triggerEvent(ctx, EventTypeAfterDelete, value, nil); errHook != nil {
		// Log error but don't fail the delete operation
		log.Printf("WARN: AfterDelete hook failed: %v", errHook)
	}

	return nil // Success
}

// queryInternal retrieves a slice of models based on query parameters.
func (t *Thing[T]) queryInternal(ctx context.Context, params QueryParams) ([]*T, error) {
	if t.db == nil || t.cache == nil {
		return nil, errors.New("Thing not properly initialized with DBAdapter and CacheClient")
	}

	// 1. Get IDs (will use query cache via idsInternal)
	ids, err := t.idsInternal(ctx, t.info, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get IDs for query: %w", err)
	}

	if len(ids) == 0 {
		return []*T{}, nil // Return empty slice if no IDs found
	}

	// 2. Fetch objects by ID (will use object cache via ByID)
	results := make([]*T, 0, len(ids))
	// Consider concurrent fetches? For now, sequential.
	for _, id := range ids {
		// Use the public ByID method which encapsulates cache/DB logic
		model, err := t.ByID(id) // ByID already uses the instance context (t.ctx)
		if err != nil {
			// Log error for this specific ID but continue fetching others
			log.Printf("WARN: Failed to fetch model %s with ID %d during query hydration: %v", t.info.tableName, id, err)
			// If ErrNotFound, it might indicate inconsistency between ID cache and object presence.
			// Decide if the whole query should fail or just skip this one.
			// For now, we skip and log.
			continue
		}
		results = append(results, model)
	}

	// TODO: Re-apply original query ordering if ByID fetches mess it up?
	// If the order matters and ByID fetches are out of order, we might need
	// to re-sort `results` based on the original `ids` order.
	// This is complex, deferring for now.

	log.Printf("Query Internal: Fetched %d / %d models for query", len(results), len(ids))
	return results, nil
}

// idsInternal retrieves a list of IDs based on query parameters, checking query cache first.
func (t *Thing[T]) idsInternal(ctx context.Context, info *modelInfo, params QueryParams) ([]int64, error) {
	if t.cache == nil || t.db == nil {
		return nil, errors.New("Thing not properly initialized with DBAdapter and CacheClient")
	}
	// 1. Generate Query Cache Key
	queryKey, err := generateQueryCacheKey(info.tableName, params)
	if err != nil {
		// Don't fail, just log and proceed without cache
		log.Printf("WARN: Failed to generate query cache key for table %s: %v. Proceeding without cache.", info.tableName, err)
	} else {
		// 2. Check Query Cache
		cacheStart := time.Now()
		cachedIDs, cacheErr := t.cache.GetQueryIDs(ctx, queryKey)
		cacheDuration := time.Since(cacheStart)

		if cacheErr == nil {
			log.Printf("QUERY CACHE HIT: Key: %s (%d IDs) (%s)", queryKey, len(cachedIDs), cacheDuration)
			return cachedIDs, nil // Found in cache
		} else if !errors.Is(cacheErr, ErrNotFound) {
			// Log unexpected cache errors but proceed to DB
			log.Printf("WARN: Cache GetQueryIDs error for key %s: %v (%s)", queryKey, cacheErr, cacheDuration)
		}
		// Log cache miss
		log.Printf("QUERY CACHE MISS: Key: %s (%s) - Error: %v", queryKey, cacheDuration, cacheErr)
	}

	// 3. Cache Miss or Key Generation Failed: Query Database
	query, args := buildSelectIDsSQL(info, params)
	if query == "" {
		return nil, errors.New("failed to build select IDs SQL")
	}

	// Destination slice for IDs. We know the PK type is int64.
	var fetchedIDs []int64
	dbStart := time.Now()
	err = t.db.Select(ctx, &fetchedIDs, query, args...)
	dbDuration := time.Since(dbStart)

	if err != nil {
		log.Printf("DB ERROR (Query IDs): %s [%v] (%s) - %v", query, args, dbDuration, err)
		return nil, fmt.Errorf("database query for IDs failed: %w", err)
	}
	log.Printf("DB HIT (Query IDs): %s [%v] (%d IDs) (%s)", query, args, len(fetchedIDs), dbDuration)

	// 4. Store fetched IDs in Query Cache (if key generation was successful)
	if queryKey != "" {
		cacheSetStart := time.Now()
		errCacheSet := t.cache.SetQueryIDs(ctx, queryKey, fetchedIDs, DefaultCacheDuration) // Use appropriate TTL
		cacheSetDuration := time.Since(cacheSetStart)
		if errCacheSet != nil {
			// Log error but don't fail the overall operation
			log.Printf("WARN: Failed to cache query IDs for key %s: %v (%s)", queryKey, errCacheSet, cacheSetDuration)
		}
	}

	return fetchedIDs, nil
}

// findChangedFieldsReflection compares two structs (original and updated)
// and returns a map of column names to the changed values from the updated struct.
// It handles embedded structs recursively.
func findChangedFieldsReflection[T any](original, updated *T, info *modelInfo) (map[string]interface{}, error) {
	changed := make(map[string]interface{})
	originalVal := reflect.ValueOf(original).Elem()
	updatedVal := reflect.ValueOf(updated).Elem()
	typeName := originalVal.Type().Name() // Get type name for logging

	if originalVal.Type() != updatedVal.Type() {
		return nil, errors.New("original and updated values must be of the same type")
	}

	var processFields func(oVal, uVal reflect.Value, path string) // Add path for nested logging
	processFields = func(oVal, uVal reflect.Value, path string) {
		structType := oVal.Type()
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)
			fieldName := field.Name
			currentPath := path + "." + fieldName

			// Handle embedded structs recursively
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				oField := oVal.Field(i)
				uField := uVal.Field(i)
				if oField.IsValid() && uField.IsValid() {
					log.Printf("DEBUG: [%s%s] Recursing into embedded struct", typeName, currentPath)
					processFields(oField, uField, currentPath)
				} else {
					log.Printf("DEBUG: [%s%s] Skipping invalid embedded struct", typeName, currentPath)
				}
				continue
			}

			// Skip unexported fields
			if !field.IsExported() {
				log.Printf("DEBUG: [%s%s] Skipping unexported field", typeName, currentPath)
				continue
			}

			// Get the DB column name for this field
			dbColName, exists := info.fieldToColumnMap[fieldName]
			if !exists || dbColName == "-" || dbColName == info.pkName {
				log.Printf("DEBUG: [%s%s] Skipping field (no db tag, ignored, or PK). Exists: %v, DBCol: %s, PK: %s", typeName, currentPath, exists, dbColName, info.pkName)
				continue
			}

			originalFieldVal := oVal.FieldByName(fieldName)
			updatedFieldVal := uVal.FieldByName(fieldName)

			if !originalFieldVal.IsValid() || !updatedFieldVal.IsValid() {
				log.Printf("Warning: Field %s%s not valid during change detection", typeName, currentPath)
				continue
			}

			originalInterface := originalFieldVal.Interface()
			updatedInterface := updatedFieldVal.Interface()

			// Compare values
			areEqual := reflect.DeepEqual(originalInterface, updatedInterface)
			log.Printf("DEBUG: [%s%s] Comparing DB:'%s' Original: [%v] (%T), Updated: [%v] (%T), Equal: %v",
				typeName, currentPath, dbColName, originalInterface, originalInterface, updatedInterface, updatedInterface, areEqual)

			if !areEqual {
				log.Printf("DEBUG: [%s%s] Change DETECTED for DB column '%s'", typeName, currentPath, dbColName)
				changed[dbColName] = updatedInterface
			}
		}
	}

	log.Printf("DEBUG: Starting change detection for type %s", typeName)
	processFields(originalVal, updatedVal, "") // Start with empty path
	log.Printf("DEBUG: Raw changed map for %s: %v", typeName, changed)

	// --- Fix for removing UpdatedAt ---
	// Get the DB column name for the UpdatedAt field. Assume it exists.
	// We need to find the Go field associated with the "updated_at" db column.
	var updatedAtDBColName string
	// This assumes the BaseModel standard convention. A more robust way might search info.columnToFieldMap
	// but this works if the field is 'UpdatedAt' and tag is 'updated_at'.
	updatedAtGoFieldName := "UpdatedAt" // Assuming standard BaseModel field name
	if col, ok := info.fieldToColumnMap[updatedAtGoFieldName]; ok {
		updatedAtDBColName = col // Should be "updated_at"
	}

	// Remove UpdatedAt DB column if it's the *only* change, as saveInternal handles it always.
	if updatedAtDBColName != "" && len(changed) == 1 {
		if _, isOnlyChange := changed[updatedAtDBColName]; isOnlyChange {
			log.Printf("DEBUG: Removing '%s' from changed map as it's the only change.", updatedAtDBColName)
			delete(changed, updatedAtDBColName)
		}
	}
	log.Printf("DEBUG: Final changed map for %s after UpdatedAt check: %v", typeName, changed)

	return changed, nil
}

// Use returns a Thing instance for the specified type T, using the globally
// configured DBAdapter and CacheClient.
// The package MUST be configured using Configure() before calling Use[T].
func Use[T any]() (*Thing[T], error) {
	configMutex.RLock()
	defer configMutex.RUnlock()
	if !isConfigured {
		return nil, errors.New("thing.Use[T] called before thing.Configure()")
	}
	// Create a new Thing instance using the global adapters
	return New[T](globalDB, globalCache)
}
