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

	// Added for environment variables
	"os"
	"reflect"
	"sort" // Added for parsing env var
	"strings"
	"sync"
	"time"
)

// --- Constants ---

const (
	// Represents a non-existent entry in the cache, similar to PHP's NoneResult
	NoneResult = "NoneResult"
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
// Definitions moved to errors.go
)

// --- Global Configuration ---

var (
	globalDB     DBAdapter   // Set via Configure
	globalCache  CacheClient // Set via Configure
	isConfigured bool
	configMutex  sync.RWMutex
	// Global cache TTL, determined at startup
	globalCacheTTL time.Duration
)

// Configure sets up the package-level database and cache clients, and the global cache TTL.
// This MUST be called once during application initialization before using Use[T].
// It accepts an optional time.Duration argument to set the global cache TTL.
// If no TTL is provided, it defaults to 8 hours.
func Configure(db DBAdapter, cache CacheClient, ttl ...time.Duration) error { // Added optional ttl parameter
	configMutex.Lock()
	defer configMutex.Unlock()

	defaultTTL := 8 * time.Hour // Define default TTL

	// --- Configure Global TTL ---
	if len(ttl) > 0 {
		// Use the first provided TTL value if it's positive
		if ttl[0] > 0 {
			globalCacheTTL = ttl[0]
			log.Printf("Configuring global cache TTL: %s (from argument)", globalCacheTTL)
		} else {
			globalCacheTTL = defaultTTL
			log.Printf("Warning: Provided TTL is not positive (%s). Using default: %s", ttl[0], globalCacheTTL)
		}
	} else {
		// No TTL provided, use the default
		globalCacheTTL = defaultTTL
		log.Printf("Configuring global cache TTL: %s (default)", globalCacheTTL)
	}
	// --- End Configure Global TTL ---
	// if isConfigured {
	// 	log.Println("Thing package already configured. Re-configuring...")
	// }

	if db == nil {
		return errors.New("DBAdapter must be non-nil")
	}
	if cache == nil {
		return errors.New("CacheClient must be non-nil")
	}
	globalDB = db
	globalCache = cache
	isConfigured = true
	log.Println("Thing package configured globally with DB and Cache adapters.")

	return nil
}

// --- Thing Core Struct ---

// Thing is the central access point for ORM operations, analogous to gorm.DB.
// It holds database/cache clients and the context for operations.
type Thing[T any] struct {
	db    DBAdapter
	cache CacheClient
	ctx   context.Context
	info  *ModelInfo // Pre-computed metadata for type T
}

// --- Thing Constructors & Accessors ---

// New creates a new Thing instance with default context.Background().
// Made generic: requires type parameter T when called, e.g., New[MyModel](...).
func New[T any](db DBAdapter, cache CacheClient) (*Thing[T], error) {
	log.Println("DEBUG: Entering New[T]") // Added log
	if db == nil {
		log.Println("DEBUG: New[T] - DB is nil") // Added log
		return nil, errors.New("DBAdapter must be non-nil")
	}
	if cache == nil {
		log.Println("DEBUG: New[T] - Cache is nil") // Added log
		return nil, errors.New("CacheClient must be non-nil")
	}
	log.Println("New Thing instance created.")

	// Pre-compute model info for T
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	log.Printf("DEBUG: New[T] - Getting model info for type: %s", modelType.Name()) // Added log
	info, err := GetCachedModelInfo(modelType)                                      // Renamed: getCachedModelInfo -> GetCachedModelInfo
	if err != nil {
		log.Printf("DEBUG: New[T] - Error getting model info: %v", err) // Added log
		return nil, fmt.Errorf("failed to get model info for type %s: %w", modelType.Name(), err)
	}
	log.Printf("DEBUG: New[T] - Got model info: %+v", info) // Added log
	// TableName is now populated within GetCachedModelInfo
	// info.TableName = getTableNameFromType(modelType) // Removed redundant call
	if info.TableName == "" {
		log.Printf("Warning: Could not determine table name for type %s during New. Relying on instance method?", modelType.Name())
	}

	log.Println("DEBUG: New[T] - Creating Thing struct") // Added log
	t := &Thing[T]{
		db:    db,
		cache: cache,
		ctx:   context.Background(), // Default context
		info:  info,                 // Store pre-computed info
	}
	log.Println("DEBUG: New[T] - Returning new Thing instance") // Added log
	return t, nil
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

// --- BaseModel Struct ---

// BaseModel provides common fields and functionality for database models.
// It should be embedded into specific model structs.
type BaseModel struct {
	ID        int64     `json:"id" db:"id"`                 // Primary key
	CreatedAt time.Time `json:"created_at" db:"created_at"` // Timestamp for creation
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"` // Timestamp for last update
	Deleted   bool      `json:"-" db:"deleted"`             // Soft delete flag

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

// KeepItem checks if the record is considered active (not soft-deleted).
func (b *BaseModel) KeepItem() bool {
	return !b.Deleted
}

// SetNewRecordFlag sets the internal isNewRecord flag.
func (b *BaseModel) SetNewRecordFlag(isNew bool) {
	b.isNewRecord = isNew
}

// --- Internal `Thing` Methods ---

// fetchModelsByIDsInternal is the core logic for fetching models by their primary keys,
// handling cache checks, database queries for misses, and caching results.
// It requires the concrete modelType to instantiate objects and slices correctly.
// REMOVED TTL arguments
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
				if errCache := cache.SetModel(ctx, cacheKey, modelInterface, globalCacheTTL); errCache != nil { // USE globalCacheTTL
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
					errCacheSet := cache.Set(ctx, cacheKey, NoneResult, globalCacheTTL) // USE globalCacheTTL
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
	resultsMap, err := fetchModelsByIDsInternal(ctx, t.cache, t.db, t.info, modelType, idsToFetch) // REMOVED TTLs
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
	var original *T                          // Declare original here for broader scope

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
			errCacheSet := t.cache.SetModel(ctx, cacheKey, value, globalCacheTTL) // USE globalCacheTTL
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
		// Assign to the already declared original variable
		original = reflect.New(modelValue.Elem().Type()).Interface().(*T)
		if errFetch := t.byIDInternal(ctx, id, original); errFetch != nil { // byIDInternal now passes TTLs
			return fmt.Errorf("failed to fetch original record for update (ID: %d): %w", id, errFetch)
		}

		// Find changed fields using reflection helper (keys are DB column names)
		changedFields, err = findChangedFields(original, value) // Uses helper defined later
		if err != nil {
			return fmt.Errorf("failed to find changed fields for update: %w", err)
		}

		// If no fields changed (excluding UpdatedAt logic handled in findChangedFieldsAdvanced), skip DB update
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

			return nil // Lock action successful
		})
		// --- End Lock ---

		if err != nil {
			// Error occurred during lock acquisition or the action inside the lock
			return fmt.Errorf("save update failed (lock or db/cache exec): %w", err)
		}
	}

	baseModelPtr.SetNewRecordFlag(false)

	// --- Update Query Caches (Incremental) ---
	if isNew {
		t.updateAffectedQueryCaches(ctx, value, nil, isNew) // Call as method, pass nil for original
	} else {
		t.updateAffectedQueryCaches(ctx, value, original, isNew) // Call as method
	}

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

		// --- Object Cache Invalidate within Lock (Using ClearCacheByID) ---
		// Removed direct call: cacheKey := generateCacheKey(tableName, id)
		// Removed direct call: errCacheDel := t.cache.DeleteModel(lctx, cacheKey)
		// Removed logging for direct call

		// Call the centralized cache clearing method
		if clearErr := t.ClearCacheByID(lctx, id); clearErr != nil {
			// Log the error from ClearCacheByID, but don't fail the delete operation
			// ClearCacheByID already logs warnings, so just a note here might suffice.
			log.Printf("Notice: ClearCacheByID returned an error during delete for %s %d: %v", tableName, id, clearErr)
			// Decide if this should return an error, for now, it doesn't.
		}
		// --- End Object Cache Invalidate ---

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

	// --- Update Query Caches (Incremental) ---
	// Type assertion needed because value is interface{}
	if model, ok := value.(*T); ok {
		t.handleDeleteInQueryCaches(ctx, model) // Call as method
	} else {
		log.Printf("WARN: Could not assert type %T for incremental delete cache update in deleteInternal", value)
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

// ComparableFieldInfo holds pre-computed metadata for a single field used during comparisons.
type ComparableFieldInfo struct {
	GoName       string                     // Go field name
	DBColumn     string                     // Database column name
	Index        []int                      // Index for fast field access via FieldByIndex
	IsZero       func(v reflect.Value) bool // Function to check if value is zero/empty
	Kind         reflect.Kind               // Field kind (string, int, struct, etc.)
	Type         reflect.Type               // Field type (for more detailed type checking)
	IsEmbedded   bool                       // Whether this is from an embedded struct
	IgnoreInDiff bool                       // Whether to ignore this field during diffing (e.g., tags like db:"-")
}

// ModelInfo holds cached reflection results for a specific Model type.
// Renamed: modelInfo -> ModelInfo
type ModelInfo struct {
	TableName        string                // Renamed: tableName -> TableName
	pkName           string                // Database name of the primary key field (kept unexported for now)
	Columns          []string              // Renamed: columns -> Columns
	fields           []string              // Corresponding Go struct field names (order matches columns, kept unexported)
	fieldToColumnMap map[string]string     // Map Go field name to its corresponding DB column name (kept unexported)
	columnToFieldMap map[string]string     // Map DB column name to its corresponding Go field name (kept unexported)
	compareFields    []ComparableFieldInfo // Fields to compare during diff operations (new)
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
		compareFields:    make([]ComparableFieldInfo, 0),
	}
	pkDbName := ""

	numFields := modelType.NumField()
	info.Columns = make([]string, 0, numFields) // Using renamed field
	info.fields = make([]string, 0, numFields)

	var processFields func(structType reflect.Type, parentIndex []int, isEmbedded bool)
	processFields = func(structType reflect.Type, parentIndex []int, isEmbedded bool) {
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)
			dbTag := field.Tag.Get("db")
			diffTag := field.Tag.Get("diff") // Optional diff tag

			// If field is embedded struct, process its fields recursively
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				// For BaseModel, process fields directly since they're important
				if field.Type == reflect.TypeOf(BaseModel{}) {
					baseModelType := field.Type
					for j := 0; j < baseModelType.NumField(); j++ {
						baseField := baseModelType.Field(j)
						baseDbTag := baseField.Tag.Get("db")
						baseDiffTag := baseField.Tag.Get("diff") // Optional diff tag for BaseModel fields

						if baseDbTag == "-" || !baseField.IsExported() {
							continue
						}

						colName := baseDbTag
						if colName == "" {
							colName = strings.ToLower(baseField.Name)
						}

						// Build the field index by appending to the parent index
						fieldIndex := append(append([]int{}, parentIndex...), i, j)

						info.Columns = append(info.Columns, colName) // Using renamed field
						info.fields = append(info.fields, baseField.Name)
						info.fieldToColumnMap[baseField.Name] = colName
						info.columnToFieldMap[colName] = baseField.Name

						// Skip adding PK to comparable fields
						if colName == "id" {
							pkDbName = colName
							continue
						}

						// Skip fields marked to be ignored in diff
						if baseDiffTag == "-" {
							continue
						}

						// Add to compareFields if not explicitly ignored
						cfInfo := ComparableFieldInfo{
							GoName:       baseField.Name,
							DBColumn:     colName,
							Index:        fieldIndex,
							Kind:         baseField.Type.Kind(),
							Type:         baseField.Type,
							IsEmbedded:   true,
							IgnoreInDiff: false,
						}

						// Add specialized zero-value checker based on field type
						cfInfo.IsZero = getZeroChecker(baseField.Type)

						info.compareFields = append(info.compareFields, cfInfo)
					}
				} else {
					// Process other embedded structs recursively
					newIndex := append(append([]int{}, parentIndex...), i)
					processFields(field.Type, newIndex, true)
				}
				continue
			}

			// Skip unexported fields and fields marked with db:"-"
			if dbTag == "-" || !field.IsExported() {
				continue
			}

			colName := dbTag
			if colName == "" {
				// Skip fields without db tag
				continue
			}

			// Build the field index by appending to the parent index
			fieldIndex := append(append([]int{}, parentIndex...), i)

			info.Columns = append(info.Columns, colName) // Using renamed field
			info.fields = append(info.fields, field.Name)
			info.fieldToColumnMap[field.Name] = colName
			info.columnToFieldMap[colName] = field.Name

			if colName == "id" && pkDbName == "" {
				pkDbName = colName
				continue // Skip adding PK to comparable fields
			}

			// Skip fields marked to be ignored in diff
			if diffTag == "-" {
				continue
			}

			// Add to compareFields if not explicitly ignored
			cfInfo := ComparableFieldInfo{
				GoName:       field.Name,
				DBColumn:     colName,
				Index:        fieldIndex,
				Kind:         field.Type.Kind(),
				Type:         field.Type,
				IsEmbedded:   isEmbedded,
				IgnoreInDiff: false,
			}

			// Add specialized zero-value checker based on field type
			cfInfo.IsZero = getZeroChecker(field.Type)

			info.compareFields = append(info.compareFields, cfInfo)
		}
	}

	processFields(modelType, []int{}, false)

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

// getZeroChecker returns a function that checks if a value is the zero value for its type
func getZeroChecker(t reflect.Type) func(v reflect.Value) bool {
	switch t.Kind() {
	case reflect.Bool:
		return func(v reflect.Value) bool { return !v.Bool() }
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return func(v reflect.Value) bool { return v.Int() == 0 }
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return func(v reflect.Value) bool { return v.Uint() == 0 }
	case reflect.Float32, reflect.Float64:
		return func(v reflect.Value) bool { return v.Float() == 0 }
	case reflect.Complex64, reflect.Complex128:
		return func(v reflect.Value) bool { return v.Complex() == complex(0, 0) }
	case reflect.Array:
		return func(v reflect.Value) bool {
			// For arrays, check if all elements are zero
			for i := 0; i < v.Len(); i++ {
				if !reflect.DeepEqual(v.Index(i).Interface(), reflect.Zero(v.Index(i).Type()).Interface()) {
					return false
				}
			}
			return true
		}
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return func(v reflect.Value) bool { return v.IsNil() }
	case reflect.String:
		return func(v reflect.Value) bool { return v.String() == "" }
	case reflect.Struct:
		// Special case for time.Time
		if t == reflect.TypeOf(time.Time{}) {
			zeroTime := time.Time{}
			return func(v reflect.Value) bool {
				// Check if this is a zero time, which can be tricky due to location/monotonic data
				t := v.Interface().(time.Time)
				return t.IsZero() || t.Equal(zeroTime)
			}
		}
		// For other structs, use reflection to check if all fields are zero
		return func(v reflect.Value) bool {
			return reflect.DeepEqual(v.Interface(), reflect.Zero(t).Interface())
		}
	default:
		return func(v reflect.Value) bool {
			return reflect.DeepEqual(v.Interface(), reflect.Zero(t).Interface())
		}
	}
}

// --- Change Detection --- (Moved Down)

// findChangedFields compares two structs and returns a map of column names to changed values.
// It will always skip private fields, primary key fields, and fields tagged with `db:"-"`.
func findChangedFields[T any](original, updated *T) (map[string]interface{}, error) {
	// Get type information
	t := reflect.TypeOf(original).Elem()
	typename := t.Name()

	// Try to retrieve cached model info for this type
	info, _ := GetCachedModelInfo(t)

	// If we have cached model info with comparison metadata, use the optimized simple path
	if info != nil && len(info.compareFields) > 0 {
		startTime := time.Now()
		result, err := findChangedFieldsSimple(original, updated, info)
		if err == nil {
			if os.Getenv("DEBUG_DIFF") != "" {
				log.Printf("DEBUG: [%s] Simple comparison took %v for %d fields",
					typename, time.Since(startTime), len(info.compareFields))
			}
			return result, nil
		}
		// If simple comparison fails, fall back to reflection
		log.Printf("WARNING: Simple comparison failed for %s: %v. Falling back to reflection.",
			typename, err)
	}

	// Fall back to reflection-based approach if no cached info or simple comparison failed
	startTime := time.Now()
	result, err := findChangedFieldsReflection(original, updated, info)
	if os.Getenv("DEBUG_DIFF") != "" {
		log.Printf("DEBUG: [%s] Reflection comparison took %v", typename, time.Since(startTime))
	}
	return result, err
}

// findChangedFieldsReflection compares two structs (original and updated)
// and returns a map of column names to the changed values from the updated struct.
// DEPRECATED in favor of findChangedFieldsAdvanced once implemented and tested.
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

// findChangedFieldsAdvanced compares two structs (original and updated) using cached metadata
// and optimized comparison logic. Returns a map of column names to the changed values.
// DEPRECATED: Use findChangedFieldsSimple instead, which is more efficient for typical database models.
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
	return query.String(), args
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
				if qcErr := t.cache.SetQueryIDs(ctx, listCacheKey, relatedIDs, globalCacheTTL); qcErr != nil {
					log.Printf("WARN: Failed to cache query IDs for key %s: %v", listCacheKey, qcErr)
				}
			} else {
				log.Printf("Caching NoneResult for query key %s", listCacheKey)
				if qcErr := t.cache.Set(ctx, listCacheKey, NoneResult, globalCacheTTL); qcErr != nil {
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
	// REMOVED TTLs from call
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

// SoftDelete marks a record as deleted by setting the Deleted flag and updating
// the UpdatedAt timestamp. It then saves the changes.
func (t *Thing[T]) SoftDelete(model *T) error {
	if model == nil {
		return errors.New("cannot soft delete a nil model")
	}

	baseModelPtr := getBaseModelPtr(model)
	if baseModelPtr == nil {
		// This case might be less likely if model is *T, but good practice
		return errors.New("SoftDelete: model must embed BaseModel")
	}

	// Set Deleted flag and update timestamp
	baseModelPtr.Deleted = true
	baseModelPtr.UpdatedAt = time.Now()

	// Use Save to persist changes and handle cache invalidation
	// Note: saveInternal triggers BeforeSave/AfterSave but not Before/AfterDelete
	if err := t.saveInternal(t.ctx, model); err != nil {
		// If save fails, attempt to revert the flags in memory?
		// baseModelPtr.Deleted = false // Maybe? Or rely on caller to retry?
		return fmt.Errorf("SoftDelete failed during save: %w", err)
	}

	return nil
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

// Load explicitly loads relationships for a given model instance using the Thing's context.
// model must be a pointer to a struct of type T.
// relations are the string names of the fields representing the relationships to load.
func (t *Thing[T]) Load(model *T, relations ...string) error {
	// Use the context stored in the Thing instance
	return t.loadInternal(t.ctx, model, relations...)
}

// findChangedFieldsSimple is a simplified version of field comparison that focuses on
// common database column types rather than handling complex structures.
func findChangedFieldsSimple[T any](original, updated *T, info *ModelInfo) (map[string]interface{}, error) {
	changed := make(map[string]interface{})
	originalVal := reflect.ValueOf(original).Elem()
	updatedVal := reflect.ValueOf(updated).Elem()

	if originalVal.Type() != updatedVal.Type() {
		return nil, errors.New("original and updated values must be of the same type")
	}

	// Get the UpdatedAt field name for special handling later
	var updatedAtDBColName string
	updatedAtGoFieldName := "UpdatedAt"
	if col, ok := info.fieldToColumnMap[updatedAtGoFieldName]; ok {
		updatedAtDBColName = col
	}

	// Only enable detailed debug logging if DEBUG_DIFF environment variable is set
	debugEnabled := os.Getenv("DEBUG_DIFF") != ""

	// Use compareFields from cached metadata for efficient field comparison
	for _, field := range info.compareFields {
		// Use the pre-computed field index for fast access
		originalFieldVal := originalVal.FieldByIndex(field.Index)
		updatedFieldVal := updatedVal.FieldByIndex(field.Index)

		if !originalFieldVal.IsValid() || !updatedFieldVal.IsValid() {
			if debugEnabled {
				log.Printf("DEBUG: Field not valid, skipping: %s", field.GoName)
			}
			continue
		}

		// Simple comparison based on field type
		var fieldsEqual bool

		// Special handling for time.Time - this is common in DB models
		if field.Type == reflect.TypeOf(time.Time{}) {
			if !originalFieldVal.IsZero() && !updatedFieldVal.IsZero() {
				oTime := originalFieldVal.Interface().(time.Time)
				uTime := updatedFieldVal.Interface().(time.Time)
				fieldsEqual = oTime.Equal(uTime)
			} else {
				fieldsEqual = originalFieldVal.IsZero() == updatedFieldVal.IsZero()
			}
		} else {
			// For basic types, use simple equality check
			// This works for strings, numbers, booleans and other basic types
			fieldsEqual = originalFieldVal.Interface() == updatedFieldVal.Interface()
		}

		if !fieldsEqual {
			if debugEnabled {
				log.Printf("DEBUG: Change detected in %s: %v -> %v",
					field.GoName, originalFieldVal.Interface(), updatedFieldVal.Interface())
			}
			// Add the changed field to the result using its DB column name
			changed[field.DBColumn] = updatedFieldVal.Interface()
		}
	}

	// Handle UpdatedAt field specially
	if updatedAtDBColName != "" && len(changed) > 0 {
		if len(changed) == 1 {
			if _, isOnlyChange := changed[updatedAtDBColName]; isOnlyChange {
				if debugEnabled {
					log.Printf("DEBUG: Removing '%s' from changed map as it's the only change.", updatedAtDBColName)
				}
				delete(changed, updatedAtDBColName)
			}
		} else {
			// If UpdatedAt changed alongside other fields, ensure it's using the latest timestamp
			if _, ok := changed[updatedAtDBColName]; ok {
				// If UpdatedAt was detected as changed, ensure we use the *actual* updated value
				updatedFieldVal := updatedVal.FieldByName(updatedAtGoFieldName)
				if updatedFieldVal.IsValid() {
					changed[updatedAtDBColName] = updatedFieldVal.Interface()
					if debugEnabled {
						log.Printf("DEBUG: Ensuring '%s' uses the latest timestamp value in changed map.", updatedAtDBColName)
					}
				}
			}
		}
	}

	return changed, nil
}

// ClearCacheByID removes the cache entry for a specific model instance by its ID.
func (t *Thing[T]) ClearCacheByID(ctx context.Context, id int64) error {
	cacheKey := generateCacheKey(t.info.TableName, id) // Use the helper function
	log.Printf("CACHE DEL MODEL: Key: %s", cacheKey)
	err := t.cache.DeleteModel(ctx, cacheKey)
	if err != nil {
		// Log the error but don't necessarily fail the operation if cache delete fails
		log.Printf("Warning: Failed to delete model cache for key %s: %v", cacheKey, err)
		// Optionally return the error if cache clearing is critical
		// return fmt.Errorf("failed to clear cache for key %s: %w", cacheKey, err)
	}
	return nil // Or return err if you want to propagate cache errors
}

// --- Incremental Cache Update Methods ---

// updateAffectedQueryCaches is called after a Save operation
// to incrementally update relevant list and count caches.
// It's now a method of Thing[T].
func (t *Thing[T]) updateAffectedQueryCaches(ctx context.Context, model *T, originalModel *T, isCreate bool) {
	if t.cache == nil {
		return // No cache configured
	}
	baseModelPtr := getBaseModelPtr(model)
	if baseModelPtr == nil || baseModelPtr.ID == 0 {
		log.Printf("WARN: updateAffectedQueryCaches skipped for model without BaseModel or ID")
		return
	}

	id := baseModelPtr.ID
	tableName := t.info.TableName

	// 1. Get potentially affected query keys from the index
	queryCacheKeys := globalCacheIndex.GetPotentiallyAffectedQueries(tableName)
	if len(queryCacheKeys) == 0 {
		log.Printf("DEBUG: No query caches registered for table '%s', skipping incremental update for ID %d", tableName, id)
		return
	}

	log.Printf("DEBUG: Found %d potential query caches to update for table '%s' after change to ID %d", len(queryCacheKeys), tableName, id)

	// --- Phase 1: Gather Update Tasks ---
	type cacheUpdateTask struct {
		cacheKey    string
		queryParams QueryParams
		isListKey   bool
		isCountKey  bool
		needsAdd    bool
		needsRemove bool
	}
	tasks := make([]cacheUpdateTask, 0, len(queryCacheKeys))

	for _, cacheKey := range queryCacheKeys {
		isListKey := strings.HasPrefix(cacheKey, "list:")
		isCountKey := strings.HasPrefix(cacheKey, "count:")
		if !isListKey && !isCountKey {
			log.Printf("WARN: Skipping unknown cache key format in index: %s", cacheKey)
			continue
		}

		params, found := globalCacheIndex.GetQueryParamsForKey(cacheKey)
		if !found {
			log.Printf("WARN: QueryParams not found for registered cache key '%s'. Cannot perform incremental update.", cacheKey)
			continue
		}

		// Check if the model matches the query conditions
		matchesCurrent, matchesOriginal, err := t.checkModelMatchAgainstQuery(model, originalModel, params, isCreate)
		if err != nil {
			// Log the error and delete the specific cache key that failed the check
			log.Printf("ERROR CheckQueryMatch Failed: Query check failed for cache key '%s'. Deleting this cache entry due to error: %v", cacheKey, err)
			// Attempt to delete the specific key for this iteration
			if delErr := t.cache.Delete(ctx, cacheKey); delErr != nil && !errors.Is(delErr, ErrNotFound) {
				log.Printf("ERROR Failed to delete cache key '%s' after CheckQueryMatch error: %v", cacheKey, delErr)
			}
			continue // Skip further processing for this query cache
		}

		// Determine action
		needsAdd, needsRemove := determineCacheAction(isCreate, matchesOriginal, matchesCurrent, baseModelPtr.KeepItem())

		if needsAdd || needsRemove {
			log.Printf("DEBUG Gather Task (%s): Action determined: Add=%v, Remove=%v", cacheKey, needsAdd, needsRemove)
			tasks = append(tasks, cacheUpdateTask{
				cacheKey:    cacheKey,
				queryParams: params, // Store params for potential future use
				isListKey:   isListKey,
				isCountKey:  isCountKey,
				needsAdd:    needsAdd,
				needsRemove: needsRemove,
			})
		} else {
			log.Printf("DEBUG Gather Task (%s): No cache change needed.", cacheKey)
		}
	}

	if len(tasks) == 0 {
		log.Printf("DEBUG: No actual cache updates required for ID %d after checks.", id)
		return
	}

	log.Printf("DEBUG: Processing %d cache update tasks for ID %d.", len(tasks), id)

	// --- Phase 2: Simulated Batch Read ---
	initialListValues := make(map[string][]int64)
	initialCountValues := make(map[string]int64)
	readErrors := make(map[string]error) // Store errors encountered during read

	for _, task := range tasks {
		if task.isListKey {
			// Avoid reading again if already processed (map check handles this)
			if _, exists := initialListValues[task.cacheKey]; !exists && readErrors[task.cacheKey] == nil {
				cachedIDs, err := t.cache.GetQueryIDs(ctx, task.cacheKey)
				if err != nil && !errors.Is(err, ErrNotFound) {
					log.Printf("ERROR: Failed to read initial list cache for key %s: %v", task.cacheKey, err)
					readErrors[task.cacheKey] = err        // Record error
					initialListValues[task.cacheKey] = nil // Mark as errored/unreadable
				} else {
					if errors.Is(err, ErrNotFound) {
						log.Printf("DEBUG Read (%s): List cache key not found, treating as empty.", task.cacheKey)
						initialListValues[task.cacheKey] = []int64{} // Treat not found as empty list
					} else if cachedIDs == nil {
						log.Printf("WARN: GetQueryIDs returned nil error but nil slice for key %s. Treating as empty.", task.cacheKey)
						initialListValues[task.cacheKey] = []int64{}
					} else {
						log.Printf("DEBUG Read (%s): Read initial list with %d IDs.", task.cacheKey, len(cachedIDs))
						initialListValues[task.cacheKey] = cachedIDs
					}
				}
			}
		} else if task.isCountKey {
			if _, exists := initialCountValues[task.cacheKey]; !exists && readErrors[task.cacheKey] == nil {
				count, err := getCachedCount(ctx, t.cache, task.cacheKey) // Uses internal helper which handles NotFound
				if err != nil {
					// getCachedCount already logs specific errors and returns 0 for ErrNotFound
					log.Printf("ERROR: Failed to read initial count cache for key %s: %v", task.cacheKey, err)
					readErrors[task.cacheKey] = err        // Record error
					initialCountValues[task.cacheKey] = -1 // Indicate error state if needed, though getCachedCount returns 0 on error
				} else {
					log.Printf("DEBUG Read (%s): Read initial count: %d.", task.cacheKey, count)
					initialCountValues[task.cacheKey] = count
				}
			}
		}
	}

	// --- Phase 3: Compute Updates & Identify Changes ---
	type finalWriteTask struct {
		cacheKey  string
		newList   []int64 // nil if not a list update
		newCount  int64   // -1 if not a count update
		isListKey bool
	}
	writesNeeded := make(map[string]finalWriteTask) // Use map to handle potential duplicate keys from tasks list

	for _, task := range tasks {
		// Skip if there was a read error for this key
		if readErrors[task.cacheKey] != nil {
			log.Printf("WARN: Skipping update computation for key %s due to previous read error.", task.cacheKey)
			continue
		}

		if task.isListKey {
			initialIDs := initialListValues[task.cacheKey]
			if initialIDs == nil {
				// This case might happen if the key was processed multiple times and failed read earlier
				log.Printf("WARN: Skipping list update for %s, initial value missing (potentially due to earlier read error).", task.cacheKey)
				continue
			}

			var updatedIDs []int64
			changed := false
			if task.needsAdd {
				if !containsID(initialIDs, id) {
					// updatedIDs = append(initialIDs, id) // Simple append, order might not matter or needs sorting later if needed
					updatedIDs = addIDToListIfNotExists(initialIDs, id) // Use helper
					changed = true
					log.Printf("DEBUG Compute (%s): Adding ID %d. List size %d -> %d", task.cacheKey, id, len(initialIDs), len(updatedIDs))
				} else {
					updatedIDs = initialIDs // Already exists
					log.Printf("DEBUG Compute (%s): Skipping list add as ID %d already exists.", task.cacheKey, id)
				}
			} else { // needsRemove
				if len(initialIDs) > 0 && containsID(initialIDs, id) {
					updatedIDs = removeIDFromList(initialIDs, id)
					changed = true
					log.Printf("DEBUG Compute (%s): Removing ID %d. List size %d -> %d", task.cacheKey, id, len(initialIDs), len(updatedIDs))
				} else {
					updatedIDs = initialIDs // Not present or list empty
					log.Printf("DEBUG Compute (%s): Skipping list removal as ID %d not found or cache empty.", task.cacheKey, id)
				}
			}

			if changed {
				// Sort if order matters (e.g., based on query Params.Order) - COMPLEX, skip for now
				// Ensure consistent order if needed by sorting `updatedIDs` here.
				// sort.Slice(updatedIDs, func(i, j int) bool { return updatedIDs[i] < updatedIDs[j] }) // Example: sort ascending

				writesNeeded[task.cacheKey] = finalWriteTask{
					cacheKey:  task.cacheKey,
					newList:   updatedIDs,
					newCount:  -1, // Mark as not a count update
					isListKey: true,
				}
				log.Printf("DEBUG Compute (%s): Identified list write needed. New size: %d", task.cacheKey, len(updatedIDs))
			}

		} else if task.isCountKey {
			initialCount := initialCountValues[task.cacheKey]
			if initialCount == -1 { // Check for error state from read phase
				log.Printf("WARN: Skipping count update for %s, initial value indicates read error.", task.cacheKey)
				continue
			}

			newCount := initialCount
			changed := false
			if task.needsAdd {
				newCount++
				changed = true
				log.Printf("DEBUG Compute (%s): Incrementing count %d -> %d", task.cacheKey, initialCount, newCount)
			} else { // needsRemove
				if newCount > 0 {
					newCount--
					changed = true
					log.Printf("DEBUG Compute (%s): Decrementing count %d -> %d", task.cacheKey, initialCount, newCount)
				} else {
					log.Printf("DEBUG Compute (%s): Skipping count decrement as it's already zero.", task.cacheKey)
				}
			}

			if changed {
				writesNeeded[task.cacheKey] = finalWriteTask{
					cacheKey:  task.cacheKey,
					newList:   nil, // Mark as not a list update
					newCount:  newCount,
					isListKey: false,
				}
				log.Printf("DEBUG Compute (%s): Identified count write needed. New value: %d", task.cacheKey, newCount)
			}
		}
	}

	// --- Phase 4: Conditional Batch Write (with Locking) ---
	if len(writesNeeded) == 0 {
		log.Printf("DEBUG: No actual cache writes required for ID %d after computation.", id)
		return
	}

	log.Printf("DEBUG: Attempting to write %d cache updates for ID %d.", len(writesNeeded), id)
	locker := globalCacheKeyLocker // Assumes global locker exists

	for key, writeTask := range writesNeeded {
		// Lock the specific cache key before modifying
		log.Printf("DEBUG Write (%s): Acquiring lock...", key)
		locker.Lock(key) // Assume Lock doesn't return an error to check here
		log.Printf("DEBUG Write (%s): Acquired lock.", key)

		var writeErr error
		if writeTask.isListKey {
			writeErr = setCachedListIDs(ctx, t.cache, key, writeTask.newList, globalCacheTTL)
			if writeErr == nil {
				log.Printf("DEBUG Write (%s): Successfully updated cached ID list (size %d).", key, len(writeTask.newList))
			}
		} else { // isCountKey
			writeErr = setCachedCount(ctx, t.cache, key, writeTask.newCount, globalCacheTTL)
			if writeErr == nil {
				log.Printf("DEBUG Write (%s): Successfully updated cached count (%d).", key, writeTask.newCount)
			}
		}

		// Unlock the key regardless of write error
		log.Printf("DEBUG Write (%s): Releasing lock...", key)
		locker.Unlock(key) // Assume Unlock doesn't return an error to check here
		log.Printf("DEBUG Write (%s): Released lock.", key)

		if writeErr != nil {
			log.Printf("ERROR: Failed cache write for key %s: %v", key, writeErr)
			// Attempt to delete the potentially corrupted key to force refresh on next query
			deleteErr := t.cache.Delete(ctx, key) // Use generic Delete
			if deleteErr != nil {
				log.Printf("ERROR: Failed to delete potentially corrupted cache key %s after write error: %v", key, deleteErr)
			} else {
				log.Printf("INFO: Deleted potentially corrupted cache key %s after write error.", key)
			}
		}
	}
	log.Printf("DEBUG: Completed cache update processing for ID %d.", id)
}

// handleDeleteInQueryCaches is called after a Delete operation
// to incrementally update relevant list and count caches by removing the deleted item.
// It's now a method of Thing[T].
func (t *Thing[T]) handleDeleteInQueryCaches(ctx context.Context, model *T) {
	if t.cache == nil {
		return // No cache configured
	}
	baseModelPtr := getBaseModelPtr(model)
	if baseModelPtr == nil || baseModelPtr.ID == 0 {
		log.Printf("WARN: handleDeleteInQueryCaches skipped for model without BaseModel or ID")
		return
	}

	id := baseModelPtr.ID
	tableName := t.info.TableName

	// 1. Get potentially affected query keys from the index
	queryCacheKeys := globalCacheIndex.GetPotentiallyAffectedQueries(tableName)
	if len(queryCacheKeys) == 0 {
		log.Printf("DEBUG: No query caches registered for table '%s', skipping incremental delete update for ID %d", tableName, id)
		return
	}

	log.Printf("DEBUG: Found %d potential query caches to update for table '%s' after delete of ID %d", len(queryCacheKeys), tableName, id)

	// --- Phase 1: Gather Remove Tasks ---
	type cacheRemoveTask struct {
		cacheKey    string
		isListKey   bool
		isCountKey  bool
		wasMatching bool // Did the item match the query before deletion?
	}
	tasks := make([]cacheRemoveTask, 0, len(queryCacheKeys))

	for _, cacheKey := range queryCacheKeys {
		isListKey := strings.HasPrefix(cacheKey, "list:")
		isCountKey := strings.HasPrefix(cacheKey, "count:")
		if !isListKey && !isCountKey {
			log.Printf("WARN Delete Cache Update: Skipping unknown cache key format: %s", cacheKey)
			continue
		}

		params, found := globalCacheIndex.GetQueryParamsForKey(cacheKey)
		if !found {
			log.Printf("WARN Delete Cache Update: QueryParams not found for key '%s'. Skipping.", cacheKey)
			continue
		}

		// Check if the model *would have matched* the query before deletion
		// We pass the model itself, assuming CheckQueryMatch handles nil originalModel correctly
		matches, err := t.CheckQueryMatch(model, params)
		if err != nil {
			log.Printf("WARN Delete Cache Update: Error checking query match for deleted model (key: %s): %v. Skipping.", cacheKey, err)
			continue
		}

		if matches {
			log.Printf("DEBUG Delete Gather Task (%s): Deleted item matched query. Needs Remove.", cacheKey)
			tasks = append(tasks, cacheRemoveTask{
				cacheKey:    cacheKey,
				isListKey:   isListKey,
				isCountKey:  isCountKey,
				wasMatching: true,
			})
		} else {
			log.Printf("DEBUG Delete Gather Task (%s): Deleted item did not match query. No cache change needed.", cacheKey)
		}
	}

	if len(tasks) == 0 {
		log.Printf("DEBUG Delete Cache Update: No actual cache removals required for ID %d.", id)
		return
	}

	log.Printf("DEBUG Delete Cache Update: Processing %d cache removal tasks for ID %d.", len(tasks), id)

	// --- Phase 2: Simulated Batch Read ---
	initialListValues := make(map[string][]int64)
	initialCountValues := make(map[string]int64)
	readErrors := make(map[string]error)

	for _, task := range tasks {
		// Only read if it was matching (otherwise no update needed)
		if !task.wasMatching {
			continue
		}

		if task.isListKey {
			if _, exists := initialListValues[task.cacheKey]; !exists && readErrors[task.cacheKey] == nil {
				cachedIDs, err := t.cache.GetQueryIDs(ctx, task.cacheKey)
				if err != nil && !errors.Is(err, ErrNotFound) {
					log.Printf("ERROR Delete Cache Update: Failed read initial list for key %s: %v", task.cacheKey, err)
					readErrors[task.cacheKey] = err
					initialListValues[task.cacheKey] = nil
				} else {
					if errors.Is(err, ErrNotFound) {
						initialListValues[task.cacheKey] = []int64{}
					} else if cachedIDs == nil {
						initialListValues[task.cacheKey] = []int64{}
					} else {
						initialListValues[task.cacheKey] = cachedIDs
					}
					log.Printf("DEBUG Delete Read (%s): Read initial list (size %d).", task.cacheKey, len(initialListValues[task.cacheKey]))
				}
			}
		} else if task.isCountKey {
			if _, exists := initialCountValues[task.cacheKey]; !exists && readErrors[task.cacheKey] == nil {
				count, err := getCachedCount(ctx, t.cache, task.cacheKey)
				if err != nil {
					log.Printf("ERROR Delete Cache Update: Failed read initial count for key %s: %v", task.cacheKey, err)
					readErrors[task.cacheKey] = err
					initialCountValues[task.cacheKey] = -1
				} else {
					initialCountValues[task.cacheKey] = count
					log.Printf("DEBUG Delete Read (%s): Read initial count (%d).", task.cacheKey, count)
				}
			}
		}
	}

	// --- Phase 3: Compute Updates & Identify Changes ---
	type finalWriteTask struct {
		cacheKey  string
		newList   []int64 // nil if not a list update
		newCount  int64   // -1 if not a count update
		isListKey bool
	}
	writesNeeded := make(map[string]finalWriteTask)

	for _, task := range tasks {
		if !task.wasMatching || readErrors[task.cacheKey] != nil {
			continue // Skip if wasn't matching or read failed
		}

		if task.isListKey {
			initialIDs := initialListValues[task.cacheKey]
			if initialIDs == nil {
				continue
			} // Skip if read failed earlier

			if len(initialIDs) > 0 && containsID(initialIDs, id) {
				updatedIDs := removeIDFromList(initialIDs, id)
				log.Printf("DEBUG Delete Compute (%s): Removing ID %d. List size %d -> %d", task.cacheKey, id, len(initialIDs), len(updatedIDs))
				// Sort if needed
				writesNeeded[task.cacheKey] = finalWriteTask{
					cacheKey:  task.cacheKey,
					newList:   updatedIDs,
					newCount:  -1,
					isListKey: true,
				}
			} else {
				log.Printf("DEBUG Delete Compute (%s): Skipping list removal as ID %d not found or cache empty.", task.cacheKey, id)
			}
		} else if task.isCountKey {
			initialCount := initialCountValues[task.cacheKey]
			if initialCount == -1 {
				continue
			} // Skip if read failed earlier

			if initialCount > 0 {
				newCount := initialCount - 1
				log.Printf("DEBUG Delete Compute (%s): Decrementing count %d -> %d", task.cacheKey, initialCount, newCount)
				writesNeeded[task.cacheKey] = finalWriteTask{
					cacheKey:  task.cacheKey,
					newList:   nil,
					newCount:  newCount,
					isListKey: false,
				}
			} else {
				log.Printf("DEBUG Delete Compute (%s): Skipping count decrement as it's already zero.", task.cacheKey)
			}
		}
	}

	// --- Phase 4: Conditional Batch Write (with Locking) ---
	if len(writesNeeded) == 0 {
		log.Printf("DEBUG Delete Cache Update: No actual cache writes required for ID %d.", id)
		return
	}

	log.Printf("DEBUG Delete Cache Update: Attempting to write %d cache updates for ID %d.", len(writesNeeded), id)
	locker := globalCacheKeyLocker

	for key, writeTask := range writesNeeded {
		log.Printf("DEBUG Delete Write (%s): Acquiring lock...", key)
		locker.Lock(key) // Assume Lock doesn't return an error
		log.Printf("DEBUG Delete Write (%s): Acquired lock.", key)

		var writeErr error
		if writeTask.isListKey {
			writeErr = setCachedListIDs(ctx, t.cache, key, writeTask.newList, globalCacheTTL)
			if writeErr == nil {
				log.Printf("DEBUG Delete Write (%s): Successfully updated list (size %d).", key, len(writeTask.newList))
			}
		} else {
			writeErr = setCachedCount(ctx, t.cache, key, writeTask.newCount, globalCacheTTL)
			if writeErr == nil {
				log.Printf("DEBUG Delete Write (%s): Successfully updated count (%d).", key, writeTask.newCount)
			}
		}

		log.Printf("DEBUG Delete Write (%s): Releasing lock...", key)
		locker.Unlock(key) // Assume Unlock doesn't return an error
		log.Printf("DEBUG Delete Write (%s): Released lock.", key)

		if writeErr != nil {
			log.Printf("ERROR Delete Cache Update: Failed write for key %s: %v", key, writeErr)
			deleteErr := t.cache.Delete(ctx, key)
			if deleteErr != nil {
				log.Printf("ERROR Delete Cache Update: Failed delete potentially corrupted key %s: %v", key, deleteErr)
			} else {
				log.Printf("INFO Delete Cache Update: Deleted potentially corrupted key %s.", key)
			}
		}
	}
	log.Printf("DEBUG Delete Cache Update: Completed processing for ID %d.", id)
}

// --- Helper functions for list/count cache ---

// containsID checks if an ID exists in a slice of int64.
func containsID(ids []int64, id int64) bool {
	for _, currentID := range ids {
		if currentID == id {
			return true
		}
	}
	return false
}

// Helper function to check if a model matches query parameters.
// Returns: matchesCurrent, matchesOriginal, error
func (t *Thing[T]) checkModelMatchAgainstQuery(model *T, originalModel *T, params QueryParams, isCreate bool) (bool, bool, error) {
	var matchesCurrent, matchesOriginal bool
	var matchErr error

	// Check current model state
	matchesCurrent, matchErr = t.CheckQueryMatch(model, params)
	if matchErr != nil {
		return false, false, fmt.Errorf("error checking query match for current model: %w", matchErr)
	}

	// Check original model state (only relevant for updates)
	if !isCreate && originalModel != nil {
		matchesOriginal, matchErr = t.CheckQueryMatch(originalModel, params)
		if matchErr != nil {
			return false, false, fmt.Errorf("error checking query match for original model: %w", matchErr)
		}
	}
	return matchesCurrent, matchesOriginal, nil
}

// Helper function to determine cache action based on match status and deletion status.
// Returns: needsAdd, needsRemove
func determineCacheAction(isCreate, matchesOriginal, matchesCurrent bool, isKept bool) (bool, bool) {
	needsAdd := false
	needsRemove := false

	if !isKept {
		// If item is soft-deleted, it always needs removal (if it was previously matching)
		// and never needs adding.
		log.Printf("DEBUG Determine Action: Model is soft-deleted (KeepItem=false). Ensuring removal.")
		needsRemove = true // Ensure removal attempt
		needsAdd = false
	} else if isCreate {
		if matchesCurrent {
			needsAdd = true
			log.Printf("DEBUG Determine Action: Create matches query. Needs Add.")
		}
	} else { // Update
		if matchesCurrent && !matchesOriginal {
			needsAdd = true
			log.Printf("DEBUG Determine Action: Update now matches query (didn't before). Needs Add.")
		} else if !matchesCurrent && matchesOriginal {
			needsRemove = true
			log.Printf("DEBUG Determine Action: Update no longer matches query (did before). Needs Remove.")
		}
		// If match status didn't change (both true or both false), neither add nor remove is needed.
	}

	// If it needs adding, it cannot simultaneously need removing based on match status change.
	if needsAdd {
		needsRemove = false
	}

	return needsAdd, needsRemove
}
