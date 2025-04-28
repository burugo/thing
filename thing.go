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
	// Cache duration for non-existent entries (used in byIDInternal)
	NegativeCacheTTL = 5 * time.Minute // Define missing constant
	// Default cache duration for models (used in byIDInternal)
	DefaultCacheTTL = 1 * time.Hour // Define missing constant
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
	// ErrQueryCacheNoneResult indicates that the cache holds the marker for an empty query result set.
	ErrQueryCacheNoneResult = errors.New("cached query result indicates no matching records")
	// ErrCacheNoneResult indicates the cache key exists but holds the NoneResult marker.
	ErrCacheNoneResult = errors.New("cache indicates record does not exist (NoneResult marker found)")
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
	info  *ModelInfo // Pre-computed metadata for type T // Renamed: modelInfo -> ModelInfo
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
	info, err := GetCachedModelInfo(modelType) // Renamed: getCachedModelInfo -> GetCachedModelInfo
	if err != nil {
		return nil, fmt.Errorf("failed to get model info for type %s: %w", modelType.Name(), err)
	}
	// TableName is now populated within GetCachedModelInfo
	// info.TableName = getTableNameFromType(modelType) // Removed redundant call
	if info.TableName == "" {
		log.Printf("Warning: Could not determine table name for type %s during New. Relying on instance method?", modelType.Name())
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

// Query prepares a query based on QueryParams and returns a *CachedResult[T] for lazy execution.
// The actual database query happens when Count() or Fetch() is called on the result.
// It returns the CachedResult instance and a nil error, assuming basic validation passed.
// Error handling for query execution is done within CachedResult methods.
func (t *Thing[T]) Query(params QueryParams) (*CachedResult[T], error) {
	// TODO: Add validation for params if necessary?
	return &CachedResult[T]{
		thing:  t,
		params: params,
		// cachedIDs, cachedCount, hasLoadedIDs, hasLoadedCount, hasLoadedAll, all initialized to zero values
	}, nil
}

// QueryResult returns a CachedResult that will lazily execute the query.
// The actual database query happens when IDs() or Fetch() is called on the result.
func (t *Thing[T]) QueryResult(params QueryParams) *CachedResult[T] {
	return &CachedResult[T]{
		thing:  t,
		params: params,
		// ids and idsFetched are zero/false by default
	}
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

// fetchModelsByIDsInternal is the core logic for fetching models by their primary keys,
// handling cache checks, database queries for misses, and caching results.
// It requires the concrete modelType to instantiate objects and slices correctly.
func fetchModelsByIDsInternal(ctx context.Context, cache CacheClient, db DBAdapter, modelInfo *ModelInfo, modelType reflect.Type, ids []int64) (map[int64]reflect.Value, error) {
	resultMap := make(map[int64]reflect.Value)
	if len(ids) == 0 {
		return resultMap, nil
	}

	missingIDs := []int64{} // Initialize explicitly

	// 1. Try fetching from cache
	if cache != nil {
		for _, id := range ids { // Iterate through original ids
			cacheKey := generateCacheKey(modelInfo.TableName, id)
			instancePtrVal := reflect.New(modelType) // Create pointer *T

			// Pass pointer interface, e.g., *User
			err := cache.GetModel(ctx, cacheKey, instancePtrVal.Interface())

			if err == nil {
				// Found in cache
				setNewRecordFlagIfBaseModel(instancePtrVal.Interface(), false)
				resultMap[id] = instancePtrVal                 // Store the reflect.Value pointer
				log.Printf("DEBUG CACHE HIT for %s", cacheKey) // Added log
			} else if errors.Is(err, ErrCacheNoneResult) {
				// Found NoneResult marker - this ID is handled, DO NOT add to missingIDs.
				log.Printf("DEBUG CACHE HIT (NoneResult) for %s", cacheKey) // Added log
			} else if errors.Is(err, ErrNotFound) {
				// True cache miss
				log.Printf("DEBUG CACHE MISS for %s", cacheKey) // Added log
				missingIDs = append(missingIDs, id)
			} else {
				// Unexpected cache error
				log.Printf("WARN: Cache error during batch fetch for key %s: %v", cacheKey, err)
				missingIDs = append(missingIDs, id) // Treat as missing if error
			}
		}
	} else {
		missingIDs = ids // No cache, all IDs are missing
	}

	// 2. If all found in cache (or marked as NoneResult), return early
	if len(missingIDs) == 0 {
		return resultMap, nil
	}

	// 3. Fetch missing models from database (in batches if needed)
	for i := 0; i < len(missingIDs); i += ByIDBatchSize {
		end := i + ByIDBatchSize
		if end > len(missingIDs) {
			end = len(missingIDs)
		}

		batchIDs := missingIDs[i:end]
		if len(batchIDs) == 0 {
			continue
		}

		// Keep track of IDs actually found in this batch DB query
		fetchedIDsInBatch := make(map[int64]bool)

		// Create placeholders for SQL IN clause
		placeholders := strings.Repeat("?,", len(batchIDs))
		placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma

		// Convert IDs to interface{} for query args
		args := make([]interface{}, len(batchIDs))
		for j, id := range batchIDs {
			args[j] = id
		}

		// Build and execute query
		query := fmt.Sprintf("%s WHERE %s IN (%s)",
			buildSelectSQL(modelInfo), // Use passed modelInfo
			modelInfo.pkName,
			placeholders)

		// Create a slice of pointers to the concrete type T for scanning
		sliceType := reflect.SliceOf(reflect.PointerTo(modelType)) // Use the passed modelType
		sliceVal := reflect.New(sliceType).Elem()

		err := db.Select(ctx, sliceVal.Addr().Interface(), query, args...)
		if err != nil {
			// Log error but continue processing potentially found results
			log.Printf("Error fetching batch %v-%v for type %s: %v", i, end-1, modelInfo.TableName, err)
			continue // Continue to next batch if any
		}

		// Add fetched models to result map and cache them
		for j := 0; j < sliceVal.Len(); j++ {
			modelPtrVal := sliceVal.Index(j) // *T as reflect.Value
			modelInterface := modelPtrVal.Interface()
			baseModelPtr := getBaseModelPtr(modelInterface)
			if baseModelPtr == nil {
				log.Printf("WARN: Fetched model of type %s has no BaseModel embedded, cannot get ID", modelInfo.TableName)
				continue
			}

			id := baseModelPtr.GetID()
			if id == 0 {
				log.Printf("WARN: Fetched model %s has zero ID", modelInfo.TableName)
				continue
			}

			setNewRecordFlagIfBaseModel(modelInterface, false)
			resultMap[id] = modelPtrVal // Store the reflect.Value pointer

			// Cache the model
			if cache != nil {
				cacheKey := generateCacheKey(modelInfo.TableName, id)
				if errCache := cache.SetModel(ctx, cacheKey, modelInterface, DefaultCacheDuration); errCache != nil {
					log.Printf("WARN: Failed to cache model %s:%d after batch fetch: %v", modelInfo.TableName, id, errCache)
				}
			}
			// Mark this ID as successfully fetched from DB in this batch
			fetchedIDsInBatch[id] = true
		}

		// --- Cache NoneResult for IDs not found in this batch ---
		if cache != nil {
			for _, batchID := range batchIDs {
				if !fetchedIDsInBatch[batchID] {
					// This ID was queried but not returned by DB
					cacheKey := generateCacheKey(modelInfo.TableName, batchID)
					log.Printf("DEBUG DB NOT FOUND for %s (in batch %v). Caching NoneResult.", cacheKey, batchIDs)
					errCacheSet := cache.Set(ctx, cacheKey, NoneResult, NegativeCacheTTL)
					if errCacheSet != nil {
						log.Printf("WARN: Failed to set NoneResult in cache for key %s: %v", cacheKey, errCacheSet)
					}
				}
			}
		}
		// --- End Cache NoneResult ---

	}

	return resultMap, nil
}

// byIDInternal fetches a single model by its ID, now acting as a wrapper
// around fetchModelsByIDsInternal.
// The dest argument must be a pointer to the struct type (e.g., *User).
func (t *Thing[T]) byIDInternal(ctx context.Context, id int64, dest interface{}) error {
	if id <= 0 {
		return errors.New("invalid ID provided (must be > 0)")
	}

	// Validate dest is a pointer to the correct struct type
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() {
		return errors.New("destination must be a non-nil pointer")
	}
	destElemType := destVal.Elem().Type()
	if destElemType != reflect.TypeOf(*new(T)) {
		return fmt.Errorf("destination type mismatch: expected *%s, got %s",
			reflect.TypeOf(*new(T)).Name(), destElemType.String())
	}

	// Get the concrete type T for the internal helper
	modelType := reflect.TypeOf((*T)(nil)).Elem()

	// Call the internal multi-fetch helper with a single ID
	idsToFetch := []int64{id}
	resultsMap, err := fetchModelsByIDsInternal(ctx, t.cache, t.db, t.info, modelType, idsToFetch)
	if err != nil {
		// Propagate errors from the internal fetch
		return fmt.Errorf("fetchModelsByIDsInternal failed for ID %d: %w", id, err)
	}

	// Check if the requested ID was found in the results
	if modelVal, ok := resultsMap[id]; ok {
		// Found the model. Need to copy the value into the destination.
		// Ensure dest is settable and types match.
		if destVal.Elem().CanSet() {
			// modelVal is reflect.Value of type *T
			destVal.Elem().Set(modelVal.Elem()) // Set the value T into dest (*T)
			return nil                          // Success
		} else {
			// This should generally not happen if dest validation passed
			return fmt.Errorf("internal error: destination cannot be set for ID %d", id)
		}
	} else {
		// ID not found in the results map (could be DB miss or cached NoneResult)
		return ErrNotFound
	}
}

// saveInternal handles both creating and updating records.
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
			t.info.TableName, // Renamed: tableName -> TableName
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
			cacheKey := generateCacheKey(t.info.TableName, newID) // Renamed: tableName -> TableName
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
			log.Printf("Skipping update for %s %d: No relevant fields changed", t.info.TableName, id)
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
			log.Printf("ERROR: No SET clauses generated for update on %s %d despite changedFields being non-empty: %v", t.info.TableName, id, changedFields) // Renamed: tableName -> TableName
			return errors.New("internal error: no fields to update after processing changed fields")
		}

		query = fmt.Sprintf("UPDATE %s SET %s WHERE %s = ?",
			t.info.TableName, // Renamed: tableName -> TableName
			strings.Join(setClauses, ", "),
			t.info.pkName, // pkName is still unexported, assume ok for now
		)
		args = append(updateArgs, id) // Add the ID for the WHERE clause

		// --- Lock for Update and Cache Invalidation ---
		lockKey := fmt.Sprintf("lock:%s:%d", t.info.TableName, id)               // Renamed: tableName -> TableName
		err = withLock(ctx, t.cache, lockKey, func(lctx context.Context) error { // Uses helper defined later
			log.Printf("DB UPDATE: %s [%v]", query, args)
			result, err = t.db.Exec(lctx, query, args...)
			if err != nil {
				return fmt.Errorf("database update failed: %w", err)
			}

			rowsAffected, _ := result.RowsAffected()
			if rowsAffected == 0 {
				log.Printf("WARN: Update affected 0 rows for %s %d. Record might not exist?", t.info.TableName, id) // Renamed: tableName -> TableName
			}

			// Invalidate Object Cache within the lock, after successful DB update
			cacheKey := generateCacheKey(t.info.TableName, id) // Renamed: tableName -> TableName
			cacheDelStart := time.Now()
			errCacheDel := t.cache.DeleteModel(lctx, cacheKey)
			cacheDelDuration := time.Since(cacheDelStart)
			if errCacheDel != nil {
				log.Printf("WARN: Failed to delete model from cache during update lock for key %s: %v (%s)", cacheKey, errCacheDel, cacheDelDuration)
			}

			// --- Query Cache Invalidation (Targeted: Delete queries containing this ID) ---
			queryPrefix := fmt.Sprintf("list:%s:", t.info.TableName) // Renamed: tableName -> TableName // CORRECTED PREFIX
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
	tableName := info.TableName // Renamed: tableName -> TableName
	pkName := info.pkName       // pkName is still unexported, assume ok for now

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
		cacheKey := generateCacheKey(tableName, id)
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

// ModelInfo holds cached reflection results for a specific Model type.
// Renamed: modelInfo -> ModelInfo
type ModelInfo struct {
	TableName        string            // Renamed: tableName -> TableName
	pkName           string            // Database name of the primary key field (kept unexported for now)
	Columns          []string          // Renamed: columns -> Columns
	fields           []string          // Corresponding Go struct field names (order matches columns, kept unexported)
	fieldToColumnMap map[string]string // Map Go field name to its corresponding DB column name (kept unexported)
	columnToFieldMap map[string]string // Map DB column name to its corresponding Go field name (kept unexported)
}

// modelCache stores ModelInfo structs, keyed by reflect.Type.
// Renamed: modelCache -> ModelCache
var ModelCache sync.Map // map[reflect.Type]*ModelInfo

// getCachedModelInfo retrieves or computes/caches metadata for a given Model type.
// Renamed: getCachedModelInfo -> GetCachedModelInfo (Exported)
// Returns *ModelInfo now
func GetCachedModelInfo(modelType reflect.Type) (*ModelInfo, error) {
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct type, got %s", modelType.Kind())
	}

	if cached, ok := ModelCache.Load(modelType); ok {
		return cached.(*ModelInfo), nil
	}

	info := ModelInfo{ // Using renamed struct
		fieldToColumnMap: make(map[string]string),
		columnToFieldMap: make(map[string]string),
	}
	pkDbName := ""

	numFields := modelType.NumField()
	info.Columns = make([]string, 0, numFields) // Using renamed field
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
						info.Columns = append(info.Columns, colName) // Using renamed field
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
			info.Columns = append(info.Columns, colName) // Using renamed field
			info.fields = append(info.fields, field.Name)
			info.fieldToColumnMap[field.Name] = colName
			info.columnToFieldMap[colName] = field.Name
		}
	}
	processFields(modelType)

	if pkDbName == "" {
		return nil, fmt.Errorf("primary key column (assumed 'id') not found via 'db:\"id\"' tag in struct %s or its embedded BaseModel", modelType.Name())
	}
	info.pkName = pkDbName                           // pkName kept unexported for now
	info.TableName = getTableNameFromType(modelType) // Added TableName population here
	if info.TableName == "" {
		log.Printf("Warning: Could not determine table name for type %s in GetCachedModelInfo.", modelType.Name())
	}

	actualInfo, _ := ModelCache.LoadOrStore(modelType, &info)
	return actualInfo.(*ModelInfo), nil
}

// --- Change Detection --- (Moved Down)

// findChangedFieldsReflection compares two structs (original and updated)
// and returns a map of column names to the changed values from the updated struct.
func findChangedFieldsReflection[T any](original, updated *T, info *ModelInfo) (map[string]interface{}, error) {
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
func buildSelectSQL(info *ModelInfo) string {
	if info.TableName == "" || len(info.Columns) == 0 {
		log.Printf("Error: buildSelectSQL called with incomplete modelInfo: %+v", info)
		return ""
	}
	quotedColumns := make([]string, len(info.Columns))
	for i, col := range info.Columns {
		quotedColumns[i] = fmt.Sprintf("\"%s\"", col)
	}
	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(quotedColumns, ", "), info.TableName)
}

// buildSelectIDsSQL constructs a SELECT statement to fetch only primary key IDs.
func buildSelectIDsSQL(info *ModelInfo, params QueryParams) (string, []interface{}) {
	var query strings.Builder
	args := []interface{}{}
	if info.TableName == "" || info.pkName == "" {
		log.Printf("Error: buildSelectIDsSQL called with incomplete modelInfo: %+v", info)
		return "", nil
	}
	query.WriteString(fmt.Sprintf("SELECT \"%s\" FROM %s", info.pkName, info.TableName))
	if params.Where != "" {
		query.WriteString(" WHERE ")
		query.WriteString(params.Where)
		args = append(args, params.Args...)
	}
	if params.Order != "" {
		query.WriteString(" ORDER BY ")
		query.WriteString(params.Order)
	}
	// LIMIT and OFFSET removed - pagination handled by CachedResult.Fetch
	// if params.Limit > 0 {
	// 	query.WriteString(" LIMIT ?")
	// 	args = append(args, params.Limit)
	// }
	// if params.Start > 0 {
	// 	query.WriteString(" OFFSET ?")
	// 	args = append(args, params.Start)
	// }
	return query.String(), args
}

// buildSelectSQLWithParams constructs SELECT SQL query including WHERE, ORDER.
// LIMIT and OFFSET are removed as they are now handled by CachedResult.Fetch.
func buildSelectSQLWithParams(info *ModelInfo, params QueryParams) (string, []interface{}) {
	baseQuery := buildSelectSQL(info) // Use existing helper for SELECT ... FROM ...

	var whereClause string
	// Keep a copy of args before potentially adding limit/offset
	args := make([]interface{}, len(params.Args))
	copy(args, params.Args)
	if params.Where != "" {
		whereClause = " WHERE " + params.Where
	}

	var orderClause string
	if params.Order != "" {
		orderClause = " ORDER BY " + params.Order
	}

	// LIMIT and OFFSET removed - pagination handled by CachedResult.Fetch
	// var limitClause string
	// if params.Limit > 0 {
	// 	// Use placeholders for limit/offset for broader DB compatibility
	// 	limitClause = " LIMIT ?"
	// 	args = append(args, params.Limit)
	// 	if params.Start > 0 {
	// 		limitClause += " OFFSET ?"
	// 		args = append(args, params.Start)
	// 	}
	// }

	finalQuery := baseQuery + whereClause + orderClause                        // + limitClause (removed)
	log.Printf("Built SQL (no limit/offset): %s | Args: %v", finalQuery, args) // Debug log
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

// generateListCacheKey generates a cache key for a query result list based on parameters.
// It ensures that different query parameters result in different cache keys.
// NOTE: This version uses JSON marshaling for simplicity, which might not be the
// most performant or canonical way for complex argument types.
func generateListCacheKey(tableName string, params QueryParams) (string, error) {
	// Create a canonical representation of the query parameters
	// Sort Args slice if elements are comparable? For now, rely on order.
	// Consider hashing complex args or using a more robust serialization.

	// Normalize args for consistent hashing (convert to string)
	normalizedArgs := make([]interface{}, len(params.Args))
	for i, arg := range params.Args {
		// Basic normalization, more complex types might need specific handling
		normalizedArgs[i] = fmt.Sprintf("%v", arg)
	}
	keyGenParams := params             // Make a copy
	keyGenParams.Args = normalizedArgs // Use normalized args for key generation

	paramsBytes, err := json.Marshal(keyGenParams)
	if err != nil {
		log.Printf("Error marshaling params for cache key: %v", err)
		return "", fmt.Errorf("failed to marshal query params for cache key: %w", err)
	}

	hasher := sha256.New()
	hasher.Write([]byte(tableName))
	hasher.Write(paramsBytes)
	hash := hex.EncodeToString(hasher.Sum(nil))

	// Format: list:{tableName}:{sha256_hash_of_params}
	return fmt.Sprintf("list:%s:%s", tableName, hash), nil
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
	Where string        // Raw WHERE clause (e.g., "status = ? AND name LIKE ?")
	Args  []interface{} // Arguments for the WHERE clause placeholders
	Order string        // Raw ORDER BY clause (e.g., "created_at DESC")
	// Start    int           // Offset (for pagination) - REMOVED
	// Limit    int           // Limit (for pagination) - REMOVED
	Preloads []string // List of relationship names to eager-load (e.g., ["Author", "Comments"])
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
	log.Printf("Preloading BelongsTo: Field %s (*%s), FK in %s: %s", field.Name, relatedModelType.Name(), t.info.TableName, opts.ForeignKey)

	// --- Get Foreign Key Info from Owning Model T ---
	owningModelType := t.info.columnToFieldMap // map[dbCol]goField
	fkFieldName, fkFieldFound := owningModelType[opts.ForeignKey]
	if !fkFieldFound {
		if _, directFieldFound := reflect.TypeOf(resultsVal.Index(0).Interface()).Elem().FieldByName(opts.ForeignKey); directFieldFound {
			fkFieldName = opts.ForeignKey
			fkFieldFound = true
		} else {
			return fmt.Errorf("foreign key field '%s' (from tag 'fk') not found in owning model %s", opts.ForeignKey, resultsVal.Type().Elem().Elem().Name())
		}
	}
	log.Printf("Foreign Key Field in Owning Model (%s): %s", resultsVal.Type().Elem().Elem().Name(), fkFieldName)

	// --- Collect Unique Foreign Key Values from results ---
	fkValuesMap := make(map[int64]bool) // Use int64 specifically for IDs
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelElem := resultsVal.Index(i).Elem()
		fkFieldVal := owningModelElem.FieldByName(fkFieldName)
		if fkFieldVal.IsValid() {
			// Attempt to convert FK to int64
			var key int64
			if fkFieldVal.Type().ConvertibleTo(reflect.TypeOf(key)) {
				key = fkFieldVal.Convert(reflect.TypeOf(key)).Int()
				if key != 0 { // Only collect non-zero keys
					fkValuesMap[key] = true
				}
			} else {
				log.Printf("WARN: FK field '%s' value (%v) on element %d is not convertible to int64", fkFieldName, fkFieldVal.Interface(), i)
			}
		} else {
			log.Printf("WARN: FK field '%s' not valid on element %d during belongsTo preload", fkFieldName, i)
		}
	}

	if len(fkValuesMap) == 0 {
		log.Println("No valid non-zero foreign keys found for belongsTo preload.")
		return nil // No related models to load
	}

	uniqueFkList := make([]int64, 0, len(fkValuesMap))
	for k := range fkValuesMap {
		uniqueFkList = append(uniqueFkList, k)
	}
	log.Printf("Collected %d unique foreign keys for %s: %v", len(uniqueFkList), field.Name, uniqueFkList)

	// --- Fetch Related Models (Type R) using the internal helper ---
	relatedInfo, err := GetCachedModelInfo(relatedModelType)
	if err != nil {
		return fmt.Errorf("failed to get model info for related type %s: %w", relatedModelType.Name(), err)
	}

	// Call the internal helper, passing the concrete relatedModelType
	relatedMap, err := fetchModelsByIDsInternal(ctx, t.cache, t.db, relatedInfo, relatedModelType, uniqueFkList)
	if err != nil {
		return fmt.Errorf("failed to fetch related %s models using internal helper: %w", relatedModelType.Name(), err)
	}

	// --- Map Related Models back to original results --- (Using reflect.Value map)
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelPtr := resultsVal.Index(i)                  // *T
		owningModelElem := owningModelPtr.Elem()               // T
		fkFieldVal := owningModelElem.FieldByName(fkFieldName) // Get FK field value

		if fkFieldVal.IsValid() {
			var fkValueInt64 int64
			if fkFieldVal.Type().ConvertibleTo(reflect.TypeOf(fkValueInt64)) {
				fkValueInt64 = fkFieldVal.Convert(reflect.TypeOf(fkValueInt64)).Int()
				if relatedModelPtr, found := relatedMap[fkValueInt64]; found {
					relationField := owningModelElem.FieldByName(field.Name) // Get the *User field
					if relationField.IsValid() && relationField.CanSet() {
						log.Printf("DEBUG Preload Set: Setting %s.%s (FK: %d) to %v", owningModelElem.Type().Name(), field.Name, fkValueInt64, relatedModelPtr.Interface()) // DEBUG LOG
						relationField.Set(relatedModelPtr)                                                                                                                  // Set post.Author = userPtr (*R)
					} else {
						log.Printf("WARN Preload Set: Relation field %s.%s is not valid or settable", owningModelElem.Type().Name(), field.Name) // DEBUG LOG
					}
				} else {
					log.Printf("DEBUG Preload Set: Related model for FK %d not found in map", fkValueInt64) // DEBUG LOG
				}
			} // else: FK was not convertible or was zero, do nothing
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
		// Ensure the relation slice is initialized to empty on the owning models
		for i := 0; i < resultsVal.Len(); i++ {
			owningModelElem := resultsVal.Index(i).Elem()
			relationField := owningModelElem.FieldByName(field.Name)
			if relationField.IsValid() && relationField.CanSet() {
				relationField.Set(reflect.MakeSlice(relatedFieldType, 0, 0))
			}
		}
		return nil // No related models to load
	}

	uniqueLkList := make([]interface{}, 0, len(localKeyValues))
	for k := range localKeyValues {
		uniqueLkList = append(uniqueLkList, k)
	}
	log.Printf("Collected %d unique local keys for %s: %v", len(uniqueLkList), field.Name, uniqueLkList)

	// --- Step 1: Get Related Model IDs ---
	relatedInfo, err := GetCachedModelInfo(relatedModelType)
	if err != nil {
		return fmt.Errorf("failed to get model info for related type %s: %w", relatedModelType.Name(), err)
	}
	relatedFkColName := opts.ForeignKey // FK column name in the related table R

	// --- Get Related Model FK Go field name ---
	relatedFkGoFieldName, fkFieldFound := relatedInfo.columnToFieldMap[relatedFkColName]
	if !fkFieldFound {
		// Fallback check if FK name matches Go field name directly
		if _, directFieldFound := relatedModelType.FieldByName(opts.ForeignKey); directFieldFound {
			relatedFkGoFieldName = opts.ForeignKey
			fkFieldFound = true
		} else {
			return fmt.Errorf("foreign key column '%s' (from tag 'fk') not found in related model %s info or as a direct field name", relatedFkColName, relatedModelType.Name())
		}
	}
	log.Printf("Foreign Key Field in Related Model (%s): %s (DB: %s)", relatedModelType.Name(), relatedFkGoFieldName, relatedFkColName)

	// Prepare query params for fetching related model IDs
	placeholders := strings.Repeat("?,", len(uniqueLkList))[:len(uniqueLkList)*2-1]
	relatedIDParams := QueryParams{
		Where: fmt.Sprintf("\"%s\" IN (%s)", relatedFkColName, placeholders), // Ensure FK column is quoted
		Args:  uniqueLkList,
		// Potentially add Order from tag later?
	}

	var relatedIDs []int64
	listCacheKey := ""
	cacheHit := false // Flag to indicate if we got a definitive result (IDs or NoneResult) from cache

	if t.cache != nil {
		// 1. Generate Cache Key (Error handling below)
		keyGenParams := relatedIDParams
		normalizedArgs := make([]interface{}, len(keyGenParams.Args))
		for i, arg := range keyGenParams.Args {
			normalizedArgs[i] = fmt.Sprintf("%v", arg)
		}
		keyGenParams.Args = normalizedArgs
		paramsBytes, jsonErr := json.Marshal(keyGenParams)

		if jsonErr == nil {
			hasher := sha256.New()
			hasher.Write([]byte(relatedInfo.TableName))
			hasher.Write(paramsBytes)
			hash := hex.EncodeToString(hasher.Sum(nil))
			listCacheKey = fmt.Sprintf("list:%s:%s", relatedInfo.TableName, hash)

			// 2. Try GetQueryIDs directly (handles NoneResult internally now)
			cachedIDs, queryIDsErr := t.cache.GetQueryIDs(ctx, listCacheKey)

			if queryIDsErr == nil {
				// Cache hit with actual IDs
				log.Printf("CACHE HIT (Query IDs): Found %d related IDs for key %s", len(cachedIDs), listCacheKey)
				relatedIDs = cachedIDs
				cacheHit = true // Got the IDs from cache
			} else if errors.Is(queryIDsErr, ErrQueryCacheNoneResult) {
				// Cache hit with NoneResult marker
				log.Printf("CACHE HIT (NoneResult): Found NoneResult marker for key %s", listCacheKey)
				relatedIDs = []int64{} // Empty slice for NoneResult
				cacheHit = true        // Definitively know the result is empty
			} else if errors.Is(queryIDsErr, ErrNotFound) {
				// Cache miss
				log.Printf("CACHE MISS (Query IDs): Key %s not found.", listCacheKey)
				// cacheHit remains false
			} else {
				// Other cache error
				log.Printf("WARN: Cache GetQueryIDs error for key %s: %v. Treating as cache miss.", listCacheKey, queryIDsErr)
				// cacheHit remains false
			}

		} else {
			log.Printf("WARN: Failed to marshal params for list cache key generation: %v", jsonErr)
			// cacheHit remains false, proceed to DB query
		}
	} // End if t.cache != nil

	// Fetch IDs from DB if cache was not hit (cacheHit is false)
	if !cacheHit {
		// Build query to select only the primary key of the related model
		idQuery := fmt.Sprintf("SELECT \"%s\" FROM %s WHERE %s",
			relatedInfo.pkName,    // PK column of the related table R
			relatedInfo.TableName, // Related table R
			relatedIDParams.Where, // WHERE clause (e.g., "\"user_id\" IN (?,?,?)")
		)
		// TODO: Add Order By if needed

		log.Printf("Executing query for related IDs: %s [%v]", idQuery, relatedIDParams.Args)

		// Execute the query - db.Select expects a slice destination
		// Create a slice of the appropriate type for the PK (assuming int64 for now)
		idSliceDest := make([]int64, 0)
		err = t.db.Select(ctx, &idSliceDest, idQuery, relatedIDParams.Args...)

		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to fetch related %s IDs: %w", relatedModelType.Name(), err)
		}
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("DB HIT (Zero IDs): No related %s IDs found for local keys %v", relatedModelType.Name(), uniqueLkList)
		} else {
			log.Printf("DB HIT (IDs): Fetched %d related %s IDs from database.", len(idSliceDest), relatedModelType.Name())
		}

		relatedIDs = idSliceDest

		// Cache the fetched IDs (or NoneResult if empty)
		if t.cache != nil && listCacheKey != "" {
			if len(relatedIDs) > 0 {
				log.Printf("Caching %d fetched IDs for query key %s", len(relatedIDs), listCacheKey)
				if qcErr := t.cache.SetQueryIDs(ctx, listCacheKey, relatedIDs, DefaultCacheDuration); qcErr != nil {
					log.Printf("WARN: Failed to cache query IDs for key %s: %v", listCacheKey, qcErr)
				}
			} else {
				log.Printf("Caching NoneResult for query key %s", listCacheKey)
				if qcErr := t.cache.Set(ctx, listCacheKey, NoneResult, NoneResultCacheDuration); qcErr != nil {
					log.Printf("WARN: Failed to cache NoneResult for query key %s: %v", listCacheKey, qcErr)
				}
			}
		}
	}

	// --- Step 2: Fetch Related Models using IDs ---
	var relatedModelsMap map[int64]reflect.Value
	if len(relatedIDs) > 0 {
		// Use the internal helper which checks object cache
		log.Printf("Fetching %d related %s models using fetchModelsByIDsInternal", len(relatedIDs), relatedModelType.Name())
		relatedModelsMap, err = fetchModelsByIDsInternal(ctx, t.cache, t.db, relatedInfo, relatedModelType, relatedIDs)
		if err != nil {
			// Log error but proceed to map any models that might have been fetched from cache before the error
			log.Printf("WARN: Error during fetchModelsByIDsInternal for %s: %v. Proceeding with potentially partial results.", relatedModelType.Name(), err)
			// return fmt.Errorf("failed to fetch related %s models using internal helper: %w", relatedModelType.Name(), err)
		}
		log.Printf("Successfully fetched/retrieved %d related %s models from internal helper.", len(relatedModelsMap), relatedModelType.Name())
	} else {
		// If no IDs were found (either from cache or DB), initialize an empty map
		relatedModelsMap = make(map[int64]reflect.Value)
		log.Printf("No related %s IDs found, skipping fetchModelsByIDsInternal.", relatedModelType.Name())
	}

	// --- Step 3: Map Related Models back to results ---
	// Group related models by their foreign key value
	groupedRelatedMap := make(map[interface{}][]reflect.Value) // Map FK value -> Slice of *R or R

	for _, relatedModelPtrVal := range relatedModelsMap { // Iterate over map[id]reflect.Value(*R)
		relatedModelElem := relatedModelPtrVal.Elem()                    // Get R from *R
		fkValField := relatedModelElem.FieldByName(relatedFkGoFieldName) // Get the FK field (e.g., UserID)
		if !fkValField.IsValid() {
			log.Printf("WARN: FK field '%s' not valid on fetched related model %s during mapping", relatedFkGoFieldName, relatedModelType.Name())
			continue
		}
		fkValue := fkValField.Interface() // Get the FK value (e.g., the owning User's ID)

		var modelToAppend reflect.Value
		if relatedIsSliceOfPtr {
			modelToAppend = relatedModelPtrVal // Append *R
		} else {
			modelToAppend = relatedModelElem // Append R
		}
		groupedRelatedMap[fkValue] = append(groupedRelatedMap[fkValue], modelToAppend)
	}

	// Iterate through original results (owning models) and set the relationship field
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelPtr := resultsVal.Index(i)                          // *T
		owningModelElem := owningModelPtr.Elem()                       // T
		lkFieldVal := owningModelElem.FieldByName(localKeyGoFieldName) // Get local key field (e.g., ID)

		relationField := owningModelElem.FieldByName(field.Name) // Get the []R or []*R field
		if !relationField.IsValid() || !relationField.CanSet() {
			log.Printf("WARN: Cannot set hasMany field '%s' on owning model %s at index %d", field.Name, resultsVal.Type().Elem().Elem().Name(), i)
			continue
		}

		if lkFieldVal.IsValid() {
			lkValue := lkFieldVal.Interface() // Get the local key value

			// Find the slice of related models for this local key value
			if relatedSliceValues, found := groupedRelatedMap[lkValue]; found {
				// Create a new slice of the correct type ([]R or []*R) and append results
				finalSlice := reflect.MakeSlice(relatedFieldType, 0, len(relatedSliceValues))
				for _, relatedVal := range relatedSliceValues {
					finalSlice = reflect.Append(finalSlice, relatedVal)
				}
				relationField.Set(finalSlice)
			} else {
				// No related models found for this owner, set empty slice
				relationField.Set(reflect.MakeSlice(relatedFieldType, 0, 0))
			}
		} else {
			// Local key was invalid, set empty slice
			relationField.Set(reflect.MakeSlice(relatedFieldType, 0, 0))
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

const ByIDBatchSize = 100 // Or make configurable

// ByIDs retrieves multiple records by their primary keys and optionally preloads relations.
func (t *Thing[T]) ByIDs(ids []int64, preloads ...string) (map[int64]*T, error) {
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	resultsMapReflect, err := fetchModelsByIDsInternal(t.ctx, t.cache, t.db, t.info, modelType, ids)
	if err != nil {
		return nil, fmt.Errorf("ByIDs failed during internal fetch: %w", err)
	}

	// Convert map[int64]reflect.Value (containing *T) to map[int64]*T
	resultsMapTyped := make(map[int64]*T, len(resultsMapReflect))
	// Also collect results in a slice for preloading
	resultsSliceForPreload := make([]*T, 0, len(resultsMapReflect))
	for id, modelVal := range resultsMapReflect {
		if typedModel, ok := modelVal.Interface().(*T); ok {
			resultsMapTyped[id] = typedModel
			resultsSliceForPreload = append(resultsSliceForPreload, typedModel)
		} else {
			log.Printf("WARN: ByIDs: Could not assert type for ID %d", id)
		}
	}

	// Apply preloads if requested
	if len(preloads) > 0 && len(resultsSliceForPreload) > 0 {
		for _, preloadName := range preloads {
			if preloadErr := t.preloadRelations(t.ctx, resultsSliceForPreload, preloadName); preloadErr != nil {
				// Log error but return results obtained so far
				log.Printf("WARN: ByIDs: failed to apply preload '%s': %v", preloadName, preloadErr)
				// Optionally return error: return nil, fmt.Errorf("failed to apply preload '%s': %w", preloadName, preloadErr)
			}
		}
	}

	return resultsMapTyped, nil
}
