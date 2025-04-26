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
// It's the central function to access struct metadata, avoiding repeated reflection.
func getCachedModelInfo(modelType reflect.Type) (*modelInfo, error) {
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct type, got %s", modelType.Kind())
	}

	// Check cache first
	if cached, ok := modelCache.Load(modelType); ok {
		// log.Printf("Metadata cache hit for %s", modelType.Name())
		return cached.(*modelInfo), nil
	}

	// --- Not in cache, perform reflection ---
	// log.Printf("Metadata cache miss for %s, reflecting...", modelType.Name())
	info := &modelInfo{
		fieldToColumnMap: make(map[string]string),
		columnToFieldMap: make(map[string]string),
	}
	pkDbName := "" // Database name of the primary key field

	numFields := modelType.NumField()
	info.columns = make([]string, 0, numFields)
	info.fields = make([]string, 0, numFields)

	// Recursive function to handle embedded structs
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

			// Skip fields marked with "-" or unexported fields
			if dbTag == "-" || !field.IsExported() {
				continue
			}

			// Regular exported fields
			colName := dbTag
			if colName == "" {
				colName = strings.ToLower(field.Name) // Fallback naming convention
			}
			// Check if this regular field is the PK (if not already found in BaseModel)
			if colName == "id" && pkDbName == "" {
				pkDbName = colName
			}
			info.columns = append(info.columns, colName)
			info.fields = append(info.fields, field.Name) // Store Go field name
			info.fieldToColumnMap[field.Name] = colName
			info.columnToFieldMap[colName] = field.Name
		}
	}

	// Start processing from the top-level model type
	processFields(modelType)

	if pkDbName == "" {
		return nil, fmt.Errorf("primary key column (assumed 'id') not found via 'db:\"id\"' tag in struct %s or its embedded BaseModel", modelType.Name())
	}
	info.pkName = pkDbName

	// Infer table name
	// Check if the type implements the Model interface which includes TableName()
	if modelType.Implements(reflect.TypeOf((*Model)(nil)).Elem()) {
		// Create a zero value pointer to the type to call the method
		instPtr := reflect.New(modelType) // *T
		if instModel, ok := instPtr.Interface().(Model); ok {
			tableName := instModel.TableName() // Call method if exists
			if tableName != "" {
				info.tableName = tableName
			}
		}
	}
	// Fallback to simple convention if method returns empty or doesn't exist/implement Model
	if info.tableName == "" {
		info.tableName = strings.ToLower(modelType.Name())
	}

	// --- Store in cache ---
	actualInfo, loaded := modelCache.LoadOrStore(modelType, info)
	if loaded {
		return actualInfo.(*modelInfo), nil
	}
	return info, nil
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
type EventListener func(ctx context.Context, eventType EventType, model Model, eventData interface{}) error

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
// It passes the model instance and any additional event data.
func triggerEvent(ctx context.Context, eventType EventType, model Model, eventData interface{}) error {
	listenerMutex.RLock()
	listeners := listenerRegistry[eventType]
	listenerMutex.RUnlock()

	if len(listeners) == 0 {
		return nil
	}

	// Execute listeners sequentially. Consider concurrent execution if needed,
	// but sequential is simpler for managing potential side effects and errors.
	for _, listener := range listeners {
		err := listener(ctx, eventType, model, eventData)
		if err != nil {
			// Decide on error handling: stop on first error, or collect all errors?
			// Stopping on first error is simpler.
			log.Printf("Error executing listener for event %s on model %T(%d): %v", eventType, model, model.GetID(), err)
			return fmt.Errorf("event listener for %s failed: %w", eventType, err)
		}
	}
	return nil
}

// --- Configuration ---

// Global variables to hold the default configured clients.
// Package-level functions will use these by default.
// Model instances can also be populated with these.
var (
	defaultDbAdapter   DBAdapter   // Changed from dbClient DBExecutor
	defaultCacheClient CacheClient // Changed from redisClient RedisClient
	clientMutex        sync.RWMutex
)

// Configure sets the default package-level DB and Cache clients.
// These clients will be used by package-level functions and injected
// into BaseModel instances created/fetched by the ORM functions.
func Configure(db DBAdapter, cache CacheClient) {
	clientMutex.Lock()
	defer clientMutex.Unlock()
	// Basic check to ensure clients are not nil
	if db == nil || cache == nil {
		log.Fatal("DBAdapter and CacheClient must be configured and non-nil")
	}
	defaultDbAdapter = db
	defaultCacheClient = cache
	log.Println("Thing ORM configured with default DB adapter and Cache client.")
}

// GetDBAdapter returns the configured default DB adapter.
func GetDBAdapter() DBAdapter {
	clientMutex.RLock()
	defer clientMutex.RUnlock()
	if defaultDbAdapter == nil {
		log.Println("Warning: Accessing DB adapter before Configure() was called.")
	}
	return defaultDbAdapter
}

// GetCacheClient returns the configured default Cache client.
func GetCacheClient() CacheClient {
	clientMutex.RLock()
	defer clientMutex.RUnlock()
	if defaultCacheClient == nil {
		log.Println("Warning: Accessing Cache client before Configure() was called.")
	}
	return defaultCacheClient
}

// ModelWithBaseModel is an internal interface for models that embed BaseModel
type ModelWithBaseModel interface {
	Model
	GetBaseModel() *BaseModel
}

// --- BaseModel Struct ---

// BaseModel provides common fields and functionality for database models.
// It should be embedded into specific model structs.
type BaseModel struct {
	ID        int64     `json:"id" db:"id"`                 // Primary key
	CreatedAt time.Time `json:"created_at" db:"created_at"` // Timestamp for creation
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"` // Timestamp for last update

	// --- Internal ORM state ---
	// We embed the clients here so instance methods can use them.
	// These fields should be populated by the ORM functions (ByID, Create, etc.).
	// They are NOT saved to the DB or cache.
	dbAdapter   DBAdapter   `json:"-" db:"-"`
	cacheClient CacheClient `json:"-" db:"-"`
	isNewRecord bool        `json:"-" db:"-"` // Flag to indicate if the record is new
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
// Default implementation returns empty string, relying on getCachedModelInfo's logic.
// Override this method in your specific model struct for custom table names.
func (b *BaseModel) TableName() string {
	return "" // Rely on getCachedModelInfo to use convention or tag
}

// GetBaseModel returns the BaseModel pointer
func (b *BaseModel) GetBaseModel() *BaseModel {
	return b
}

// SetAdapter sets the DB and Cache clients for this model instance.
// This is primarily for internal ORM use (e.g., after fetching).
func (b *BaseModel) SetAdapter(db DBAdapter, cache CacheClient) {
	b.dbAdapter = db
	b.cacheClient = cache
}

// IsNewRecord returns whether this is a new record
func (b *BaseModel) IsNewRecord() bool {
	return b.isNewRecord
}

// SetNewRecordFlag sets the internal isNewRecord flag
func (b *BaseModel) SetNewRecordFlag(isNew bool) {
	b.isNewRecord = isNew
}

// --- Helper Functions ---

// Helper function to get the embedded BaseModel pointer using reflection.
// Returns nil if not found or not accessible.
func getBaseModelPtr(modelPtr interface{}) *BaseModel {
	v := reflect.ValueOf(modelPtr)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return nil
	}
	bmField := v.FieldByName("BaseModel")
	if !bmField.IsValid() || bmField.Type() != reflect.TypeOf(BaseModel{}) {
		return nil
	}
	if bmField.CanAddr() {
		// Ensure we are getting a pointer to the actual field within the struct
		bmPtr, ok := bmField.Addr().Interface().(*BaseModel)
		if ok {
			return bmPtr
		}
	}
	log.Printf("Warning: Could not get addressable *BaseModel from type %T", modelPtr)
	return nil
}

// generateCacheKey creates a Redis key string.
func generateCacheKey(tableName string, id int64) string {
	// Mimics the PHP logic: prefix (table name) : id
	return fmt.Sprintf("%s:%d", tableName, id)
}

// packData serializes model data for caching. Uses JSON here.
func packData(data interface{}) (string, error) {
	// Go version: Just JSON for simplicity. Add compression if needed.
	packed, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to pack data for cache: %w", err)
	}
	return string(packed), nil
}

// unpackData deserializes cached data into the target model struct.
func unpackData(packed string, target interface{}) error {
	// Go version: Just JSON. Add decompression if needed.
	if packed == "" {
		// Handle case where cache might return empty string instead of error for nil
		return fmt.Errorf("cannot unpack empty string")
	}
	err := json.Unmarshal([]byte(packed), target)
	if err != nil {
		return fmt.Errorf("failed to unpack cached data: %w", err)
	}
	return nil
}

// --- Locking Helpers ---

// withLock acquires a lock, executes the action, and releases the lock.
func withLock(ctx context.Context, lockKey string, action func(ctx context.Context) error) error {
	cache := GetCacheClient()
	if cache == nil {
		log.Printf("Warning: Proceeding without lock for key '%s', cache client not configured", lockKey)
		return action(ctx) // Execute without lock if cache isn't configured
	}

	var acquired bool
	var err error
	for i := 0; i < LockMaxRetries; i++ {
		acquired, err = cache.AcquireLock(ctx, lockKey, LockDuration)
		if err != nil {
			return fmt.Errorf("failed to attempt lock acquisition for key '%s': %w", lockKey, err)
		}
		if acquired {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err() // Context cancelled
		case <-time.After(LockRetryDelay):
			// Retry
		}
	}

	if !acquired {
		return fmt.Errorf("failed to acquire lock for key '%s' after %d retries: %w", lockKey, LockMaxRetries, ErrLockNotAcquired)
	}

	defer func() {
		releaseErr := cache.ReleaseLock(ctx, lockKey)
		if releaseErr != nil {
			log.Printf("Warning: Failed to release lock '%s': %v", lockKey, releaseErr)
		}
	}()

	// Execute the action while holding the lock
	return action(ctx)
}

// --- Core ORM Functions ---

// ByID retrieves a single record by its primary key.
// It checks the cache first, then falls back to the database.
func ByID[T Model](ctx context.Context, id int64) (*T, error) {
	db := GetDBAdapter()
	cache := GetCacheClient()
	if db == nil || cache == nil {
		return nil, errors.New("database or cache client not configured")
	}

	// Create a zero value ptr instance (*T) to get type info
	modelPtr := new(T)
	modelType := reflect.TypeOf(modelPtr).Elem()

	// Get model metadata (table name, pk name, columns) from cache
	info, err := getCachedModelInfo(modelType)
	if err != nil {
		return nil, fmt.Errorf("could not get model info for %s: %w", modelType.Name(), err)
	}

	cacheKey := generateCacheKey(info.tableName, id)

	// 1. Check Cache
	cachedModel := new(T)                                         // Create a new model pointer for cache retrieval
	err = cache.GetModel(ctx, cacheKey, interface{}(cachedModel)) // Convert to interface{}
	if err == nil {
		log.Printf("Cache hit for %s %d", info.tableName, id)
		// Set adapter and new record flag via reflection helper
		if bm := getBaseModelPtr(cachedModel); bm != nil {
			bm.SetAdapter(db, cache)
			bm.SetNewRecordFlag(false)
		}
		return cachedModel, nil
	} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, RedisNilError) { // Check for actual cache errors
		log.Printf("Cache error for %s %d: %v", info.tableName, id, err)
		// Decide: Proceed to DB or return error? Proceeding might hide cache issues. Let's proceed for now.
	} else {
		log.Printf("Cache miss for %s %d", info.tableName, id)
	}

	// 2. Database Query (Cache Miss or Error)
	// Use cached info to build the query
	query := buildSelectSQL(info) // Pass cached info

	log.Printf("Executing DB query for %s %d: %s", info.tableName, id, query)
	// Create another new instance to scan into from DB
	model := new(T)
	err = db.Get(ctx, interface{}(model), query, id) // Convert to interface{}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("Record not found in DB for %s %d", info.tableName, id)
			// Cache the fact that it's not found
			_ = cache.SetModel(ctx, cacheKey, nil, NoneResultCacheDuration) // Use nil for not found
			return nil, ErrNotFound
		}
		log.Printf("Database error fetching %s %d: %v", info.tableName, id, err)
		return nil, fmt.Errorf("database error fetching %s %d: %w", info.tableName, id, err)
	}

	// 3. Cache Result
	log.Printf("Caching result for %s %d", info.tableName, id)
	_ = cache.SetModel(ctx, cacheKey, interface{}(model), DefaultCacheDuration) // Convert to interface{}

	// Set adapter and new record flag via reflection helper
	if bm := getBaseModelPtr(model); bm != nil {
		bm.SetAdapter(db, cache)
		bm.SetNewRecordFlag(false)
	}
	return model, nil
}

// Create inserts a new record into the database.
// It sets CreatedAt/UpdatedAt timestamps and clears the relevant cache entries.
func Create[T Model](ctx context.Context, model *T) error {
	db := GetDBAdapter()
	cache := GetCacheClient()
	if db == nil || cache == nil {
		return errors.New("database or cache client not configured")
	}

	modelValue := reflect.ValueOf(model).Elem()
	modelType := modelValue.Type()

	// Get model metadata from cache
	info, err := getCachedModelInfo(modelType)
	if err != nil {
		return fmt.Errorf("could not get model info for %s: %w", modelType.Name(), err)
	}

	// Set timestamps
	now := time.Now()
	setCreatedAtTimestamp(model, now)
	setUpdatedAtTimestamp(model, now)

	// Prepare data for insert
	sqlStmt, columnsToInsert := buildInsertSQL(info) // Use cached info
	values, err := getValuesForColumns(model, info, columnsToInsert)
	if err != nil {
		return fmt.Errorf("could not extract values for create: %w", err)
	}

	// Trigger BeforeCreate event
	err = triggerEvent(ctx, EventTypeBeforeCreate, interface{}(model), nil) // Convert to interface{}
	if err != nil {
		return err
	}

	// Execute Insert
	log.Printf("Executing DB Insert for %s: %s | Args: %v", info.tableName, sqlStmt, values)
	result, err := db.Exec(ctx, sqlStmt, values...)
	if err != nil {
		log.Printf("Database error creating %s: %v", info.tableName, err)
		return fmt.Errorf("database error creating %s: %w", info.tableName, err)
	}

	// Set the ID from the result
	id, err := result.LastInsertId()
	if err != nil {
		// Not all drivers support LastInsertId. Consider alternatives if needed.
		log.Printf("Warning: Could not get LastInsertId for %s: %v", info.tableName, err)
	} else {
		(*model).SetID(id)
	}

	// Mark as not new and inject clients using reflection
	if bm := getBaseModelPtr(model); bm != nil {
		bm.SetNewRecordFlag(false)
		bm.SetAdapter(db, cache)
	}

	// Trigger AfterCreate event
	err = triggerEvent(ctx, EventTypeAfterCreate, interface{}(model), nil) // Convert to interface{}
	if err != nil {
		log.Printf("Error executing AfterCreate listener for %s %d: %v", info.tableName, (*model).GetID(), err)
	}

	// Invalidate/Update Cache (individual record)
	cacheKey := generateCacheKey(info.tableName, (*model).GetID())
	log.Printf("Setting cache after create for %s", cacheKey)
	_ = cache.SetModel(ctx, cacheKey, interface{}(model), DefaultCacheDuration) // Convert to interface{}

	// TODO: Consider more sophisticated query cache invalidation later.

	log.Printf("Successfully created %s %d", info.tableName, (*model).GetID())
	return nil
}

// Save updates an existing record or creates a new one if the ID is 0.
// It only updates changed fields for existing records.
func Save[T Model](ctx context.Context, model *T) error {
	// If ID is 0 or unset, treat as Create
	if (*model).GetID() == 0 {
		// Mark as new before calling Create using reflection
		if bm := getBaseModelPtr(model); bm != nil {
			bm.SetNewRecordFlag(true)
		}
		return Create(ctx, model)
	}

	// --- Handle Update ---
	db := GetDBAdapter()
	cache := GetCacheClient()
	if db == nil || cache == nil {
		return errors.New("database or cache client not configured")
	}

	modelValue := reflect.ValueOf(model).Elem()
	modelType := modelValue.Type()

	// Get model metadata from cache
	info, err := getCachedModelInfo(modelType)
	if err != nil {
		return fmt.Errorf("could not get model info for %s: %w", modelType.Name(), err)
	}

	// 1. Fetch original record (to find changed fields)
	originalModel, err := ByID[T](ctx, (*model).GetID())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("cannot save model %s with ID %d: %w", info.tableName, (*model).GetID(), ErrNotFound)
		}
		return fmt.Errorf("could not fetch original model for comparison: %w", err)
	}

	// 2. Find changed fields
	changedFields, err := findChangedFields(*originalModel, *model, info)
	if err != nil {
		return fmt.Errorf("error comparing fields for save: %w", err)
	}

	if len(changedFields) == 0 {
		log.Printf("No changes detected for %s %d. Skipping save.", info.tableName, (*model).GetID())
		// Still inject adapters using reflection
		if bm := getBaseModelPtr(model); bm != nil {
			bm.SetAdapter(db, cache)
		}
		return nil // Nothing to save
	}

	// Add UpdatedAt timestamp to changed fields
	now := time.Now()
	setUpdatedAtTimestamp(model, now)
	// Use cached info to get the correct column name for UpdatedAt
	if updatedAtCol, ok := info.fieldToColumnMap["UpdatedAt"]; ok {
		changedFields[updatedAtCol] = now
	} else {
		log.Printf("Warning: UpdatedAt field not found in cached metadata for %s", modelType.Name())
	}

	// 3. Prepare Update SQL
	sqlStmt, values, err := buildUpdateSQL(info, changedFields)
	if err != nil {
		return fmt.Errorf("could not build update SQL: %w", err)
	}
	values = append(values, (*model).GetID()) // Add ID for WHERE clause

	// Trigger BeforeSave event
	err = triggerEvent(ctx, EventTypeBeforeSave, interface{}(model), changedFields) // Convert to interface{}
	if err != nil {
		return err
	}

	// 4. Execute Update
	lockKey := fmt.Sprintf("lock:%s:%d", info.tableName, (*model).GetID())
	err = withLock(ctx, lockKey, func(lctx context.Context) error {
		log.Printf("Executing DB Update for %s %d: %s | Args: %v", info.tableName, (*model).GetID(), sqlStmt, values)
		_, execErr := db.Exec(lctx, sqlStmt, values...)
		if execErr != nil {
			log.Printf("Database error updating %s %d: %v", info.tableName, (*model).GetID(), execErr)
			return fmt.Errorf("database error updating %s %d: %w", info.tableName, (*model).GetID(), execErr)
		}
		return nil
	})

	if err != nil {
		return err // Error during lock acquisition or DB execution
	}

	// Mark as not new and inject clients using reflection
	if bm := getBaseModelPtr(model); bm != nil {
		bm.SetNewRecordFlag(false)
		bm.SetAdapter(db, cache)
	}

	// Trigger AfterSave event
	err = triggerEvent(ctx, EventTypeAfterSave, interface{}(model), changedFields) // Convert to interface{}
	if err != nil {
		log.Printf("Error executing AfterSave listener for %s %d: %v", info.tableName, (*model).GetID(), err)
	}

	// 5. Invalidate Cache
	cacheKey := generateCacheKey(info.tableName, (*model).GetID())
	log.Printf("Deleting cache after save for %s", cacheKey)
	_ = cache.DeleteModel(ctx, cacheKey)

	// TODO: More sophisticated query cache invalidation needed here as well.

	log.Printf("Successfully saved %s %d", info.tableName, (*model).GetID())
	return nil
}

// findChangedFields compares two models and returns a map of changed fields.
// Keys are DB column names, values are the *new* values from the `updated` model.
// Uses cached model info for mapping.
func findChangedFields[T interface{ Model }](original T, updated T, info *modelInfo) (map[string]interface{}, error) {
	changes := make(map[string]interface{})
	originalVal := reflect.ValueOf(original)
	updatedVal := reflect.ValueOf(updated)

	if originalVal.Kind() == reflect.Ptr {
		originalVal = originalVal.Elem()
	}
	if updatedVal.Kind() == reflect.Ptr {
		updatedVal = updatedVal.Elem()
	}

	if originalVal.Type() != updatedVal.Type() {
		return nil, fmt.Errorf("type mismatch between original (%s) and updated (%s) models", originalVal.Type(), updatedVal.Type())
	}

	// Iterate through the fields defined in the cached metadata
	for _, fieldName := range info.fields {
		originalField := originalVal.FieldByName(fieldName)
		updatedField := updatedVal.FieldByName(fieldName)

		if !originalField.IsValid() || !updatedField.IsValid() {
			log.Printf("Warning: Field %s not found during comparison in findChangedFields for type %s", fieldName, info.tableName)
			continue // Should not happen if info is correct
		}

		if !originalField.CanInterface() || !updatedField.CanInterface() {
			continue // Skip unexported
		}

		dbColumnName, ok := info.fieldToColumnMap[fieldName]
		if !ok || dbColumnName == info.pkName { // Skip PK and fields not mapped
			continue
		}

		// Use DeepEqual for robust comparison, especially for slices/maps/structs.
		if !reflect.DeepEqual(originalField.Interface(), updatedField.Interface()) {
			changes[dbColumnName] = updatedField.Interface()
		}
	}

	return changes, nil
}

// setCreatedAtTimestamp sets the CreatedAt field using reflection.
func setCreatedAtTimestamp(modelPtr interface{}, now time.Time) {
	v := reflect.ValueOf(modelPtr)
	if v.Kind() != reflect.Ptr {
		return
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}
	createdAtField := v.FieldByName("CreatedAt") // Assumes field name
	if createdAtField.IsValid() && createdAtField.CanSet() && createdAtField.Type() == reflect.TypeOf(time.Time{}) {
		createdAtField.Set(reflect.ValueOf(now))
	}
}

// setUpdatedAtTimestamp sets the UpdatedAt field using reflection.
func setUpdatedAtTimestamp(modelPtr interface{}, now time.Time) {
	v := reflect.ValueOf(modelPtr)
	if v.Kind() != reflect.Ptr {
		return
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}
	updatedAtField := v.FieldByName("UpdatedAt") // Assumes field name
	if updatedAtField.IsValid() && updatedAtField.CanSet() && updatedAtField.Type() == reflect.TypeOf(time.Time{}) {
		updatedAtField.Set(reflect.ValueOf(now))
	}
}

// Delete removes a record from the database by its primary key
// and invalidates the corresponding cache entry.
func Delete[T Model](ctx context.Context, id int64) error {
	db := GetDBAdapter()
	cache := GetCacheClient()
	if db == nil || cache == nil {
		return errors.New("database or cache client not configured")
	}

	// Need model info for table name, pk name
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	info, err := getCachedModelInfo(modelType)
	if err != nil {
		return fmt.Errorf("could not get model info for delete: %w", err)
	}

	// Fetch the model before deleting to trigger events with the instance?
	modelToDelete, err := ByID[T](ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			log.Printf("Attempted to delete non-existent record %s %d", info.tableName, id)
			return nil // Be idempotent
		}
		return fmt.Errorf("failed to fetch record before delete %s %d: %w", info.tableName, id, err)
	}

	// Trigger BeforeDelete event
	err = triggerEvent(ctx, EventTypeBeforeDelete, interface{}(modelToDelete), nil) // Convert to interface{}
	if err != nil {
		return err
	}

	// Prepare Delete SQL
	sqlStmt := buildDeleteSQL(info) // Use cached info

	// Use locking
	lockKey := fmt.Sprintf("lock:%s:%d", info.tableName, id)
	err = withLock(ctx, lockKey, func(lctx context.Context) error {
		log.Printf("Executing DB Delete for %s %d: %s", info.tableName, id, sqlStmt)
		result, execErr := db.Exec(lctx, sqlStmt, id)
		if execErr != nil {
			log.Printf("Database error deleting %s %d: %v", info.tableName, id, execErr)
			return fmt.Errorf("database error deleting %s %d: %w", info.tableName, id, execErr)
		}

		// Check rows affected
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			log.Printf("Delete affected 0 rows for %s %d (already deleted?)", info.tableName, id)
		}
		return nil
	})

	if err != nil {
		return err // Error from lock or DB execution
	}

	// Trigger AfterDelete event
	err = triggerEvent(ctx, EventTypeAfterDelete, interface{}(modelToDelete), nil) // Convert to interface{}
	if err != nil {
		log.Printf("Error executing AfterDelete listener for %s %d: %v", info.tableName, id, err)
	}

	// Invalidate Cache
	cacheKey := generateCacheKey(info.tableName, id)
	log.Printf("Deleting cache after delete for %s", cacheKey)
	_ = cache.DeleteModel(ctx, cacheKey)

	// TODO: Query cache invalidation.

	log.Printf("Successfully deleted %s %d", info.tableName, id)
	return nil
}

// --- Querying ---

// QueryParams defines parameters for list queries.
type QueryParams struct {
	Where string        // Raw WHERE clause (e.g., "status = ? AND name LIKE ?")
	Args  []interface{} // Arguments for the WHERE clause placeholders
	Order string        // Raw ORDER BY clause (e.g., "created_at DESC")
	Start int           // Offset (for pagination)
	Limit int           // Limit (for pagination)
}

// IDs executes a query and returns only the primary key IDs of the matching records.
// Results can be cached.
func IDs[T Model](ctx context.Context, params QueryParams) ([]int64, error) {
	db := GetDBAdapter()
	cache := GetCacheClient()
	if db == nil || cache == nil {
		return nil, errors.New("database or cache client not configured")
	}

	// Need model info for table name, pk name
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	info, err := getCachedModelInfo(modelType)
	if err != nil {
		return nil, fmt.Errorf("could not get model info for IDs query: %w", err)
	}

	// Generate cache key based on query params
	queryCacheKey, err := generateQueryCacheKey(info.tableName, params)
	if err != nil {
		log.Printf("Error generating query cache key: %v", err)
	} else {
		// 1. Check query cache
		cachedIDs, err := cache.GetQueryIDs(ctx, queryCacheKey)
		if err == nil {
			log.Printf("Query cache hit for key: %s", queryCacheKey)
			return cachedIDs, nil
		} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, RedisNilError) {
			log.Printf("Query cache error for key %s: %v", queryCacheKey, err)
		} else {
			log.Printf("Query cache miss for key: %s", queryCacheKey)
		}
	}

	// 2. Database Query
	sqlStmt, args := buildSelectIDsSQL(info, params) // Use cached info

	log.Printf("Executing DB IDs query for %s: %s | Args: %v", info.tableName, sqlStmt, args)

	var ids []int64
	err = db.Select(ctx, &ids, sqlStmt, args...) // Pass pointer to slice
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("No IDs found in DB for query: %s", sqlStmt)
			if queryCacheKey != "" {
				_ = cache.SetQueryIDs(ctx, queryCacheKey, []int64{}, NoneResultCacheDuration)
			}
			return []int64{}, nil
		}
		log.Printf("Database error fetching IDs for %s: %v", info.tableName, err)
		return nil, fmt.Errorf("database error fetching IDs for %s: %w", info.tableName, err)
	}

	// 3. Cache Result
	if queryCacheKey != "" {
		log.Printf("Caching query IDs for key: %s", queryCacheKey)
		_ = cache.SetQueryIDs(ctx, queryCacheKey, ids, DefaultCacheDuration)
	}

	return ids, nil
}

// CachedResult holds the results (IDs) of a query, allowing deferred fetching.
type CachedResult[T Model] struct {
	ids    []int64
	params QueryParams // Store original params for context
}

// Fetch retrieves the full model objects corresponding to the IDs in the CachedResult.
// It leverages the ByID function, which uses caching.
func (cr *CachedResult[T]) Fetch(ctx context.Context) ([]*T, error) {
	if len(cr.ids) == 0 {
		return []*T{}, nil
	}

	results := make([]*T, 0, len(cr.ids))
	var errs []error // Collect errors for individual fetches

	// TODO: Potential optimization: Fetch multiple IDs concurrently?
	// Requires careful context management and error aggregation.
	for _, id := range cr.ids {
		model, err := ByID[T](ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				// Log inconsistency: ID was in the list but now not found?
				log.Printf("Warning: Record %d not found during CachedResult.Fetch, though present in ID list", id)
				continue // Skip this one
			} else {
				// Collect other errors
				err = fmt.Errorf("failed to fetch model %d: %w", id, err)
			}
		} else {
			results = append(results, model)
		}
	}

	// Return collected errors if any
	if len(errs) > 0 {
		// Combine errors (e.g., using multierr package or just the first one)
		return results, fmt.Errorf("encountered %d error(s) during fetch: %w", len(errs), errs[0])
	}

	return results, nil
}

// IDs returns the raw IDs held by the CachedResult.
func (cr *CachedResult[T]) IDs() []int64 {
	return cr.ids
}

// Query executes a query and returns a CachedResult containing the IDs,
// allowing for deferred fetching of the full objects.
func Query[T Model](ctx context.Context, params QueryParams) (*CachedResult[T], error) {
	// Leverage the IDs function which handles caching
	ids, err := IDs[T](ctx, params)
	if err != nil {
		return nil, err
	}

	result := &CachedResult[T]{
		ids:    ids,
		params: params,
	}

	return result, nil
}

// --- SQL Builder Helpers (Using Cached Metadata) ---

// buildSelectSQL constructs a SELECT statement for fetching a single record by ID.
func buildSelectSQL(info *modelInfo) string {
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
			return nil, fmt.Errorf("column '%s' not found in cached metadata for type %s", colName, v.Type().Name())
		}
		fieldVal := v.FieldByName(fieldName)
		if !fieldVal.IsValid() {
			return nil, fmt.Errorf("field '%s' (for column '%s') not found in struct %s", fieldName, colName, v.Type().Name())
		}
		if !fieldVal.CanInterface() {
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
		if col != info.pkName { // Exclude primary key column
			insertCols = append(insertCols, col)
			placeholders = append(placeholders, "?")
		}
	}

	sqlStmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		info.tableName,
		strings.Join(insertCols, ", "),
		strings.Join(placeholders, ", "),
	)
	return sqlStmt, insertCols // Return the columns actually used
}

// buildUpdateSQL constructs an UPDATE statement based on changed fields.
func buildUpdateSQL(info *modelInfo, changedFields map[string]interface{}) (string, []interface{}, error) {
	if len(changedFields) == 0 {
		return "", nil, errors.New("no fields provided for update")
	}

	setClauses := make([]string, 0, len(changedFields))
	values := make([]interface{}, 0, len(changedFields))

	// Ensure consistent order for testing/determinism if needed, though not strictly required by SQL
	// Iterate over cached columns to maintain order
	for _, colName := range info.columns {
		if value, ok := changedFields[colName]; ok {
			if colName == info.pkName {
				continue // Should not update PK
			}
			setClauses = append(setClauses, fmt.Sprintf("%s = ?", colName))
			values = append(values, value)
		}
	}

	if len(setClauses) == 0 {
		return "", nil, errors.New("no updatable fields provided (only PK change attempted or non-model fields?) V2")
	}

	sqlStmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s = ?",
		info.tableName,
		strings.Join(setClauses, ", "),
		info.pkName,
	)

	return sqlStmt, values, nil
}

// buildDeleteSQL constructs a DELETE statement.
func buildDeleteSQL(info *modelInfo) string {
	return fmt.Sprintf("DELETE FROM %s WHERE %s = ?", info.tableName, info.pkName)
}

// buildSelectIDsSQL constructs a SELECT statement to fetch only primary key IDs.
func buildSelectIDsSQL(info *modelInfo, params QueryParams) (string, []interface{}) {
	var query strings.Builder
	args := []interface{}{}

	query.WriteString(fmt.Sprintf("SELECT %s FROM %s", info.pkName, info.tableName))

	if params.Where != "" {
		query.WriteString(" WHERE ")
		query.WriteString(params.Where)
		args = append(args, params.Args...)
	}

	if params.Order != "" {
		query.WriteString(" ORDER BY ")
		query.WriteString(params.Order)
	}

	// Add LIMIT/OFFSET (syntax varies between DBs)
	if params.Limit > 0 {
		query.WriteString(" LIMIT ?")
		args = append(args, params.Limit)
	}
	if params.Start > 0 {
		query.WriteString(" OFFSET ?")
		args = append(args, params.Start)
	}

	return query.String(), args
}

// generateQueryCacheKey generates a cache key for a query based on table name and params.
func generateQueryCacheKey(tableName string, params QueryParams) (string, error) {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query params for cache key: %w", err)
	}

	hasher := sha256.New()
	hasher.Write([]byte(tableName))
	hasher.Write(paramsBytes)
	hash := hex.EncodeToString(hasher.Sum(nil))

	return fmt.Sprintf("query:%s:%s", tableName, hash), nil
}
