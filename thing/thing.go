package thing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	// We'll need a Redis client library. Using go-redis as an example.
	// "github.com/redis/go-redis/v9"
	"crypto/sha256"
	"encoding/hex"
	"log" // For placeholder logging
)

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
	return defaultDbAdapter
}

// GetCacheClient returns the configured default Cache client.
func GetCacheClient() CacheClient {
	clientMutex.RLock()
	defer clientMutex.RUnlock()
	return defaultCacheClient
}

// --- BaseModel Struct ---

// BaseModel provides common fields and functionality for database models.
// It should be embedded into specific model structs.
type BaseModel struct {
	ID        int64     `json:"id" db:"id"`                 // Added db tag
	CreatedAt time.Time `json:"created_at" db:"created_at"` // Added db tag
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"` // Added db tag

	// --- Internal ORM state ---
	// We embed the clients here so instance methods can use them.
	// These fields should be populated by the ORM functions (ByID, Create, etc.).
	// They are NOT saved to the DB or cache.
	dbAdapter   DBAdapter   `json:"-" db:"-"`
	cacheClient CacheClient `json:"-" db:"-"`
	isNewRecord bool        `json:"-" db:"-"` // Flag to indicate if the record is new

	// Removed redundant placeholders:
	// db    DBExecutor  `json:"-"`
	// cache RedisClient `json:"-"`
}

// SetAdapter sets the DB and Cache clients for this model instance.
// This is primarily for internal ORM use (e.g., after fetching).
func (b *BaseModel) SetAdapter(db DBAdapter, cache CacheClient) {
	b.dbAdapter = db
	b.cacheClient = cache
}

// --- Helper Functions ---

// generateCacheKey creates a Redis key string.
// Adapt this based on your actual model interface and naming conventions.
func generateCacheKey(tableName string, id int64) string {
	// Mimics the PHP logic: prefix (table name) : id
	return fmt.Sprintf("%s:%d", tableName, id)
}

// packData serializes model data for caching. Uses JSON here.
func packData(data interface{}) (string, error) {
	// PHP version used gzcompress + json_encode.
	// Go version: Just JSON for simplicity. Add compression if needed.
	packed, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to pack data for cache: %w", err)
	}
	return string(packed), nil
}

// unpackData deserializes cached data into the target model struct.
func unpackData(packed string, target interface{}) error {
	// PHP version used json_decode + gzuncompress.
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

// acquireLock attempts to acquire a distributed lock using CacheClient.
func acquireLock(ctx context.Context, lockKey string) (bool, error) {
	cache := GetCacheClient()
	if cache == nil {
		return false, errors.New("cache client not configured for locking")
	}
	// Use the interface method
	acquired, err := cache.AcquireLock(ctx, lockKey, LockDuration)
	if err != nil {
		log.Printf("Error acquiring lock '%s': %v", lockKey, err)
		return false, fmt.Errorf("failed to attempt lock acquisition: %w", err)
	}
	return acquired, nil
}

// releaseLock releases the distributed lock.
func releaseLock(ctx context.Context, lockKey string) {
	cache := GetCacheClient()
	if cache == nil {
		log.Printf("Warning: Cannot release lock '%s', cache client not configured", lockKey)
		return
	}
	// Use the interface method
	err := cache.ReleaseLock(ctx, lockKey)
	if err != nil {
		log.Printf("Warning: Failed to release lock '%s': %v", lockKey, err)
	}
}

// withLock encapsulates the acquire/retry/release logic for distributed locks.
func withLock(ctx context.Context, lockKey string, action func(ctx context.Context) error) error {
	acquired := false
	var lockErr error
	for i := 0; i < LockMaxRetries; i++ {
		acquired, lockErr = acquireLock(ctx, lockKey)
		if lockErr != nil {
			return lockErr // Error during acquisition attempt
		}
		if acquired {
			log.Printf("Acquired lock for %s", lockKey)
			defer releaseLock(ctx, lockKey)
			// Execute the action within the lock
			return action(ctx)
		}
		// Wait before retrying
		if i < LockMaxRetries-1 { // Don't sleep after the last attempt
			time.Sleep(LockRetryDelay)
		}
	}

	// If loop finishes without acquiring
	log.Printf("Failed to acquire lock for %s after %d retries", lockKey, LockMaxRetries)
	return ErrLockNotAcquired
}

// --- Helper: Get BaseModel field ---
func getBaseModelField(modelPtr interface{}) (*BaseModel, error) {
	val := reflect.ValueOf(modelPtr)
	if val.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("getBaseModelField requires a pointer to a struct, got %T", modelPtr)
	}
	val = val.Elem()
	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("getBaseModelField requires a pointer to a struct, got pointer to %s", val.Kind())
	}

	bmField := val.FieldByName("BaseModel")
	if !bmField.IsValid() {
		return nil, fmt.Errorf("struct %T does not have an embedded BaseModel field", modelPtr)
	}
	if !bmField.CanAddr() {
		return nil, fmt.Errorf("cannot get address of BaseModel field in %T", modelPtr)
	}

	if baseModel, ok := bmField.Addr().Interface().(*BaseModel); ok {
		return baseModel, nil
	}
	return nil, fmt.Errorf("embedded BaseModel field in %T is not of type *BaseModel", modelPtr)
}

// --- ByID Implementation ---
// Returns *T for idiomatic Go (can return nil if not found)
func ByID[T Model](ctx context.Context, id int64) (*T, error) {
	// Get configured clients
	db := GetDBAdapter()
	cache := GetCacheClient()
	if db == nil || cache == nil {
		return nil, errors.New("internal configuration error: DB or Cache client not configured")
	}

	// Determine TableName and Type using reflection on type T
	var typeHelper T
	modelType := reflect.TypeOf(typeHelper)
	var instanceForTable T
	var tableName string
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()                              // Get the struct type if T is a pointer
		instanceForTable = reflect.New(modelType).Interface().(T) // Create *Struct
		tableName = instanceForTable.TableName()
	} else {
		instanceForTable = reflect.New(modelType).Interface().(T) // Create *Struct
		tableName = instanceForTable.TableName()
	}
	if tableName == "" {
		return nil, fmt.Errorf("internal error: could not determine table name for type %T", typeHelper)
	}

	cacheKey := generateCacheKey(tableName, id)

	// Create a new instance of *T to store the result
	model := reflect.New(modelType).Interface().(*T) // Create *Struct

	// 2. Check Cache
	cacheErr := cache.GetModel(ctx, cacheKey, *model) // Pass *model (*T) which implements Model

	if cacheErr == nil {
		log.Printf("Cache hit for key: %s", cacheKey)
		// Inject adapters using reflection helper
		if bm, err := getBaseModelField(model); err == nil {
			bm.SetAdapter(db, cache)
			bm.isNewRecord = false
		} else {
			log.Printf("Warning: could not get BaseModel field for injection on cache hit: %v", err)
		}
		return model, nil
	}

	// Handle cache errors / miss
	if !errors.Is(cacheErr, ErrNotFound) {
		log.Printf("Warning: CacheClient error getting key %s: %v. Proceeding to database.", cacheKey, cacheErr)
	} else {
		log.Printf("Cache miss for key: %s", cacheKey)
	}

	// 6. Query Database using DBAdapter and SQL Builder
	log.Printf("Querying database for %s with ID %d", tableName, id)
	pkName := "id" // Assuming primary key is 'id'
	query := buildSelectSQL(tableName, modelType, pkName)

	// Use the DBAdapter's Get method
	dbErr := db.Get(ctx, *model, query, id) // Pass *model (*T) which implements Model

	if dbErr != nil {
		if errors.Is(dbErr, ErrNotFound) { // Check for ErrNotFound from adapter
			log.Printf("Database query returned no rows for %s:%d", tableName, id)
			// TODO: Cache NoneResult concept?
			return nil, ErrNotFound // Return nil for not found
		}
		log.Printf("Error querying database for %s:%d: %v", tableName, id, dbErr)
		return nil, fmt.Errorf("database query failed: %w", dbErr)
	}

	// 9. Cache the fetched model
	cacheSetErr := cache.SetModel(ctx, cacheKey, *model, DefaultCacheDuration)
	if cacheSetErr != nil {
		log.Printf("Warning: Failed to cache data for key %s after DB query: %v", cacheKey, cacheSetErr)
	}

	// Inject adapters using reflection helper
	if bm, err := getBaseModelField(model); err == nil {
		bm.SetAdapter(db, cache)
		bm.isNewRecord = false
	} else {
		log.Printf("Warning: could not get BaseModel field for injection on DB fetch: %v", err)
	}

	return model, nil
}

// Create inserts a new model record into the database and updates the cache.
func Create[T interface{ Model }](ctx context.Context, model *T) error { // Model is *T
	db := GetDBAdapter()
	cache := GetCacheClient()
	if db == nil || cache == nil {
		return errors.New("internal configuration error: DBAdapter or CacheClient not configured")
	}

	// Set timestamps & isNewRecord flag using reflection helper
	now := time.Now()
	bm, err := getBaseModelField(model)
	if err != nil {
		return fmt.Errorf("could not get BaseModel field for create: %w", err)
	}
	// Only set timestamps if they are zero values
	if bm.CreatedAt.IsZero() {
		bm.CreatedAt = now
	}
	if bm.UpdatedAt.IsZero() {
		bm.UpdatedAt = now
	}
	bm.isNewRecord = true
	bm.SetAdapter(db, cache) // Inject adapters

	// Trigger BeforeCreate event
	if err := triggerEvent(ctx, EventTypeBeforeCreate, *model, nil); err != nil { // Pass *model (T) which implements Model
		return fmt.Errorf("BeforeCreate event listener failed: %w", err)
	}

	// --- Database Insert ---
	tableName := (*model).TableName()
	pkName := "id" // Assuming PK is id
	modelType := reflect.TypeOf(*model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	query, columns := buildInsertSQL(tableName, modelType, pkName)
	args, err := getValues(model, columns)
	if err != nil {
		return fmt.Errorf("failed to get values for insert: %w", err)
	}

	log.Printf("Executing Create: %s with args: %v", query, args)
	result, err := db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to execute create statement for %s: %w", tableName, err)
	}
	insertedID, err := result.LastInsertId()
	if err != nil {
		log.Printf("Warning: Could not get LastInsertId for %s after insert: %v. ID might not be set.", tableName, err)
		// Depending on DB/Driver, might need RETURNING clause or explicit select after insert.
	} else {
		(*model).SetID(insertedID)
		log.Printf("Assigned ID %d to new %s", insertedID, tableName)
	}
	// --- End Insert ---

	bm.isNewRecord = false // Mark as not new

	// 3. Cache the newly created record
	if id := (*model).GetID(); id != 0 { // Only cache if we got an ID
		cacheKey := generateCacheKey(tableName, id)
		// Pass *model (*T) which implements Model
		err = cache.SetModel(ctx, cacheKey, *model, DefaultCacheDuration)
		if err != nil {
			log.Printf("Warning: Failed to cache new record %s(%d): %v", tableName, id, err)
		}
	} else {
		log.Printf("Warning: Cannot cache record %s because ID is zero.", tableName)
	}

	// Trigger AfterCreate event
	if err := triggerEvent(ctx, EventTypeAfterCreate, *model, nil); err != nil { // Pass *model (T)
		log.Printf("Warning: AfterCreate event listener failed for %s(%d): %v", tableName, (*model).GetID(), err)
	}

	return nil // Create succeeded
}

// --- Save Implementation ---
func Save[T interface{ Model }](ctx context.Context, model *T) error {
	db := GetDBAdapter()
	cache := GetCacheClient()
	if db == nil || cache == nil {
		return errors.New("internal configuration error: DBAdapter or CacheClient not configured")
	}

	// Ensure adapters are set via reflection helper
	bm, err := getBaseModelField(model)
	if err != nil {
		return fmt.Errorf("could not get BaseModel field for save: %w", err)
	}
	bm.SetAdapter(db, cache) // Ensure adapters are (re)injected

	id := (*model).GetID()
	if id == 0 {
		// If ID is 0, maybe we should call Create instead?
		// For now, enforce Save is only for existing records.
		return errors.New("cannot save model without an ID; use Create for new records")
		// Alternatively: return Create(ctx, model)
	}

	tableName := (*model).TableName()
	pkName := "id" // Assuming PK is id
	cacheKey := generateCacheKey(tableName, id)
	lockKey := cacheKey + ":lock"

	var changedFields map[string]interface{} // To store changes for AfterSave event
	var saveErr error

	action := func(lockedCtx context.Context) error {
		// 1. Fetch original state using ByID (handles cache/DB)
		original, fetchErr := ByID[T](lockedCtx, id)
		if fetchErr != nil {
			if errors.Is(fetchErr, ErrNotFound) {
				return fmt.Errorf("cannot save model %s(%d): original record not found", tableName, id)
			}
			return fmt.Errorf("failed to fetch original state for save %s(%d): %w", tableName, id, fetchErr)
		}

		// 2. Compare original (*T) and current (*T) state to find changes
		changes := make(map[string]interface{})
		originalValue := reflect.ValueOf(original).Elem()
		updatedValue := reflect.ValueOf(model).Elem()
		modelType := updatedValue.Type() // Get type from the concrete value

		for i := 0; i < updatedValue.NumField(); i++ {
			fieldInfo := modelType.Field(i)
			fieldName := fieldInfo.Name
			if fieldName == "BaseModel" {
				continue
			} // Skip embedded

			updatedField := updatedValue.Field(i)
			originalField := originalValue.FieldByName(fieldName)
			if !originalField.IsValid() {
				continue
			}

			// Use DeepEqual for comparison, handles more types
			if !reflect.DeepEqual(updatedField.Interface(), originalField.Interface()) {
				dbTag := fieldInfo.Tag.Get("db")
				if dbTag != "" && dbTag != "-" {
					changes[dbTag] = updatedField.Interface()
				} else if dbTag == "" {
					// Optional: default to field name if no db tag (maybe convert to snake_case)
					// changes[fieldName] = updatedField.Interface()
				}
			}
		}
		changedFields = changes // Assign to outer scope var for AfterSave event

		if len(changedFields) == 0 {
			log.Printf("No changes detected for %s(%d), skipping save.", tableName, id)
			return nil // Nothing to save
		}
		log.Printf("Detected changes for %s(%d): %v", tableName, id, changedFields)

		// 3. Set UpdatedAt timestamp
		now := time.Now()
		bmSave, _ := getBaseModelField(model) // Ignore error, checked above
		bmSave.UpdatedAt = now
		// Ensure updated_at is included in the update, using tag from BaseModel if possible
		uaColName := "updated_at" // Default
		if bmFieldType, ok := reflect.TypeOf(BaseModel{}).FieldByName("UpdatedAt"); ok {
			tag := bmFieldType.Tag.Get("db")
			if tag != "" && tag != "-" {
				uaColName = tag
			}
		}
		changedFields[uaColName] = now

		// Trigger BeforeSave event
		if err := triggerEvent(lockedCtx, EventTypeBeforeSave, *model, changedFields); err != nil {
			return fmt.Errorf("BeforeSave event listener failed: %w", err)
		}

		// 4. Perform partial database update using DBAdapter
		updateQuery, args, err := buildUpdateSQL(tableName, pkName, changedFields)
		if err != nil {
			return fmt.Errorf("failed to build update query: %w", err)
		}
		// Append the ID for the WHERE clause
		args = append(args, id)

		log.Printf("Executing Update: %s with args: %v", updateQuery, args)
		result, err := db.Exec(lockedCtx, updateQuery, args...)
		if err != nil {
			return fmt.Errorf("failed to execute update for %s(%d): %w", tableName, id, err)
		}
		rowsAffected, _ := result.RowsAffected()
		log.Printf("Update affected %d rows for %s(%d)", rowsAffected, tableName, id)
		// Optional: Check rowsAffected if needed (e.g., == 0 might mean record was deleted concurrently)

		// 5. Update the cache with the full new state
		// Pass *model (*T) which implements Model
		cacheErr := cache.SetModel(lockedCtx, cacheKey, *model, DefaultCacheDuration)
		if cacheErr != nil {
			// Log but don't fail the operation
			log.Printf("Warning: Failed to update cache after save for record %s(%d): %v", tableName, id, cacheErr)
		}

		return nil // Action completed successfully
	}

	saveErr = withLock(ctx, lockKey, action)

	if saveErr != nil {
		return saveErr // Return error from lock/action
	}

	// Trigger AfterSave event outside the lock
	// Pass the changedFields map (might be empty if save was skipped)
	if err := triggerEvent(ctx, EventTypeAfterSave, *model, changedFields); err != nil {
		log.Printf("Warning: AfterSave event listener failed for %s(%d): %v", (*model).TableName(), id, err)
	}

	return nil // Save operation successful (or skipped due to no changes)
}

// Helper function (needs implementation)
func findChangedFields[T interface{ Model }](original T, updated *T) map[string]interface{} {
	// Use reflection to compare fields of original and updated.Elem()
	// Return map[db_column_name]new_value for changed fields.
	log.Println("TODO: Implement findChangedFields using reflection")
	// Placeholder: assume 'Name' field changed if original had ID < 500
	changes := make(map[string]interface{})
	// Need to handle original potentially being zero value if fetched from DB and was nil?
	// Check if original is the zero value for its type
	var zeroOriginal T
	if !reflect.DeepEqual(original, zeroOriginal) && original.GetID() < 500 {
		nameField := reflect.ValueOf(updated).Elem().FieldByName("Name") // Assuming a 'Name' field exists
		if nameField.IsValid() {
			changes["name"] = nameField.Interface() // Assuming 'name' is the DB column
		}
	}
	return changes
}

// Helper function (needs implementation)
func setUpdatedAtTimestamp[T interface{ Model }](model *T, now time.Time) {
	// Use reflection or interface method to set UpdatedAt field
	log.Println("TODO: Implement setUpdatedAtTimestamp")
	modelValue := reflect.ValueOf(model).Elem()
	updatedAtField := modelValue.FieldByName("UpdatedAt")
	if updatedAtField.IsValid() && updatedAtField.CanSet() && updatedAtField.Type() == reflect.TypeOf(now) {
		updatedAtField.Set(reflect.ValueOf(now))
	}
}

// Helper function for lock retries
// func tryAcquireLockWithRetries(ctx context.Context, lockKey string) (bool, error) { ... } // Moved into withLock helper

// GetID returns the BaseModel's ID.
func (b *BaseModel) GetID() int64 {
	return b.ID
}

// SetID sets the BaseModel's ID.
func (b *BaseModel) SetID(id int64) {
	b.ID = id
}

// --- Delete Implementation ---

// Delete removes a model record from the database and clears its cache entry.
func Delete[T Model](ctx context.Context, id int64) error {
	db := GetDBAdapter()
	cache := GetCacheClient()
	if db == nil || cache == nil {
		return errors.New("internal configuration error: DBAdapter or CacheClient not configured for Delete")
	}

	// Need table name. Fetching is one way, but potentially inefficient.
	// Alternative: Require passing Model type information or using reflection.
	// Let's use reflection similar to ByID to get TableName from type T
	var instanceForTable T
	var tableName string
	modelType := reflect.TypeOf(instanceForTable)
	if modelType.Kind() == reflect.Ptr {
		elemType := modelType.Elem()
		newInstance := reflect.New(elemType).Interface().(T)
		tableName = newInstance.TableName()
	} else {
		tableName = instanceForTable.TableName()
	}
	if tableName == "" {
		log.Printf("Error: Could not determine table name for delete type %T", instanceForTable)
		return errors.New("internal error: could not determine table name for delete")
	}

	cacheKey := generateCacheKey(tableName, id)
	lockKey := cacheKey + ":lock"
	var model T // Needed for AfterDelete event - fetch inside lock?
	var deleteErr error

	action := func(lockedCtx context.Context) error {
		// Optional: Fetch model for Before/After hooks if needed
		// model, err := ByID[T](lockedCtx, id) ... handle error ...

		// Trigger BeforeDelete event (pass zero model if not fetched)
		if err := triggerEvent(lockedCtx, EventTypeBeforeDelete, model, nil); err != nil {
			return fmt.Errorf("BeforeDelete event listener failed: %w", err)
		}

		// --- Database Delete ---
		query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", tableName)
		result, err := db.Exec(lockedCtx, query, id)
		if err != nil {
			return fmt.Errorf("failed to execute delete for %s(%d): %w", tableName, id, err)
		}
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			log.Printf("Warning: Delete affected 0 rows for %s(%d), might indicate record didn't exist.", tableName, id)
			// Treat as success? Or return ErrNotFound? Let's treat as success for idempotency.
		}
		// --- End Delete ---

		// 2. Clear the cache entry
		err = cache.DeleteModel(lockedCtx, cacheKey)
		if err != nil {
			log.Printf("Warning: Failed to clear cache for deleted record %s(%d): %v", tableName, id, err)
		}

		return nil // Action completed successfully
	}

	deleteErr = withLock(ctx, lockKey, action)

	if deleteErr != nil {
		return deleteErr
	}

	// Trigger AfterDelete event outside the lock (pass zero model if not fetched)
	if err := triggerEvent(ctx, EventTypeAfterDelete, model, nil); err != nil {
		log.Printf("Warning: AfterDelete event listener failed for %s(%d): %v", tableName, id, err)
	}

	return nil
}

// --- IDs Implementation ---

// QueryParams defines parameters for the IDs query.
// Using a struct for better clarity than multiple arguments.
type QueryParams struct {
	Where string        // Raw WHERE clause (e.g., "status = ? AND name LIKE ?") - Be cautious about SQL injection if user-provided!
	Args  []interface{} // Arguments for the WHERE clause placeholders
	Order string        // Raw ORDER BY clause (e.g., "created_at DESC")
	Start int           // Offset (for pagination)
	Limit int           // Limit (for pagination)
}

// IDs retrieves a list of primary key IDs based on query parameters.
// Similar to the PHP version, but requires explicit query building.
func IDs[T Model](ctx context.Context, params QueryParams) ([]int64, error) {
	db := GetDBAdapter()
	if db == nil {
		log.Printf("Error: DBAdapter not configured for IDs")
		return nil, errors.New("internal configuration error: DBAdapter not configured")
	}

	// Get TableName using reflection on type T
	var instanceForTable T
	var tableName string
	modelType := reflect.TypeOf(instanceForTable)
	if modelType.Kind() == reflect.Ptr {
		elemType := modelType.Elem()
		newInstance := reflect.New(elemType).Interface().(T)
		tableName = newInstance.TableName()
	} else {
		tableName = instanceForTable.TableName()
	}
	if tableName == "" {
		log.Printf("Error: Could not determine table name for IDs type %T", instanceForTable)
		return nil, errors.New("internal error: could not determine table name for IDs")
	}

	pkName := "id" // Assuming PK is id
	// Build the query - TODO: Needs robust SQL builder
	query := fmt.Sprintf("SELECT %s FROM %s", pkName, tableName)
	if params.Where != "" {
		query += " WHERE " + params.Where
	}
	if params.Order != "" {
		query += " ORDER BY " + params.Order
	}
	if params.Limit > 0 {
		// NOTE: LIMIT/OFFSET syntax varies between DBs! Adapter needs to handle this.
		query += fmt.Sprintf(" LIMIT %d", params.Limit)
		if params.Start > 0 {
			query += fmt.Sprintf(" OFFSET %d", params.Start)
		}
	}

	log.Printf("Executing IDs query: %s with args: %v", query, params.Args)

	var ids []int64
	// Use the adapter's Select method. Note dest is a slice.
	err := db.Select(ctx, &ids, query, params.Args...)

	if err != nil {
		log.Printf("Error executing IDs query for %s: %v", tableName, err)
		return nil, fmt.Errorf("database IDs query failed: %w", err)
	}

	return ids, nil
}

// --- Query Implementation ---

// QueryCacheDuration defines the TTL for cached query results (ID lists).
const QueryCacheDuration = 5 * time.Minute // Adjust as needed

// CachedResult holds the results (list of IDs) from a Query execution.
// It allows fetching the actual objects on demand.
type CachedResult[T Model] struct {
	ids    []int64
	params QueryParams // Store original params for context
	// We might need a reference back to the db/cache clients or context here
	// if Fetch needs to be called much later, but let's assume ByID uses the global ones for now.
}

// Fetch retrieves the full model objects corresponding to the IDs stored in the CachedResult.
// It utilizes the ByID function, which handles individual object caching.
func (cr *CachedResult[T]) Fetch(ctx context.Context) ([]T, error) {
	if len(cr.ids) == 0 {
		return []T{}, nil
	}

	results := make([]T, 0, len(cr.ids))
	var firstErr error
	// TODO: Consider fetching ByID concurrently using goroutines for performance
	// Be mindful of potential DB connection pool limits if fetching many objects.
	for _, id := range cr.ids {
		model, err := ByID[T](ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				// Log this? It means an ID from a cached query result no longer exists.
				log.Printf("Warning: Record with ID %d from cached query result not found during Fetch.", id)
				continue // Skip this one
			}
			// For other errors, log and potentially stop or collect errors.
			log.Printf("Error fetching record with ID %d during CachedResult.Fetch: %v", id, err)
			if firstErr == nil { // Store the first error encountered
				firstErr = fmt.Errorf("failed to fetch record %d: %w", id, err)
			}
			continue // Optionally stop processing on error: return nil, firstErr
		}
		results = append(results, *model)
	}

	// Return partial results even if some errors occurred?
	// Or return nil, firstErr if any error happened?
	// Current behavior: return all successfully fetched items, and the first error encountered.
	return results, firstErr
}

// IDs returns the raw list of IDs from the cached result.
func (cr *CachedResult[T]) IDs() []int64 {
	return cr.ids
}

// generateQueryCacheKey creates a unique cache key based on the query parameters.
func generateQueryCacheKey[T Model](params QueryParams) (string, error) {
	var model T
	tableName := model.TableName()
	if tableName == "" {
		return "", errors.New("internal error: could not determine table name for query cache key")
	}

	// Create a stable representation of the query parameters.
	// Using JSON marshalling for simplicity, though more performant methods exist.
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query params for cache key: %w", err)
	}

	// Hash the parameters to keep the key length manageable.
	hasher := sha256.New()
	hasher.Write(paramsBytes)
	hash := hex.EncodeToString(hasher.Sum(nil))

	return fmt.Sprintf("query:%s:%s", tableName, hash), nil
}

// Query executes a query based on parameters, retrieves a list of IDs,
// caches the ID list, and returns a CachedResult object.
func Query[T Model](ctx context.Context, params QueryParams) (*CachedResult[T], error) {
	db := GetDBAdapter()
	cache := GetCacheClient()
	if db == nil || cache == nil {
		log.Printf("Error: DB or Cache client not configured for Query")
		return nil, errors.New("internal configuration error: DB or Cache client not configured")
	}

	// 1. Generate Cache Key
	queryCacheKey, keyErr := generateQueryCacheKey[T](params)
	if keyErr != nil {
		log.Printf("Error generating query cache key: %v", keyErr)
		return nil, keyErr
	}

	// 2. Check Cache for the ID list
	ids, cacheErr := cache.GetQueryIDs(ctx, queryCacheKey)

	if cacheErr == nil {
		log.Printf("Query cache hit for key: %s (%d IDs)", queryCacheKey, len(ids))
		return &CachedResult[T]{ids: ids, params: params}, nil
	} else if !errors.Is(cacheErr, ErrNotFound) {
		log.Printf("Warning: CacheClient error getting query key %s: %v. Proceeding to database.", queryCacheKey, cacheErr)
	} else {
		log.Printf("Query cache miss for key: %s", queryCacheKey)
	}

	// 3. Execute DB Query using IDs function
	dbIDs, dbErr := IDs[T](ctx, params)
	if dbErr != nil {
		log.Printf("Error executing IDs query during Query function: %v", dbErr)
		return nil, dbErr
	}
	ids = dbIDs // Assign result from DB

	log.Printf("Executed DB query for IDs, found %d IDs for params: %+v", len(ids), params)

	// 4. Cache the retrieved ID list
	setErr := cache.SetQueryIDs(ctx, queryCacheKey, ids, QueryCacheDuration)
	if setErr != nil {
		log.Printf("Warning: Failed to cache ID list for query key %s: %v", queryCacheKey, setErr)
	}

	// 5. Return CachedResult
	return &CachedResult[T]{ids: ids, params: params}, nil
}

// --- Instance Methods (Example: Save) ---
func (b *BaseModel) Save(ctx context.Context) error {
	if b.dbAdapter == nil || b.cacheClient == nil {
		// Inject if missing (assuming Configure was called)
		b.dbAdapter = GetDBAdapter()
		b.cacheClient = GetCacheClient()
		if b.dbAdapter == nil || b.cacheClient == nil {
			return errors.New("cannot save model: DBAdapter or CacheClient not configured or injected")
		}
	}

	// --- Reflection Magic ---
	// We need to get the pointer to the embedding struct (*User)
	// that contains this *BaseModel 'b'. This is the hard part.
	// Let's make the instance method call the package func *if* we had the embedding ptr.

	// Placeholder: Assume 'b' itself can be cast or used in a way
	// that represents the Model interface implementation of the embedding struct.
	// This requires careful interface design or more complex reflection.
	if modelInstance, ok := interface{}(b).(Model); ok {
		// THIS IS STILL WRONG: Save[T] needs the actual *T (*User), not just the Model interface.
		// We cannot easily get T here without passing it or using heavy reflection/unsafe.

		// Explicitly ignore the unused variable:
		_ = modelInstance

		// log.Printf("Calling package Save for type %T", b) // Log type of b (*BaseModel)
		// return Save[???](ctx, modelInstance) // CANNOT DETERMINE T

		return errors.New("instance Save method cannot determine generic type T - requires further implementation")
	} else {
		return errors.New("receiver does not fully implement Model for instance save")
	}
}

// --- SQL Generation Helpers (Basic) ---

// getColumns extracts db column names from struct tags or field names.
// Excludes the primary key by default.
func getColumns(modelType reflect.Type, includePK bool, pkName string) []string {
	cols := []string{}
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return cols // Or handle error
	}

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		if field.Name == "BaseModel" { // Recurse into embedded BaseModel for its fields
			baseCols := getColumns(field.Type, includePK, pkName)
			cols = append(cols, baseCols...)
			continue
		}

		dbTag := field.Tag.Get("db")
		if dbTag == "-" {
			continue
		} // Skip fields marked with db:"-"

		colName := dbTag
		if colName == "" {
			colName = field.Name // Default to field name if no tag (convert to snake_case?)
			// TODO: Implement snake_case conversion if desired
		}

		if !includePK && colName == pkName {
			continue // Skip primary key if requested
		}
		cols = append(cols, colName)
	}
	return cols
}

// buildSelectSQL generates a simple SELECT statement for fetching by primary key.
func buildSelectSQL(tableName string, modelType reflect.Type, pkName string) string {
	cols := getColumns(modelType, true, pkName) // Include PK for SELECT
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", strings.Join(cols, ", "), tableName, pkName)
}

// getValues extracts values from a model instance corresponding to columns.
// Ensures values are returned in the same order as the input columns slice.
func getValues(modelPtr interface{}, columns []string) ([]interface{}, error) {
	val := reflect.ValueOf(modelPtr)
	if val.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("getValues requires a pointer to a struct, got %T", modelPtr)
	}
	val = val.Elem()
	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("getValues requires a pointer to a struct, got pointer to %s", val.Kind())
	}

	// Map column names to their extracted values
	currentValuesMap := make(map[string]interface{})
	var mapValues func(v reflect.Value)
	mapValues = func(v reflect.Value) {
		if v.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			fieldVal := v.Field(i)
			if field.Name == "BaseModel" && field.Type == reflect.TypeOf(BaseModel{}) {
				mapValues(fieldVal) // Recurse
				continue
			}
			dbTag := field.Tag.Get("db")
			if dbTag == "-" {
				continue
			}
			colName := dbTag
			if colName == "" {
				colName = field.Name /* TODO: Snake case? */
			}
			// Check if this column is needed (optimization, avoids storing all fields)
			needed := false
			for _, requestedCol := range columns {
				if requestedCol == colName {
					needed = true
					break
				}
			}
			if needed {
				currentValuesMap[colName] = fieldVal.Interface()
			}
		}
	}
	mapValues(val)

	// Populate orderedValues based on the columns slice order
	orderedValues := make([]interface{}, len(columns))
	for i, colName := range columns {
		if val, ok := currentValuesMap[colName]; ok {
			orderedValues[i] = val
		} else {
			// This shouldn't happen if getColumns and getValues logic are consistent
			return nil, fmt.Errorf("value for column '%s' not found in model %T during getValues mapping", colName, modelPtr)
		}
	}

	return orderedValues, nil
}

// buildInsertSQL generates a standard INSERT statement.
func buildInsertSQL(tableName string, modelType reflect.Type, pkName string) (string, []string) {
	cols := getColumns(modelType, false, pkName) // Exclude PK for INSERT
	placeholders := make([]string, len(cols))
	for i := range cols {
		placeholders[i] = "?" // Assuming ? placeholders, adapter might need to adjust
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "))
	return query, cols
}

// buildUpdateSQL generates a partial UPDATE statement based on changed fields.
func buildUpdateSQL(tableName string, pkName string, changedFields map[string]interface{}) (string, []interface{}, error) {
	if len(changedFields) == 0 {
		return "", nil, errors.New("no fields provided for update")
	}

	setClauses := make([]string, 0, len(changedFields))
	args := make([]interface{}, 0, len(changedFields)+1) // +1 for the ID in WHERE

	// Ensure consistent order for query generation (maps are unordered)
	keys := make([]string, 0, len(changedFields))
	for k := range changedFields {
		keys = append(keys, k)
	}
	// Sort keys for deterministic query string (important for potential query caching/logging)
	// Use reflect or a stable sort if specific order matters beyond determinism
	// For now, default map iteration order is acceptable for correctness.
	// sort.Strings(keys) // Optional: Sort if needed

	for _, colName := range keys {
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", colName))
		args = append(args, changedFields[colName])
	}

	// Add the primary key value for the WHERE clause
	// NOTE: The actual ID value needs to be appended to args *after* this function call.
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = ?",
		tableName,
		strings.Join(setClauses, ", "),
		pkName)

	return query, args, nil
}
