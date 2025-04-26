package thing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log" // For placeholder logging
	"reflect"
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

// buildSelectSQL constructs a SELECT statement for fetching a single record by ID.
// Uses columns from the cached modelInfo.
func buildSelectSQL(info *modelInfo) string {
	if info.tableName == "" || info.pkName == "" || len(info.columns) == 0 {
		// Log error or panic? Returning empty might cause SQL errors later.
		log.Printf("Error: buildSelectSQL called with incomplete modelInfo: %+v", info)
		return ""
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?",
		strings.Join(info.columns, ", "),
		info.tableName,
		info.pkName,
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

// byIDInternal fetches a record by ID into the dest pointer.
// dest must be a non-nil pointer to a struct.
func (t *Thing[T]) byIDInternal(ctx context.Context, id int64, dest interface{}) error {
	// Validate dest
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() || destVal.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("dest must be a non-nil pointer to a struct, got %T", dest)
	}
	destType := destVal.Elem().Type()
	db := t.db
	cache := t.cache
	info, err := getCachedModelInfo(destType)
	if err != nil {
		return fmt.Errorf("could not get model info for ByID %s: %w", destType.Name(), err)
	}
	tableName := getTableName(dest)
	if tableName == "" {
		return fmt.Errorf("could not determine table name for ByID type %T", dest)
	}
	info.tableName = tableName
	cacheKey := generateCacheKey(info.tableName, id)
	err = cache.GetModel(ctx, cacheKey, dest)
	if err == nil {
		log.Printf("Thing Cache hit for %s", cacheKey)
		setNewRecordFlagIfBaseModel(dest, false)
		return nil
	} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, RedisNilError) {
		log.Printf("Thing Cache error for %s: %v. Proceeding with DB query.", cacheKey, err)
	} else {
		log.Printf("Thing Cache miss for %s", cacheKey)
	}
	query := buildSelectSQL(info)
	if query == "" {
		return errors.New("failed to build select SQL in byIDInternal")
	}
	log.Printf("Thing executing DB query for %s: %s [%d]", info.tableName, query, id)
	err = db.Get(ctx, dest, query, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNotFound) {
			log.Printf("Thing record not found in DB for %s", cacheKey)
			_ = cache.SetModel(ctx, cacheKey, nil, NoneResultCacheDuration)
			return ErrNotFound
		}
		log.Printf("Thing database error fetching %s: %v", cacheKey, err)
		return fmt.Errorf("database error fetching %s %d: %w", info.tableName, id, err)
	}
	log.Printf("Thing caching result for %s", cacheKey)
	errCache := cache.SetModel(ctx, cacheKey, dest, DefaultCacheDuration)
	if errCache != nil {
		log.Printf("Warning: Thing failed to cache result for %s: %v", cacheKey, errCache)
	}
	setNewRecordFlagIfBaseModel(dest, false)
	return nil
}

// saveInternal handles both create and update logic now.
// Changed signature: value is *T, returns only error.
func (t *Thing[T]) saveInternal(ctx context.Context, value *T) error {
	db := t.db
	cache := t.cache
	// Ensure t.info is available first.
	if t.info == nil {
		// Attempt to compute info if missing (shouldn't happen if New worked)
		modelType := reflect.TypeOf((*T)(nil)).Elem()
		info, err := getCachedModelInfo(modelType)
		if err != nil {
			return fmt.Errorf("saveInternal: failed to get model info for type %T: %w", value, err)
		}
		t.info = info
		log.Printf("Warning: saveInternal had to compute model info for %T", value)
	}

	// Ensure table name is set in t.info *before* any operation needing it.
	if t.info.tableName == "" {
		t.info.tableName = getTableName(value)
		if t.info.tableName == "" {
			return fmt.Errorf("saveInternal: could not determine table name for type %T", value)
		}
	}
	// Now use the consistently populated t.info
	info := t.info

	valPtr := interface{}(value) // For reflection helpers needing interface{}

	id := getModelID(valPtr)
	// Default to isNew if ID is 0
	isNew := id == 0
	// Allow overriding ONLY if the flag is explicitly set to true (to force create)
	if bmPtr := getBaseModelPtr(valPtr); bmPtr != nil && bmPtr.IsNewRecord() {
		isNew = true
	}

	if isNew {
		// --- Perform Create Logic Directly ---
		if id != 0 {
			log.Printf("Warning: Save called on model marked as new, but ID (%d) is non-zero. Treating as Create.", id)
			// Optionally reset ID here? Depends on desired behavior.
			// If using auto-increment, DB typically ignores passed ID on INSERT.
		}

		now := time.Now()
		setCreatedAtTimestamp(valPtr, now)
		setUpdatedAtTimestamp(valPtr, now)
		setNewRecordFlagIfBaseModel(valPtr, true) // Mark as new for event

		if err := triggerEvent(ctx, EventTypeBeforeCreate, valPtr, nil); err != nil {
			return err
		}

		sqlStmt, columnsToInsert := buildInsertSQL(info)
		if sqlStmt == "" {
			return errors.New("save(create): failed to build insert SQL")
		}
		values, err := getValuesForColumns(valPtr, info, columnsToInsert)
		if err != nil {
			return fmt.Errorf("save(create): %w", err)
		}

		result, err := db.Exec(ctx, sqlStmt, values...)
		if err != nil {
			return fmt.Errorf("save(create): db error creating %s: %w", info.tableName, err)
		}

		newId, errId := result.LastInsertId()
		if errId != nil {
			log.Printf("Warning: save(create): could not get LastInsertId for %s: %v", info.tableName, errId)
		} else {
			structVal := reflect.ValueOf(value).Elem()
			if idField := structVal.FieldByName("ID"); idField.IsValid() && idField.CanSet() && idField.Kind() == reflect.Int64 {
				idField.SetInt(newId)
				id = newId // Update local id variable for caching
			} else {
				log.Printf("Warning: save(create): could not set ID field after insert for %s", info.tableName)
			}
		}

		setNewRecordFlagIfBaseModel(valPtr, false) // No longer new

		_ = triggerEvent(ctx, EventTypeAfterCreate, valPtr, nil) // Log error only

		if id > 0 { // Use updated id
			cacheKey := generateCacheKey(info.tableName, id)
			_ = cache.SetModel(ctx, cacheKey, valPtr, DefaultCacheDuration) // Log error only
		}
		return nil // Return nil error on success
	}

	// --- Perform Update Logic ---
	originalValue := new(T)
	// byIDInternal will use t.info implicitly via receiver, but its local info doesn't matter now.
	// The crucial part is that the SQL/cache keys below use the consistent t.info.
	err := t.byIDInternal(ctx, id, originalValue)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Use the consistent info.tableName
			return fmt.Errorf("cannot save non-existent %s %d", info.tableName, id)
		}
		return fmt.Errorf("save(update): failed to fetch original: %w", err)
	}

	// Use the consistent info
	changedFields, err := findChangedFieldsReflection(originalValue, value, info)
	if err != nil {
		return fmt.Errorf("save(update): field comparison error: %w", err)
	}

	if len(changedFields) == 0 {
		setNewRecordFlagIfBaseModel(valPtr, false)
		// Use info from t.info
		log.Printf("Save: No changes detected for %s %d. Skipping DB update.", info.tableName, id)
		// Change return type to just error
		return nil // No changes, return success (nil error)
	}

	now := time.Now()
	setUpdatedAtTimestamp(valPtr, now)
	// Use info from t.info
	if updatedAtCol, ok := info.fieldToColumnMap["UpdatedAt"]; ok {
		changedFields[updatedAtCol] = now
	} else {
		// Use info from t.info
		log.Printf("Warning: save(update): UpdatedAt field not found in metadata for %s", info.tableName)
	}

	// Use info from t.info
	sqlStmt, updateValues, err := buildUpdateSQL(info, changedFields)
	if err != nil {
		return fmt.Errorf("save(update): build update sql: %w", err)
	}
	if sqlStmt == "" {
		return errors.New("save(update): no updatable fields provided")
	}
	updateValues = append(updateValues, id)

	if err := triggerEvent(ctx, EventTypeBeforeSave, valPtr, changedFields); err != nil {
		return err
	}

	// Use info from t.info
	lockKey := fmt.Sprintf("lock:%s:%d", info.tableName, id)
	err = withLock(ctx, cache, lockKey, func(lctx context.Context) error {
		_, execErr := db.Exec(lctx, sqlStmt, updateValues...)
		return execErr
	})
	if err != nil {
		return fmt.Errorf("save(update): lock/db error: %w", err)
	}

	setNewRecordFlagIfBaseModel(valPtr, false)
	_ = triggerEvent(ctx, EventTypeAfterSave, valPtr, changedFields) // Log error only

	// Use info from t.info
	cacheKey := generateCacheKey(info.tableName, id)
	_ = cache.DeleteModel(ctx, cacheKey) // Log error only

	return nil // Return nil error on success
}

// deleteInternal removes a record.
// value must be a non-nil pointer to a struct with ID set.
func (t *Thing[T]) deleteInternal(ctx context.Context, value interface{}) error {
	// Validate value
	val := reflect.ValueOf(value)
	if val.Kind() != reflect.Ptr || val.IsNil() || val.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("value must be a non-nil pointer to a struct, got %T", value)
	}
	structType := val.Elem().Type()
	id := getModelID(value)
	if id == 0 {
		return fmt.Errorf("cannot delete model with ID 0")
	}
	db := t.db
	cache := t.cache
	info, err := getCachedModelInfo(structType)
	if err != nil {
		return fmt.Errorf("could not get model info for delete: %w", err)
	}
	tableName := getTableName(value)
	if tableName == "" {
		return fmt.Errorf("could not determine table name for delete type %T", value)
	}
	info.tableName = tableName
	if err := triggerEvent(ctx, EventTypeBeforeDelete, value, nil); err != nil {
		return err
	}
	sqlStmt := buildDeleteSQL(info)
	if sqlStmt == "" {
		return errors.New("failed to build delete SQL in deleteInternal")
	}
	lockKey := fmt.Sprintf("lock:%s:%d", info.tableName, id)
	err = withLock(ctx, cache, lockKey, func(lctx context.Context) error {
		log.Printf("Thing executing DB Delete for %s %d: %s", info.tableName, id, sqlStmt)
		result, execErr := db.Exec(lctx, sqlStmt, id)
		if execErr != nil {
			return fmt.Errorf("database error deleting %s %d: %w", info.tableName, id, execErr)
		}
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			log.Printf("Warning: Delete affected 0 rows for %s %d.", info.tableName, id)
		}
		return nil
	})
	if err != nil {
		return err
	}
	err = triggerEvent(ctx, EventTypeAfterDelete, value, nil)
	if err != nil {
		log.Printf("Error executing AfterDelete listener for %s %d: %v", info.tableName, id, err)
	}
	cacheKey := generateCacheKey(info.tableName, id)
	log.Printf("Thing deleting cache after delete for %s", cacheKey)
	errCache := cache.DeleteModel(ctx, cacheKey)
	if errCache != nil {
		log.Printf("Warning: Thing failed to delete cache after delete for %s: %v", cacheKey, errCache)
	}
	log.Printf("Thing successfully deleted %s %d", info.tableName, id)
	return nil
}

// queryInternal executes a query and returns a slice of results ([]*T).
func (t *Thing[T]) queryInternal(ctx context.Context, params QueryParams) ([]*T, error) {
	// 1. Determine the target type T - We already know T from the receiver t *Thing[T]
	// We can access the pre-computed info via t.info
	info := t.info
	if info == nil { // Should not happen if NewFor worked correctly
		return nil, errors.New("queryInternal: model info not available on Thing instance")
	}

	// 2. Get IDs matching the query - Pass the pre-computed info
	ids, err := t.idsInternal(ctx, info, params) // Use internal method with ctx and info
	if err != nil {
		return nil, fmt.Errorf("queryInternal failed to get IDs: %w", err)
	}

	// 3. If no IDs found, return empty slice immediately
	if len(ids) == 0 {
		return []*T{}, nil
	}

	// 4. Fetch each model by ID (Hydration)
	results := make([]*T, 0, len(ids))
	for _, id := range ids {
		instance := new(T)
		// Call internal ByID, passing the info as well
		err := t.byIDInternal(ctx, id, instance)
		if err == nil {
			results = append(results, instance)
		} else if errors.Is(err, ErrNotFound) {
			log.Printf("Warning: Record %d (type %s) not found during query hydration", id, info.tableName)
		} else {
			log.Printf("Error fetching record %d (type %s) during query hydration: %v", id, info.tableName, err)
			return nil, fmt.Errorf("error fetching record %d (%s) during query: %w", id, info.tableName, err)
		}
	}

	// 5. Return the hydrated results
	return results, nil
}

// idsInternal fetches only the primary key IDs matching the query.
// It now accepts info *modelInfo directly
func (t *Thing[T]) idsInternal(ctx context.Context, info *modelInfo, params QueryParams) ([]int64, error) {
	db := t.db
	cache := t.cache

	// Added check for info validity, including table name which is needed now.
	if info == nil || info.tableName == "" {
		// Try to determine table name if missing
		if info != nil && info.tableName == "" {
			modelType := reflect.TypeOf((*T)(nil)).Elem()
			info.tableName = getTableNameFromType(modelType)
			if info.tableName == "" {
				return nil, errors.New("idsInternal: invalid model info provided (missing table name)")
			}
			log.Printf("Warning: idsInternal determined table name '%s' dynamically.", info.tableName)
		} else {
			return nil, errors.New("idsInternal: invalid model info provided")
		}
	}

	// Generate cache key
	queryCacheKey, err := generateQueryCacheKey(info.tableName, params)
	cacheable := err == nil && queryCacheKey != ""
	if err != nil {
		log.Printf("Error generating query cache key (proceeding without query cache): %v", err)
	}

	// 1. Check query cache if possible
	if cacheable {
		cachedIDs, err := cache.GetQueryIDs(ctx, queryCacheKey)
		if err == nil {
			log.Printf("Thing Query cache hit for key: %s", queryCacheKey)
			return cachedIDs, nil
		} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, RedisNilError) {
			log.Printf("Thing Query cache error for key %s: %v", queryCacheKey, err)
		} else {
			log.Printf("Thing Query cache miss for key: %s", queryCacheKey)
		}
	}

	// 2. Database Query
	sqlStmt, args := buildSelectIDsSQL(info, params)
	if sqlStmt == "" {
		return nil, errors.New("idsInternal: failed to build select IDs SQL")
	}
	log.Printf("Thing executing DB IDs query for %s: %s | Args: %v", info.tableName, sqlStmt, args)

	var ids []int64
	err = db.Select(ctx, &ids, sqlStmt, args...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("No IDs found in DB for query: %s", sqlStmt)
			if cacheable {
				_ = cache.SetQueryIDs(ctx, queryCacheKey, []int64{}, NoneResultCacheDuration)
			}
			return []int64{}, nil
		}
		return nil, fmt.Errorf("db error fetching IDs for %s: %w", info.tableName, err)
	}

	// 3. Cache Result if possible
	if cacheable {
		_ = cache.SetQueryIDs(ctx, queryCacheKey, ids, DefaultCacheDuration)
	}

	return ids, nil
}

// findChangedFieldsReflection compares two structs (original and updated)
// and returns a map of column names to the changed values from the updated struct.
// NOTE: This is a basic implementation. It compares exported fields based on 'db' tags.
// It does not handle complex types (slices, maps, nested structs) deeply.
func findChangedFieldsReflection[T any](original, updated *T, info *modelInfo) (map[string]interface{}, error) {
	changed := make(map[string]interface{})
	originalVal := reflect.ValueOf(original).Elem()
	updatedVal := reflect.ValueOf(updated).Elem()
	modelType := originalVal.Type() // Assumes original and updated are same type

	if originalVal.Type() != updatedVal.Type() {
		return nil, errors.New("original and updated values must be of the same type")
	}

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		fieldName := field.Name

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get the DB column name for this field
		dbColName, exists := info.fieldToColumnMap[fieldName]
		if !exists || dbColName == "-" || dbColName == info.pkName {
			// Skip fields without 'db' tag, explicitly ignored, or the primary key
			continue
		}

		originalFieldVal := originalVal.FieldByName(fieldName)
		updatedFieldVal := updatedVal.FieldByName(fieldName)

		if !originalFieldVal.IsValid() || !updatedFieldVal.IsValid() {
			log.Printf("Warning: Field %s not valid during change detection for type %s", fieldName, modelType.Name())
			continue
		}

		// Basic comparison - uses reflect.DeepEqual for simplicity.
		// May need adjustments for specific types (e.g., time.Time comparison precision).
		if !reflect.DeepEqual(originalFieldVal.Interface(), updatedFieldVal.Interface()) {
			changed[dbColName] = updatedFieldVal.Interface()
		}
	}

	// Handle embedded BaseModel specifically if needed (e.g., UpdatedAt)
	// The loop above should handle fields within BaseModel if they are exported and tagged.
	// The UpdatedAt timestamp is handled separately in saveInternal.

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
