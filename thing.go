package thing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log" // For placeholder logging
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

// --- Constants ---

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

const (
	// Lifecycle Events (Used by Event System)
	EventTypeBeforeSave   EventType = "BeforeSave"
	EventTypeAfterSave    EventType = "AfterSave"
	EventTypeBeforeCreate EventType = "BeforeCreate"
	EventTypeAfterCreate  EventType = "AfterCreate"
	EventTypeBeforeDelete EventType = "BeforeDelete"
	EventTypeAfterDelete  EventType = "AfterDelete"
)

// --- Errors ---

var (
	ErrNotFound        = errors.New("record not found")
	ErrLockNotAcquired = errors.New("could not acquire lock")
)

// --- Global Configuration ---

var (
	globalDB     DBAdapter   // Set via Configure
	globalCache  CacheClient // Set via Configure
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

// --- Thing Core Struct ---

// Thing is the central access point for ORM operations, analogous to gorm.DB.
// It holds database/cache clients and the context for operations.
type Thing[T any] struct {
	db    DBAdapter
	cache CacheClient
	ctx   context.Context
	info  *modelInfo // Pre-computed metadata for type T
}

// --- Thing Constructors & Accessors ---

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
	info, err := getCachedModelInfo(modelType) // Uses helper defined later
	if err != nil {
		return nil, fmt.Errorf("failed to get model info for type %s: %w", modelType.Name(), err)
	}
	// Attempt to determine table name here. Instance method might override later.
	info.tableName = getTableNameFromType(modelType) // Uses helper defined later
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

// --- Thing Public Methods ---

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

// ByID retrieves a single record of type T by its primary key.
func (t *Thing[T]) ByID(id int64) (*T, error) {
	instance := new(T)
	err := t.byIDInternal(t.ctx, id, instance) // Calls internal method
	if err != nil {
		return nil, err
	}
	return instance, nil
}

// Save updates an existing record or creates a new one if its primary key is zero
// or it's explicitly marked as new. Also updates timestamps.
func (t *Thing[T]) Save(value *T) error {
	// Delegate directly to the internal save logic
	return t.saveInternal(t.ctx, value) // Calls internal method
}

// Delete removes a record of type T.
func (t *Thing[T]) Delete(value *T) error {
	return t.deleteInternal(t.ctx, value) // Calls internal method
}

// Query executes a query based on QueryParams and returns results of type []*T.
// Note: This method currently passes its own context directly to internal methods.
// It could be refactored to use t.ctx like ByID/Save/Delete if desired.
func (t *Thing[T]) Query(params QueryParams) ([]*T, error) {
	return t.queryInternal(t.ctx, params) // Calls internal method
}

// IDs executes a query based on QueryParams and returns only the primary key IDs for type T.
// Note: This method currently passes its own context directly to internal methods.
// It could be refactored to use t.ctx like ByID/Save/Delete if desired.
func (t *Thing[T]) IDs(params QueryParams) ([]int64, error) {
	if t.info == nil { // Add check for safety
		return nil, errors.New("IDs: model info not available on Thing instance")
	}
	// Pass the pre-computed info from the Thing instance
	return t.idsInternal(t.ctx, t.info, params) // Calls internal method
}

// Load explicitly loads relationships for a given model instance using the Thing's context.
// model must be a pointer to a struct of type T.
// relations are the string names of the fields representing the relationships to load.
func (t *Thing[T]) Load(model *T, relations ...string) error {
	// Use the context stored in the Thing instance
	return t.loadInternal(t.ctx, model, relations...)
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

// --- BaseModel Methods ---

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

// IsNewRecord returns whether this is a new record.
func (b *BaseModel) IsNewRecord() bool {
	return b.isNewRecord
}

// SetNewRecordFlag sets the internal isNewRecord flag.
func (b *BaseModel) SetNewRecordFlag(isNew bool) {
	b.isNewRecord = isNew
}

// --- Internal `Thing` Methods ---

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

	cacheKey := generateCacheKey(t.info.tableName, id) // Uses helper defined later

	// 1. Check cache
	cacheStart := time.Now()
	cachedErr := t.cache.GetModel(ctx, cacheKey, dest)
	cacheDuration := time.Since(cacheStart)
	if cachedErr == nil {
		log.Printf("CACHE HIT: Key: %s (%s)", cacheKey, cacheDuration)
		// Successfully retrieved from cache, ensure internal flags are set correctly
		setNewRecordFlagIfBaseModel(dest, false) // Uses helper defined later
		return nil                               // Found in cache
	} else if !errors.Is(cachedErr, ErrNotFound) {
		// Log unexpected cache errors but proceed to DB
		log.Printf("WARN: Cache GetModel error for key %s: %v (%s)", cacheKey, cachedErr, cacheDuration)
	}
	// Log cache miss if it was ErrNotFound or another error occurred
	log.Printf("CACHE MISS: Key: %s (%s) - Error: %v", cacheKey, cacheDuration, cachedErr)

	// 2. Cache miss, query database
	selectSQL := buildSelectSQL(t.info) // Uses helper defined later
	query := fmt.Sprintf("%s WHERE %s = ? LIMIT 1", selectSQL, t.info.pkName)

	dbStart := time.Now()
	dbErr := t.db.Get(ctx, dest, query, id) // Use the DBAdapter's Get method
	dbDuration := time.Since(dbStart)

	if dbErr != nil {
		if errors.Is(dbErr, ErrNotFound) {
			log.Printf("DB MISS: Key: %s (%s)", cacheKey, dbDuration)
			// Optional: Cache the fact that the record doesn't exist (negative caching)
			// errCacheNone := t.cache.SetModel(ctx, cacheKey, &NoneResult, NoneResultCacheDuration)
			// if errCacheNone != nil { log.Printf("WARN: Failed to set negative cache for key %s: %v", cacheKey, errCacheNone) }
			return ErrNotFound // Return the standard not found error
		}
		log.Printf("DB ERROR: Key: %s (%s) - %v", cacheKey, dbDuration, dbErr)
		return fmt.Errorf("database query failed: %w", dbErr)
	}
	log.Printf("DB HIT: Key: %s (%s)", cacheKey, dbDuration)

	// 3. Found in DB, store in cache
	setNewRecordFlagIfBaseModel(dest, false) // Uses helper defined later
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
	if err := triggerEvent(ctx, EventTypeBeforeSave, value, nil); err != nil { // Uses helper defined later
		return fmt.Errorf("BeforeSave hook failed: %w", err)
	}

	baseModelPtr := getBaseModelPtr(value) // Uses helper defined later
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
		if err := triggerEvent(ctx, EventTypeBeforeCreate, value, nil); err != nil { // Uses helper defined later
			return fmt.Errorf("BeforeCreate hook failed: %w", err)
		}

		setCreatedAtTimestamp(value, now) // Uses helper defined later
		setUpdatedAtTimestamp(value, now) // Uses helper defined later

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
			log.Printf("WARN: Could not get LastInsertId after insert: %v", errId)
		} else {
			baseModelPtr.SetID(newID)
			// --- Cache Set on Create ---
			cacheKey := generateCacheKey(t.info.tableName, newID) // Uses helper defined later
			cacheSetStart := time.Now()
			errCacheSet := t.cache.SetModel(ctx, cacheKey, value, DefaultCacheDuration)
			cacheSetDuration := time.Since(cacheSetStart)
			if errCacheSet != nil {
				log.Printf("WARN: Failed to cache model after create for key %s: %v (%s)", cacheKey, errCacheSet, cacheSetDuration)
			}
			// --- End Cache Set ---
		}

		// --- Trigger AfterCreate hook ---
		if errHook := triggerEvent(ctx, EventTypeAfterCreate, value, nil); errHook != nil { // Uses helper defined later
			log.Printf("WARN: AfterCreate hook failed: %v", errHook)
		}

	} else {
		// --- UPDATE Path ---
		id := baseModelPtr.ID
		if id == 0 {
			return errors.New("cannot update record with zero ID")
		}

		// Need to fetch the original record to find changed fields
		original := reflect.New(modelValue.Elem().Type()).Interface().(*T)
		if errFetch := t.byIDInternal(ctx, id, original); errFetch != nil {
			return fmt.Errorf("failed to fetch original record for update (ID: %d): %w", id, errFetch)
		}

		// Find changed fields using reflection helper (keys are DB column names)
		changedFields, err = findChangedFieldsReflection(original, value, t.info) // Uses helper defined later
		if err != nil {
			return fmt.Errorf("failed to find changed fields for update: %w", err)
		}

		// If no fields changed (excluding UpdatedAt logic handled in findChangedFieldsReflection), skip DB update
		if len(changedFields) == 0 {
			log.Printf("Skipping update for %s %d: No relevant fields changed", t.info.tableName, id)
			if errHook := triggerEvent(ctx, EventTypeAfterSave, value, changedFields); errHook != nil { // Uses helper defined later
				log.Printf("WARN: AfterSave hook failed (no changes): %v", errHook)
			}
			baseModelPtr.SetNewRecordFlag(false) // Ensure flag is correct
			return nil
		}

		// Always update the UpdatedAt timestamp in the model
		setUpdatedAtTimestamp(value, now) // Uses helper defined later
		// Ensure updated_at is included in the map for the SQL query
		changedFields["updated_at"] = now

		// Build the UPDATE SQL
		setClauses := []string{}
		updateArgs := []interface{}{}

		// --- Iterate over the CHANGED DB columns ---
		changedCols := make([]string, 0, len(changedFields))
		for colName := range changedFields {
			changedCols = append(changedCols, colName)
		}
		sort.Strings(changedCols) // Sort for deterministic order

		for _, colName := range changedCols {
			if colName == t.info.pkName {
				log.Printf("WARN: Primary key column '%s' found in changedFields map during update. Skipping.", colName)
				continue
			}
			setClauses = append(setClauses, fmt.Sprintf("%s = ?", colName))
			updateArgs = append(updateArgs, changedFields[colName])
		}
		// --- End iteration over changed columns ---

		if len(setClauses) == 0 {
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
		err = withLock(ctx, t.cache, lockKey, func(lctx context.Context) error { // Uses helper defined later
			log.Printf("DB UPDATE: %s [%v]", query, args)
			result, err = t.db.Exec(lctx, query, args...)
			if err != nil {
				return fmt.Errorf("database update failed: %w", err)
			}

			rowsAffected, _ := result.RowsAffected()
			if rowsAffected == 0 {
				log.Printf("WARN: Update affected 0 rows for %s %d. Record might not exist?", t.info.tableName, id)
			}

			// Invalidate Object Cache within the lock, after successful DB update
			cacheKey := generateCacheKey(t.info.tableName, id) // Uses helper defined later
			cacheDelStart := time.Now()
			errCacheDel := t.cache.DeleteModel(lctx, cacheKey)
			cacheDelDuration := time.Since(cacheDelStart)
			if errCacheDel != nil {
				log.Printf("WARN: Failed to delete model from cache during update lock for key %s: %v (%s)", cacheKey, errCacheDel, cacheDelDuration)
			}

			// --- Query Cache Invalidation (Targeted: Delete queries containing this ID) ---
			queryPrefix := fmt.Sprintf("query:%s:", t.info.tableName)
			qcDelStart := time.Now()
			errQcDel := t.cache.InvalidateQueriesContainingID(lctx, queryPrefix, id) // Pass model ID
			qcDelDuration := time.Since(qcDelStart)
			if errQcDel != nil {
				log.Printf("WARN: Failed to invalidate queries containing ID %d with prefix '%s': %v (%s)", id, queryPrefix, errQcDel, qcDelDuration)
			}
			// --- End Query Cache Invalidation ---

			return nil // Lock action successful
		})
		// --- End Lock ---

		if err != nil {
			// Error occurred during lock acquisition or the action inside the lock
			return fmt.Errorf("save update failed (lock or db/cache exec): %w", err)
		}
	}

	baseModelPtr.SetNewRecordFlag(false)

	// --- Trigger AfterSave hook (for both create and update) ---
	if errHook := triggerEvent(ctx, EventTypeAfterSave, value, changedFields); errHook != nil { // Uses helper defined later
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

	baseModelPtr := getBaseModelPtr(value) // Uses helper defined later
	if baseModelPtr == nil {
		return errors.New("deleteInternal: value must embed BaseModel")
	}
	id := baseModelPtr.ID
	if id == 0 {
		return errors.New("deleteInternal: cannot delete record with zero ID")
	}

	// --- Trigger BeforeDelete hook ---
	if err := triggerEvent(ctx, EventTypeBeforeDelete, value, nil); err != nil { // Uses helper defined later
		return fmt.Errorf("BeforeDelete hook failed: %w", err)
	}

	// Use the cached model info
	info := t.info
	tableName := info.tableName
	pkName := info.pkName

	// Lock the record during deletion
	lockKey := fmt.Sprintf("lock:%s:%d", tableName, id)
	err := withLock(ctx, t.cache, lockKey, func(lctx context.Context) error { // Uses helper defined later
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
			return ErrNotFound
		}

		// --- Object Cache Invalidate within Lock ---
		cacheKey := generateCacheKey(tableName, id) // Uses helper defined later
		cacheDelStart := time.Now()
		errCacheDel := t.cache.DeleteModel(lctx, cacheKey) // Use context from lock
		cacheDelDuration := time.Since(cacheDelStart)
		if errCacheDel != nil {
			log.Printf("WARN: Failed to delete model from cache during delete lock for key %s: %v (%s)", cacheKey, errCacheDel, cacheDelDuration)
		}
		// --- End Object Cache Invalidate ---

		// --- Query Cache Invalidation (Targeted: Delete queries containing this ID) ---
		queryPrefix := fmt.Sprintf("query:%s:", tableName)
		qcDelStart := time.Now()
		errQcDel := t.cache.InvalidateQueriesContainingID(lctx, queryPrefix, id) // Pass model ID
		qcDelDuration := time.Since(qcDelStart)
		if errQcDel != nil {
			log.Printf("WARN: Failed to invalidate queries containing ID %d with prefix '%s': %v (%s)", id, queryPrefix, errQcDel, qcDelDuration)
		}
		// --- End Query Cache Invalidation ---

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
	if errHook := triggerEvent(ctx, EventTypeAfterDelete, value, nil); errHook != nil { // Uses helper defined later
		log.Printf("WARN: AfterDelete hook failed: %v", errHook)
	}

	return nil // Success
}

// queryInternal retrieves a slice of models based on query parameters.
func (t *Thing[T]) queryInternal(ctx context.Context, params QueryParams) ([]*T, error) {
	if t.db == nil || t.cache == nil { // Ensure cache is also checked
		return nil, errors.New("queryInternal: DBAdapter and CacheClient must be configured on Thing instance")
	}
	if t.info == nil {
		return nil, errors.New("queryInternal: model info not available on Thing instance")
	}

	// 1. Get IDs using the cache-aware idsInternal function
	ids, err := t.idsInternal(ctx, t.info, params)
	if err != nil {
		// Propagate errors from fetching IDs (e.g., DB error if cache missed)
		return nil, fmt.Errorf("failed to get IDs for query: %w", err)
	}
	if len(ids) == 0 {
		return []*T{}, nil // No IDs found, return empty slice
	}

	// 2. Fetch models by ID, utilizing the object cache via byIDInternal
	//    We fetch them one by one for simplicity, leveraging byIDInternal's caching.
	//    A batch fetch could be implemented for optimization if needed.
	results := make([]*T, 0, len(ids))
	fetchErrors := []string{}
	for _, id := range ids {
		instance := new(T)
		err := t.byIDInternal(ctx, id, instance) // This checks cache first
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				// This might indicate cache inconsistency (ID found in query cache but not object cache/DB)
				log.Printf("WARN: queryInternal: ID %d found by idsInternal but not found by byIDInternal (cache/DB inconsistency?)", id)
				// Skip this ID, but collect the error
				fetchErrors = append(fetchErrors, fmt.Sprintf("ID %d not found", id))
			} else {
				// Log and collect other unexpected errors during fetch
				log.Printf("ERROR: queryInternal: Failed to fetch model for ID %d: %v", id, err)
				fetchErrors = append(fetchErrors, fmt.Sprintf("fetching ID %d: %v", id, err))
			}
			continue // Skip adding this instance if an error occurred
		}
		results = append(results, instance)
	}

	// Optional: If strict consistency is required, return an error if any fetches failed.
	if len(fetchErrors) > 0 {
		log.Printf("WARN: queryInternal completed with %d fetch errors: %s", len(fetchErrors), strings.Join(fetchErrors, "; "))
		// Decide whether to return partial results or an error. Returning partial results for now.
		// return nil, fmt.Errorf("failed to fetch some models: %s", strings.Join(fetchErrors, "; "))
	}

	// 3. Eager Loading (Preloading) - Apply to the successfully fetched results
	if len(params.Preloads) > 0 && len(results) > 0 {
		for _, preloadName := range params.Preloads {
			if err := t.preloadRelations(ctx, results, preloadName); err != nil {
				// Log the error but potentially continue? Or return?
				// For now, return the error to make it visible.
				log.Printf("Warning: failed to preload relation '%s': %v", preloadName, err)
				return nil, fmt.Errorf("failed to preload relation '%s': %w", preloadName, err)
			}
		}
	}

	// Set isNewRecord flag to false for all retrieved records
	for _, result := range results {
		setNewRecordFlagIfBaseModel(result, false)
	}

	return results, nil
}

// --- IDs Query ---

// idsInternal retrieves a list of IDs based on query parameters, checking query cache first.
func (t *Thing[T]) idsInternal(ctx context.Context, info *modelInfo, params QueryParams) ([]int64, error) {
	if t.cache == nil || t.db == nil {
		return nil, errors.New("Thing not properly initialized with DBAdapter and CacheClient")
	}
	// 1. Generate Query Cache Key
	queryKey, err := generateQueryCacheKey(info.tableName, params) // Uses helper defined later
	if err != nil {
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
			log.Printf("WARN: Cache GetQueryIDs error for key %s: %v (%s)", queryKey, cacheErr, cacheDuration)
		}
		log.Printf("QUERY CACHE MISS: Key: %s (%s) - Error: %v", queryKey, cacheDuration, cacheErr)
	}

	// 3. Cache Miss or Key Generation Failed: Query Database
	query, args := buildSelectIDsSQL(info, params) // Uses helper defined later
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
			log.Printf("WARN: Failed to cache query IDs for key %s: %v (%s)", queryKey, errCacheSet, cacheSetDuration)
		}
	}

	return fetchedIDs, nil
}

// --- Event System --- (Moved Down)

type EventType string // Already defined in Constants section

// EventListener defines the signature for functions that can listen to events.
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

	// Get model ID for logging
	var modelID int64 = -1
	if bm := getBaseModelPtr(model); bm != nil { // Uses helper defined later
		modelID = bm.GetID()
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

// --- Model Metadata Cache --- (Moved Down)

// modelInfo holds cached reflection results for a specific Model type.
type modelInfo struct {
	tableName        string
	pkName           string            // Database name of the primary key field
	columns          []string          // List of all database column names (including PK)
	fields           []string          // Corresponding Go struct field names (order matches columns)
	fieldToColumnMap map[string]string // Map Go field name to its corresponding DB column name
	columnToFieldMap map[string]string // Map DB column name to its corresponding Go field name
}

// modelCache stores modelInfo structs, keyed by reflect.Type.
var modelCache sync.Map // map[reflect.Type]*modelInfo

// getCachedModelInfo retrieves or computes/caches metadata for a given Model type.
func getCachedModelInfo(modelType reflect.Type) (*modelInfo, error) {
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct type, got %s", modelType.Kind())
	}

	if cached, ok := modelCache.Load(modelType); ok {
		return cached.(*modelInfo), nil
	}

	info := modelInfo{
		fieldToColumnMap: make(map[string]string),
		columnToFieldMap: make(map[string]string),
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

			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				if field.Type == reflect.TypeOf(BaseModel{}) {
					baseModelType := field.Type
					for j := 0; j < baseModelType.NumField(); j++ {
						baseField := baseModelType.Field(j)
						baseDbTag := baseField.Tag.Get("db")
						if baseDbTag == "-" || !baseField.IsExported() {
							continue
						}
						colName := baseDbTag
						if colName == "" {
							colName = strings.ToLower(baseField.Name)
						}
						info.columns = append(info.columns, colName)
						info.fields = append(info.fields, baseField.Name)
						info.fieldToColumnMap[baseField.Name] = colName
						info.columnToFieldMap[colName] = baseField.Name
						if colName == "id" {
							pkDbName = colName
						}
					}
				} else {
					processFields(field.Type)
				}
				continue
			}

			if dbTag == "-" || dbTag == "" || !field.IsExported() {
				continue
			}
			colName := dbTag
			if colName == "id" && pkDbName == "" {
				pkDbName = colName
			}
			info.columns = append(info.columns, colName)
			info.fields = append(info.fields, field.Name)
			info.fieldToColumnMap[field.Name] = colName
			info.columnToFieldMap[colName] = field.Name
		}
	}
	processFields(modelType)

	if pkDbName == "" {
		return nil, fmt.Errorf("primary key column (assumed 'id') not found via 'db:\"id\"' tag in struct %s or its embedded BaseModel", modelType.Name())
	}
	info.pkName = pkDbName

	actualInfo, _ := modelCache.LoadOrStore(modelType, &info)
	return actualInfo.(*modelInfo), nil
}

// --- Change Detection --- (Moved Down)

// findChangedFieldsReflection compares two structs (original and updated)
// and returns a map of column names to the changed values from the updated struct.
func findChangedFieldsReflection[T any](original, updated *T, info *modelInfo) (map[string]interface{}, error) {
	changed := make(map[string]interface{})
	originalVal := reflect.ValueOf(original).Elem()
	updatedVal := reflect.ValueOf(updated).Elem()
	typeName := originalVal.Type().Name()

	if originalVal.Type() != updatedVal.Type() {
		return nil, errors.New("original and updated values must be of the same type")
	}

	var processFields func(oVal, uVal reflect.Value, path string)
	processFields = func(oVal, uVal reflect.Value, path string) {
		structType := oVal.Type()
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)
			fieldName := field.Name
			currentPath := path + "." + fieldName

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

			if !field.IsExported() {
				log.Printf("DEBUG: [%s%s] Skipping unexported field", typeName, currentPath)
				continue
			}

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
	processFields(originalVal, updatedVal, "")
	log.Printf("DEBUG: Raw changed map for %s: %v", typeName, changed)

	// --- Handle UpdatedAt specifically ---
	var updatedAtDBColName string
	updatedAtGoFieldName := "UpdatedAt"
	if col, ok := info.fieldToColumnMap[updatedAtGoFieldName]; ok {
		updatedAtDBColName = col
	}

	if updatedAtDBColName != "" && len(changed) == 1 {
		if _, isOnlyChange := changed[updatedAtDBColName]; isOnlyChange {
			log.Printf("DEBUG: Removing '%s' from changed map as it's the only change.", updatedAtDBColName)
			delete(changed, updatedAtDBColName)
		}
	}
	log.Printf("DEBUG: Final changed map for %s after UpdatedAt check: %v", typeName, changed)

	return changed, nil
}

// --- SQL Builder Helpers --- (Moved Down)

// buildSelectSQL constructs a SELECT statement.
func buildSelectSQL(info *modelInfo) string {
	if info.tableName == "" || len(info.columns) == 0 {
		log.Printf("Error: buildSelectSQL called with incomplete modelInfo: %+v", info)
		return ""
	}
	quotedColumns := make([]string, len(info.columns))
	for i, col := range info.columns {
		quotedColumns[i] = fmt.Sprintf("\"%s\"", col)
	}
	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(quotedColumns, ", "), info.tableName)
}

// buildSelectIDsSQL constructs a SELECT statement to fetch only primary key IDs.
func buildSelectIDsSQL(info *modelInfo, params QueryParams) (string, []interface{}) {
	var query strings.Builder
	args := []interface{}{}
	if info.tableName == "" || info.pkName == "" {
		log.Printf("Error: buildSelectIDsSQL called with incomplete modelInfo: %+v", info)
		return "", nil
	}
	query.WriteString(fmt.Sprintf("SELECT \"%s\" FROM %s", info.pkName, info.tableName))
	if params.Where != "" {
		query.WriteString(" WHERE ")
		query.WriteString(params.Where)
		args = append(args, params.Args...)
	}
	if params.Order != "" {
		query.WriteString(" ORDER BY ")
		query.WriteString(params.Order)
	}
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

// buildSelectSQLWithParams constructs SELECT SQL query including WHERE, ORDER, LIMIT, OFFSET.
func buildSelectSQLWithParams(info *modelInfo, params QueryParams) (string, []interface{}) {
	baseQuery := buildSelectSQL(info) // Use existing helper for SELECT ... FROM ...

	var whereClause string
	var args = params.Args
	if params.Where != "" {
		whereClause = " WHERE " + params.Where
	}

	var orderClause string
	if params.Order != "" {
		orderClause = " ORDER BY " + params.Order
	}

	var limitClause string
	if params.Limit > 0 {
		// Use placeholders for limit/offset for broader DB compatibility
		limitClause = " LIMIT ?"
		args = append(args, params.Limit)
		if params.Start > 0 {
			limitClause += " OFFSET ?"
			args = append(args, params.Start)
		}
	}

	finalQuery := baseQuery + whereClause + orderClause + limitClause
	log.Printf("Built SQL: %s | Args: %v", finalQuery, args) // Debug log
	return finalQuery, args
}

// --- Reflection & Value Helpers --- (Moved Down)

// getTableNameFromType determines table name from reflect.Type.
func getTableNameFromType(modelType reflect.Type) string {
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return ""
	}
	instPtr := reflect.New(modelType)
	meth := instPtr.MethodByName("TableName")
	if meth.IsValid() && meth.Type().NumIn() == 0 && meth.Type().NumOut() == 1 && meth.Type().Out(0) == reflect.TypeOf("") {
		results := meth.Call(nil)
		if len(results) > 0 {
			if name := results[0].String(); name != "" {
				return name
			}
		}
	}
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
	return strings.ToLower(modelType.Name()) + "s" // Pluralize convention
}

// getBaseModelPtr returns a pointer to the embedded BaseModel if it exists and is addressable.
func getBaseModelPtr(value interface{}) *BaseModel {
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() == reflect.Struct {
		bmField := val.FieldByName("BaseModel")
		if bmField.IsValid() && bmField.Type() == reflect.TypeOf(BaseModel{}) && bmField.CanAddr() {
			return bmField.Addr().Interface().(*BaseModel)
		}
	}
	return nil
}

// setNewRecordFlagIfBaseModel sets the flag if the value embeds BaseModel.
func setNewRecordFlagIfBaseModel(value interface{}, isNew bool) {
	if bmPtr := getBaseModelPtr(value); bmPtr != nil {
		bmPtr.SetNewRecordFlag(isNew)
	}
}

// setCreatedAtTimestamp sets the CreatedAt field if it exists.
func setCreatedAtTimestamp(value interface{}, t time.Time) {
	if bmPtr := getBaseModelPtr(value); bmPtr != nil {
		bmPtr.CreatedAt = t
		return
	}
	val := reflect.ValueOf(value).Elem()
	field := val.FieldByName("CreatedAt")
	if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
		field.Set(reflect.ValueOf(t))
	}
}

// setUpdatedAtTimestamp sets the UpdatedAt field if it exists.
func setUpdatedAtTimestamp(value interface{}, t time.Time) {
	if bmPtr := getBaseModelPtr(value); bmPtr != nil {
		bmPtr.UpdatedAt = t
		return
	}
	val := reflect.ValueOf(value).Elem()
	field := val.FieldByName("UpdatedAt")
	if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
		field.Set(reflect.ValueOf(t))
	}
}

// --- Cache Helpers --- (Moved Down)

// generateCacheKey creates a standard cache key string for a single model.
func generateCacheKey(tableName string, id int64) string {
	// Format: {tableName}:{id}
	return fmt.Sprintf("%s:%d", tableName, id)
}

// generateQueryCacheKey generates a cache key for a query based on table name and params hash.
func generateQueryCacheKey(tableName string, params QueryParams) (string, error) {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		log.Printf("ERROR: json.Marshal(params) failed in generateQueryCacheKey: %v\nParams: %+v", err, params)
		return "", fmt.Errorf("failed to marshal query params for cache key: %w", err)
	}
	hasher := sha256.New()
	hasher.Write([]byte(tableName))
	hasher.Write(paramsBytes)
	hash := hex.EncodeToString(hasher.Sum(nil))
	return fmt.Sprintf("query:%s:%s", tableName, hash), nil
}

// withLock acquires a lock, executes the action, and releases the lock.
func withLock(ctx context.Context, cache CacheClient, lockKey string, action func(ctx context.Context) error) error {
	if cache == nil {
		log.Printf("Warning: Proceeding without lock for key '%s', cache client is nil", lockKey)
		return action(ctx)
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
			log.Printf("Context cancelled while waiting for lock: %s", lockKey)
			return ctx.Err()
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
		} else {
			log.Printf("Lock released for key '%s'", lockKey)
		}
	}()
	log.Printf("Lock acquired for key '%s'", lockKey)
	return action(ctx)
}

// --- Querying Structs --- (Moved Down)

// QueryParams defines parameters for database queries.
type QueryParams struct {
	Where    string        // Raw WHERE clause (e.g., "status = ? AND name LIKE ?")
	Args     []interface{} // Arguments for the WHERE clause placeholders
	Order    string        // Raw ORDER BY clause (e.g., "created_at DESC")
	Start    int           // Offset (for pagination)
	Limit    int           // Limit (for pagination)
	Preloads []string      // List of relationship names to eager-load (e.g., ["Author", "Comments"])
}

// --- NEW Relationship Preloading Functions --- (Added at the end)

// RelationshipOpts holds parsed options from the 'thing' tag for relationships.
type RelationshipOpts struct {
	RelationType string // "belongsTo", "hasMany"
	ForeignKey   string // FK field name in the *owning* struct (for belongsTo) or *related* struct (for hasMany)
	LocalKey     string // PK field name in the *owning* struct (defaults to info.pkName)
	RelatedModel string // Optional: Specify related model name if different from field type
}

// parseRelationTag parses the `thing` tag for relationship definitions.
// Example: `thing:"rel=belongsTo;fk=AuthorID"`
// Example: `thing:"rel=hasMany;fk=PostID"`
func parseRelationTag(tag string) (opts RelationshipOpts, err error) {
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue // Ignore invalid parts
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		// --- DEBUG LOGGING ---
		log.Printf("DEBUG parseRelationTag: Part='%s', Key='%s', Value='%s'", part, key, value)
		// --- END DEBUG LOGGING ---

		switch strings.ToLower(key) {
		case "rel":
			opts.RelationType = strings.ToLower(value) // Ensure value is lowercased
			// --- DEBUG LOGGING ---
			log.Printf("DEBUG parseRelationTag: Set opts.RelationType to '%s'", opts.RelationType)
			// --- END DEBUG LOGGING ---
		case "fk":
			opts.ForeignKey = value
		case "localkey":
			opts.LocalKey = value
		case "model":
			opts.RelatedModel = value // Less common, might be needed for interface fields?
		}
	}

	if opts.RelationType != "belongs_to" && opts.RelationType != "has_many" {
		err = fmt.Errorf("invalid or missing 'rel' type in tag: %s", tag)
		return
	}
	// Basic validation (more could be added)
	if opts.ForeignKey == "" {
		// FK is usually required, derivation logic is complex, enforce for now.
		err = fmt.Errorf("missing required 'fk' (foreign key) in relation tag: %s", tag)
		return
	}

	return opts, nil
}

// preloadRelations dispatches preloading work based on relationship type.
func (t *Thing[T]) preloadRelations(ctx context.Context, results []*T, preloadName string) error {
	if len(results) == 0 {
		return nil // Nothing to preload
	}
	// 1. Get reflect.Type and reflect.Value of the results slice
	resultsVal := reflect.ValueOf(results)
	modelType := reflect.TypeOf(results[0]).Elem() // Get type T from the first element

	// 2. Get the StructField for preloadName
	field, found := modelType.FieldByName(preloadName)
	if !found {
		return fmt.Errorf("relation field '%s' not found in model type %s", preloadName, modelType.Name())
	}

	// 3. Parse the `thing` tag
	tag := field.Tag.Get("thing")
	if tag == "" {
		return fmt.Errorf("missing 'thing' tag on relation field '%s' in model type %s", preloadName, modelType.Name())
	}
	opts, err := parseRelationTag(tag)
	if err != nil {
		return fmt.Errorf("error parsing 'thing' tag for field '%s': %w", preloadName, err)
	}

	// Ensure LocalKey is set (defaults to primary key of T)
	if opts.LocalKey == "" {
		opts.LocalKey = t.info.pkName
		if opts.LocalKey == "" {
			return fmt.Errorf("cannot determine local key (primary key) for model type %s", modelType.Name())
		}
	}

	// 4. Based on tag, call appropriate helper
	log.Printf("Dispatching preload for '%s.%s' (Type: %s, FK: %s, LocalKey: %s)", modelType.Name(), preloadName, opts.RelationType, opts.ForeignKey, opts.LocalKey)
	switch opts.RelationType {
	case "belongs_to":
		return t.preloadBelongsTo(ctx, resultsVal, field, opts)
	case "has_many":
		return t.preloadHasMany(ctx, resultsVal, field, opts)
	default:
		// Should be caught by parseRelationTag, but defensive check
		return fmt.Errorf("unsupported relation type '%s' for field '%s'", opts.RelationType, preloadName)
	}
}

// preloadBelongsTo handles eager loading for BelongsTo relationships.
func (t *Thing[T]) preloadBelongsTo(ctx context.Context, resultsVal reflect.Value, field reflect.StructField, opts RelationshipOpts) error {
	// T = Owning Model (e.g., Post)
	// R = Related Model (e.g., User)

	// --- Type checking ---
	relatedFieldType := field.Type // Type of the field (e.g., *User)
	if relatedFieldType.Kind() != reflect.Ptr || relatedFieldType.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("belongsTo field '%s' must be a pointer to a struct, but got %s", field.Name, relatedFieldType.String())
	}
	relatedModelType := relatedFieldType.Elem() // Type R (e.g., User)
	log.Printf("Preloading BelongsTo: Field %s (*%s), FK in %s: %s", field.Name, relatedModelType.Name(), t.info.tableName, opts.ForeignKey)

	// --- Get Foreign Key Info from Owning Model T ---
	owningModelType := t.info.columnToFieldMap // map[dbCol]goField
	fkFieldName, fkFieldFound := owningModelType[opts.ForeignKey]
	if !fkFieldFound {
		// Maybe FK *is* the Go field name?
		if _, directFieldFound := reflect.TypeOf(resultsVal.Index(0).Interface()).Elem().FieldByName(opts.ForeignKey); directFieldFound {
			fkFieldName = opts.ForeignKey
			fkFieldFound = true
		} else {
			return fmt.Errorf("foreign key field '%s' (from tag 'fk') not found in owning model %s", opts.ForeignKey, resultsVal.Type().Elem().Elem().Name())
		}
	}
	log.Printf("Foreign Key Field in Owning Model (%s): %s", resultsVal.Type().Elem().Elem().Name(), fkFieldName)

	// --- Collect Foreign Key Values from results ---
	fkValues := make(map[interface{}]bool)
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelElem := resultsVal.Index(i).Elem()          // Get underlying struct T
		fkFieldVal := owningModelElem.FieldByName(fkFieldName) // Get FK field (e.g., AuthorID)
		if fkFieldVal.IsValid() {
			key := fkFieldVal.Interface() // Get the value (e.g., int64 ID)
			// Check for zero value - don't preload nil associations
			if !reflect.ValueOf(key).IsZero() {
				fkValues[key] = true
			}
		} else {
			log.Printf("WARN: FK field '%s' not valid on element %d during belongsTo preload", fkFieldName, i)
		}
	}

	if len(fkValues) == 0 {
		log.Println("No valid non-zero foreign keys found for belongsTo preload.")
		return nil // No related models to load
	}

	uniqueFkList := make([]interface{}, 0, len(fkValues))
	for k := range fkValues {
		uniqueFkList = append(uniqueFkList, k)
	}
	log.Printf("Collected %d unique foreign keys for %s: %v", len(uniqueFkList), field.Name, uniqueFkList)

	// --- Fetch Related Models (Type R) ---
	// We need info for the related model (R)
	relatedInfo, err := getCachedModelInfo(relatedModelType)
	if err != nil {
		return fmt.Errorf("failed to get model info for related type %s: %w", relatedModelType.Name(), err)
	}
	// Assume related model's PK is the target of our FK list
	relatedPkName := relatedInfo.pkName
	if relatedPkName == "" {
		return fmt.Errorf("cannot determine primary key for related type %s", relatedModelType.Name())
	}

	// Construct query: SELECT * FROM related_table WHERE related_pk IN (?, ?, ...)
	relatedQuery := buildSelectSQL(relatedInfo)                                     // SELECT related_cols... FROM related_table
	placeholders := strings.Repeat("?,", len(uniqueFkList))[:len(uniqueFkList)*2-1] // "?,?,?"
	relatedQuery = fmt.Sprintf("%s WHERE %s IN (%s)", relatedQuery, relatedPkName, placeholders)

	// Create a slice of the correct related type R (e.g., []*User)
	relatedSliceType := reflect.SliceOf(relatedFieldType)   // Slice of pointers *R
	relatedSliceVal := reflect.New(relatedSliceType).Elem() // reflect.Value of the slice

	log.Printf("Executing query for related %s: %s [%v]", relatedModelType.Name(), relatedQuery, uniqueFkList)
	// Use the instance's db adapter (t.db) instead of globalDB
	err = t.db.Select(ctx, relatedSliceVal.Addr().Interface(), relatedQuery, uniqueFkList...)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to fetch related %s models: %w", relatedModelType.Name(), err)
	}

	// --- Map Related Models back to results ---
	// Create a map: foreignKeyValue -> *RelatedModel
	relatedMap := make(map[interface{}]reflect.Value)
	for i := 0; i < relatedSliceVal.Len(); i++ {
		relatedModelPtr := relatedSliceVal.Index(i) // *R
		relatedModelElem := relatedModelPtr.Elem()  // R
		// Find the PK field value of the related model (which corresponds to our FK)
		pkGoFieldName, ok := relatedInfo.columnToFieldMap[relatedPkName]
		if !ok {
			log.Printf("WARN: Could not find Go field name for PK '%s' in related model %s", relatedPkName, relatedModelType.Name())
			continue
		}
		pkValField := relatedModelElem.FieldByName(pkGoFieldName)
		if pkValField.IsValid() {
			relatedMap[pkValField.Interface()] = relatedModelPtr // Map FK -> *R
		} else {
			log.Printf("WARN: PK field '%s' not valid on fetched related model %s at index %d", pkGoFieldName, relatedModelType.Name(), i)
		}
	}

	// Iterate through original results and set the relationship field
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelPtr := resultsVal.Index(i)                  // *T
		owningModelElem := owningModelPtr.Elem()               // T
		fkFieldVal := owningModelElem.FieldByName(fkFieldName) // Get FK field value

		if fkFieldVal.IsValid() {
			fkValue := fkFieldVal.Interface()
			if relatedModelPtr, found := relatedMap[fkValue]; found {
				relationField := owningModelElem.FieldByName(field.Name) // Get the *User field
				if relationField.IsValid() && relationField.CanSet() {
					relationField.Set(relatedModelPtr) // Set post.Author = userPtr
				}
			}
		}
	}

	log.Printf("Successfully preloaded BelongsTo relation '%s'", field.Name)
	return nil
}

// preloadHasMany handles eager loading for HasMany relationships.
func (t *Thing[T]) preloadHasMany(ctx context.Context, resultsVal reflect.Value, field reflect.StructField, opts RelationshipOpts) error {
	// T = Owning Model (e.g., Post)
	// R = Related Model (e.g., Comment)

	// --- Type checking ---
	relatedFieldType := field.Type // Type of the field (e.g., []Comment or []*Comment)
	if relatedFieldType.Kind() != reflect.Slice {
		return fmt.Errorf("hasMany field '%s' must be a slice, but got %s", field.Name, relatedFieldType.String())
	}
	relatedElemType := relatedFieldType.Elem() // Type of slice elements (e.g., Comment or *Comment)
	var relatedModelType reflect.Type
	var relatedIsSliceOfPtr bool
	if relatedElemType.Kind() == reflect.Ptr && relatedElemType.Elem().Kind() == reflect.Struct {
		relatedModelType = relatedElemType.Elem() // Type R (e.g., Comment)
		relatedIsSliceOfPtr = true
	} else if relatedElemType.Kind() == reflect.Struct {
		relatedModelType = relatedElemType // Type R (e.g., Comment)
		relatedIsSliceOfPtr = false
	} else {
		return fmt.Errorf("hasMany field '%s' must be a slice of structs or pointers to structs, got slice of %s", field.Name, relatedElemType.String())
	}
	log.Printf("Preloading HasMany: Field %s (%s), FK in %s: %s", field.Name, relatedFieldType.String(), relatedModelType.Name(), opts.ForeignKey)

	// --- Get Local Key Info from Owning Model T ---
	// Local key is usually the PK of the owning model T
	localKeyColName := opts.LocalKey // e.g., "id"
	localKeyGoFieldName, ok := t.info.columnToFieldMap[localKeyColName]
	if !ok {
		return fmt.Errorf("local key column '%s' not found in model %s info", localKeyColName, resultsVal.Type().Elem().Elem().Name())
	}
	log.Printf("Local Key Field in Owning Model (%s): %s (DB: %s)", resultsVal.Type().Elem().Elem().Name(), localKeyGoFieldName, localKeyColName)

	// --- Collect Local Key Values from results ---
	localKeyValues := make(map[interface{}]bool)
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelElem := resultsVal.Index(i).Elem()                  // Get underlying struct T
		lkFieldVal := owningModelElem.FieldByName(localKeyGoFieldName) // Get local key field (e.g., ID)
		if lkFieldVal.IsValid() {
			key := lkFieldVal.Interface() // Get the value (e.g., int64 ID)
			localKeyValues[key] = true
		} else {
			log.Printf("WARN: Local key field '%s' not valid on element %d during hasMany preload", localKeyGoFieldName, i)
		}
	}

	if len(localKeyValues) == 0 {
		log.Println("No valid local keys found for hasMany preload.")
		return nil // No related models to load
	}

	uniqueLkList := make([]interface{}, 0, len(localKeyValues))
	for k := range localKeyValues {
		uniqueLkList = append(uniqueLkList, k)
	}
	log.Printf("Collected %d unique local keys for %s: %v", len(uniqueLkList), field.Name, uniqueLkList)

	// --- Fetch Related Models (Type R) ---
	// We need info for the related model (R)
	relatedInfo, err := getCachedModelInfo(relatedModelType)
	if err != nil {
		return fmt.Errorf("failed to get model info for related type %s: %w", relatedModelType.Name(), err)
	}
	// Foreign key is in the *related* table R, referencing the owning model T
	relatedFkColName := opts.ForeignKey // e.g., "post_id"
	relatedFkGoFieldName, fkFieldFound := relatedInfo.columnToFieldMap[relatedFkColName]
	if !fkFieldFound {
		// Maybe FK *is* the Go field name in related model?
		if _, directFieldFound := relatedModelType.FieldByName(opts.ForeignKey); directFieldFound {
			relatedFkGoFieldName = opts.ForeignKey
			fkFieldFound = true
		} else {
			return fmt.Errorf("foreign key column '%s' (from tag 'fk') not found in related model %s info", relatedFkColName, relatedModelType.Name())
		}
	}
	log.Printf("Foreign Key Field in Related Model (%s): %s (DB: %s)", relatedModelType.Name(), relatedFkGoFieldName, relatedFkColName)

	// Construct query: SELECT * FROM related_table WHERE related_fk IN (?, ?, ...)
	relatedQuery := buildSelectSQL(relatedInfo)                                     // SELECT related_cols... FROM related_table
	placeholders := strings.Repeat("?,", len(uniqueLkList))[:len(uniqueLkList)*2-1] // "?,?,?"
	relatedQuery = fmt.Sprintf("%s WHERE %s IN (%s)", relatedQuery, relatedFkColName, placeholders)
	// TODO: Add ORDER BY? Maybe another tag option?

	// Create a slice of the correct related type R (e.g., []*Comment or []Comment)
	// We need a slice of pointers for db.Select even if the target field is slice of structs
	relatedSelectDestType := reflect.SliceOf(reflect.PtrTo(relatedModelType)) // []*R
	relatedSelectDestVal := reflect.New(relatedSelectDestType).Elem()         // reflect.Value of the []*R slice

	log.Printf("Executing query for related %s: %s [%v]", relatedModelType.Name(), relatedQuery, uniqueLkList)
	// Use the instance's db adapter (t.db) instead of globalDB
	err = t.db.Select(ctx, relatedSelectDestVal.Addr().Interface(), relatedQuery, uniqueLkList...)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to fetch related %s models: %w", relatedModelType.Name(), err)
	}

	// --- Map Related Models back to results ---
	// Create a map: localKeyValue -> SliceOfRelatedModels ([]*R or []R)
	relatedMap := reflect.MakeMap(reflect.MapOf(reflect.TypeOf(uniqueLkList[0]), relatedFieldType)) // map[LocalKeyType]SliceType

	for i := 0; i < relatedSelectDestVal.Len(); i++ {
		relatedModelPtr := relatedSelectDestVal.Index(i) // *R
		relatedModelElem := relatedModelPtr.Elem()       // R
		// Get the FK value from the related model (which points back to the owning model's PK)
		fkValField := relatedModelElem.FieldByName(relatedFkGoFieldName)
		if !fkValField.IsValid() {
			log.Printf("WARN: FK field '%s' not valid on fetched related model %s at index %d", relatedFkGoFieldName, relatedModelType.Name(), i)
			continue
		}
		fkValue := fkValField.Interface()
		mapKey := reflect.ValueOf(fkValue)

		// Get the existing slice for this key, or create a new one
		sliceForKey := relatedMap.MapIndex(mapKey)
		if !sliceForKey.IsValid() {
			sliceForKey = reflect.MakeSlice(relatedFieldType, 0, 0)
		}

		// Append the current related model (*R or R)
		var modelToAppend reflect.Value
		if relatedIsSliceOfPtr {
			modelToAppend = relatedModelPtr // Append *R
		} else {
			modelToAppend = relatedModelElem // Append R
		}
		sliceForKey = reflect.Append(sliceForKey, modelToAppend)
		relatedMap.SetMapIndex(mapKey, sliceForKey) // Put the updated slice back in the map
	}

	// Iterate through original results and set the relationship field
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelPtr := resultsVal.Index(i)                          // *T
		owningModelElem := owningModelPtr.Elem()                       // T
		lkFieldVal := owningModelElem.FieldByName(localKeyGoFieldName) // Get local key value (e.g., post.ID)

		if lkFieldVal.IsValid() {
			lkValue := lkFieldVal.Interface()
			mapKey := reflect.ValueOf(lkValue)
			relatedSlice := relatedMap.MapIndex(mapKey) // Get the reflect.Value for the slice

			// Check if the key was found *and* if the resulting slice value is valid
			if relatedSlice.IsValid() { // Check if the key existed in the map
				relationField := owningModelElem.FieldByName(field.Name) // Get the []Comment field
				if relationField.IsValid() && relationField.CanSet() {
					relationField.Set(relatedSlice) // Set the reflect.Value directly
				} else {
					log.Printf("WARN: Cannot set hasMany field '%s' on owning model %s at index %d", field.Name, resultsVal.Type().Elem().Elem().Name(), i)
				}
			} else {
				// Key not found in map, ensure field is set to an empty slice
				relationField := owningModelElem.FieldByName(field.Name)
				if relationField.IsValid() && relationField.CanSet() {
					relationField.Set(reflect.MakeSlice(relatedFieldType, 0, 0))
				}
			}
		}
	}

	log.Printf("Successfully preloaded HasMany relation '%s'", field.Name)
	return nil
}

// loadInternal explicitly loads relationships for a given model instance using the provided context.
// This is the internal implementation called by the public Load method.
// model must be a pointer to a struct of type T.
// relations are the string names of the fields representing the relationships to load.
func (t *Thing[T]) loadInternal(ctx context.Context, model *T, relations ...string) error {
	if model == nil {
		return errors.New("model cannot be nil")
	}
	if len(relations) == 0 {
		return nil // Nothing to load
	}
	if t.info == nil { // Add check for safety
		return errors.New("loadInternal: model info not available on Thing instance")
	}

	// Wrap the single model in a slice to reuse the preloadRelations helper
	modelSlice := []*T{model}

	for _, relationName := range relations {
		// Use the provided context (ctx) when calling preloadRelations
		if err := t.preloadRelations(ctx, modelSlice, relationName); err != nil {
			// Stop on the first error
			return fmt.Errorf("failed to load relation '%s': %w", relationName, err)
		}
	}

	return nil
}
