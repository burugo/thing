package thing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/burugo/thing/common"
	"github.com/burugo/thing/internal/interfaces"
	"github.com/burugo/thing/internal/schema"
	"github.com/burugo/thing/internal/utils"
)

// Import internal cache package

// --- Constants used internally ---
const (
	ByIDBatchSize = 100 // Size of batches for fetching by ID from DB
)

// --- Core Internal CRUD & Fetching Logic ---

// fetchModelsByIDsInternal is the core logic for fetching models by their primary keys,
// handling cache checks, database queries for misses, and caching results.
// It requires the concrete modelType to instantiate objects and slices correctly.
// REMOVED TTL arguments
func fetchModelsByIDsInternal(ctx context.Context, cache interfaces.CacheClient, db interfaces.DBAdapter, modelInfo *schema.ModelInfo, modelType reflect.Type, ids []int64) (map[int64]reflect.Value, error) {
	resultMap := make(map[int64]reflect.Value)
	if len(ids) == 0 {
		return resultMap, nil
	}

	// Ensure modelType is a pointer type (e.g., *User)
	if modelType.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("fetchModelsByIDsInternal: modelType must be a pointer to struct, got %s", modelType.Kind())
	}

	missingIDs := []int64{} // Initialize explicitly

	// 1. Try fetching from cache
	if cache != nil {
		for _, id := range ids { // Iterate through original ids
			cacheKey := generateCacheKey(modelInfo.TableName, id)
			instanceVal := reflect.New(modelType.Elem()).Elem() // User
			instancePtr := instanceVal.Addr().Interface()       // *User
			err := cache.GetModel(ctx, cacheKey, instancePtr)

			if err == nil {
				// Found in cache
				setNewRecordFlagIfBaseModel(instancePtr, false)
				resultMap[id] = reflect.ValueOf(instancePtr)   // Store the pointer
				log.Printf("DEBUG CACHE HIT for %s", cacheKey) // Added log
			} else if errors.Is(err, common.ErrCacheNoneResult) {
				// Found NoneResult marker - this ID is handled, DO NOT add to missingIDs.
				log.Printf("DEBUG CACHE HIT (NoneResult) for %s", cacheKey) // Added log
			} else if errors.Is(err, common.ErrNotFound) {
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
		query := fmt.Sprintf("%s WHERE \"%s\" IN (%s)",
			db.Builder().BuildSelectSQL(modelInfo.TableName, modelInfo.Columns), // Use db.Builder()
			modelInfo.PkName,
			placeholders)

		// Create a slice of the concrete type (not pointer) for scanning
		sliceType := reflect.SliceOf(modelType.Elem()) // []User
		sliceVal := reflect.New(sliceType).Elem()

		err := db.Select(ctx, sliceVal.Addr().Interface(), query, args...)
		if err != nil {
			// Log error but continue processing potentially found results
			log.Printf("Error fetching batch %v-%v for type %s: %v", i, end-1, modelInfo.TableName, err)
			continue // Continue to next batch if any
		}

		// Add fetched models to result map and cache them
		for j := 0; j < sliceVal.Len(); j++ {
			modelVal := sliceVal.Index(j)           // User
			modelPtr := modelVal.Addr().Interface() // *User
			model, ok := modelPtr.(Model)
			if !ok {
				log.Printf("WARN: Fetched model of type %s does not implement Model interface, cannot get ID", modelInfo.TableName)
				continue
			}

			id := model.GetID()
			if id == 0 {
				log.Printf("WARN: Fetched model %s has zero ID", modelInfo.TableName)
				continue
			}

			setNewRecordFlagIfBaseModel(modelPtr, false)
			resultMap[id] = reflect.ValueOf(modelPtr) // Store the pointer

			// Cache the model
			if cache != nil {
				cacheKey := generateCacheKey(modelInfo.TableName, id)
				if errCache := cache.SetModel(ctx, cacheKey, modelPtr, globalCacheTTL); errCache != nil { // USE globalCacheTTL
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
					errCacheSet := cache.Set(ctx, cacheKey, common.NoneResult, globalCacheTTL) // USE globalCacheTTL
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
func (t *Thing[T]) byIDInternal(ctx context.Context, id int64, dest *T) error {
	if id <= 0 {
		return errors.New("invalid ID provided (must be > 0)")
	}

	// Fetch using the batch function (handles cache logic internally)
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	idsToFetch := []int64{id}

	resultsMap, err := fetchModelsByIDsInternal(ctx, t.cache, t.db, t.info, modelType, idsToFetch)
	if err != nil {
		// Propagate the error from fetchModelsByIDsInternal
		return fmt.Errorf("failed to fetch model by ID %d: %w", id, err)
	}

	// Check if the requested ID was found in the results
	if modelVal, ok := resultsMap[id]; ok {
		// Assign the found model to dest
		if typedModel, ok := modelVal.Interface().(T); ok {
			*dest = typedModel
			return nil
		} else {
			return fmt.Errorf("type assertion failed for model ID %d", id)
		}
	} else {
		// ID not found in resultsMap, implies it wasn't in DB or cache (or marked NoneResult)
		return common.ErrNotFound
	}
}

// saveInternal handles both creating and updating records.
func (t *Thing[T]) saveInternal(ctx context.Context, value T) error {
	if t.db == nil || t.cache == nil {
		return errors.New("Thing not properly initialized with DBAdapter and CacheClient")
	}

	if reflect.ValueOf(value).IsNil() {
		return errors.New("saveInternal: value (model pointer) is nil")
	}

	modelValue := reflect.ValueOf(value)
	if modelValue.Kind() != reflect.Ptr || modelValue.IsNil() {
		return errors.New("value must be a non-nil pointer")
	}

	// --- Prepare state ---
	id := value.GetID()
	isNew := id == 0
	now := time.Now()
	// Set the internal flag *before* hooks are called
	setNewRecordFlagIfBaseModel(value, isNew)

	// --- Trigger BeforeSave hook ---
	if err := triggerEvent(ctx, EventTypeBeforeSave, value, nil); err != nil { // Uses helper defined later
		return fmt.Errorf("BeforeSave hook failed: %w", err)
	}

	var query string
	var args []interface{}
	var err error
	var result sql.Result
	var changedFields map[string]interface{} // Only used for update
	var original T                           // Use T, not *T

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
		for _, fieldName := range t.info.Fields { // Use exported Fields
			colName := t.info.FieldToColumnMap[fieldName] // Use exported FieldToColumnMap
			// Skip PK column during insert (assuming auto-increment)
			if colName == t.info.PkName { // Use exported PkName
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

		// Add UpdatedAt specifically if not already included (it should be unless skipped by tag)
		updatedAtCol, updatedAtExists := t.info.FieldToColumnMap["UpdatedAt"]
		if updatedAtExists && !sliceContains(colsToInsert, updatedAtCol) {
			updatedAtField := modelValue.Elem().FieldByName("UpdatedAt")
			if updatedAtField.IsValid() {
				colsToInsert = append(colsToInsert, updatedAtCol)
				placeholders = append(placeholders, "?")
				vals = append(vals, updatedAtField.Interface())
			} else {
				colsToInsert = append(colsToInsert, updatedAtCol)
				placeholders = append(placeholders, "?")
				vals = append(vals, now) // Fallback
			}
		}

		if len(colsToInsert) == 0 {
			return errors.New("no columns to insert")
		}

		query = t.builder.BuildInsertSQL(t.info.TableName, colsToInsert)
		args = vals

		// Execute the INSERT query
		result, err = t.db.Exec(ctx, query, args...)

	} else {
		// --- UPDATE Path ---
		// Fetch the original record to compare against (bypass cache)
		// We need the original state to correctly update query caches incrementally.
		original = utils.NewPtr[T]()
		// Now original is a non-nil pointer of type T
		err = t.db.Get(ctx, original, fmt.Sprintf("%s WHERE \"%s\" = ?", t.builder.BuildSelectSQL(t.info.TableName, t.info.Columns), t.info.PkName), id) // Use exported PkName
		if err != nil {
			// If not found, use a non-nil zero value pointer for original
			original = utils.NewPtr[T]() // Ensure original is a non-nil pointer
			setUpdatedAtTimestamp(value, now)
			changedFields, err = findChangedFieldsSimple[T](&original, utils.ToPtr(value), t.info)
			if err != nil {
				return fmt.Errorf("failed to find changed fields: %w", err)
			}
			// Proceed with update as if all fields changed (or skip, depending on policy)
		} else {
			setUpdatedAtTimestamp(value, now)
			changedFields, err = findChangedFieldsSimple[T](&original, utils.ToPtr(value), t.info)
			if err != nil {
				return fmt.Errorf("failed to find changed fields: %w", err)
			}
		}

		if len(changedFields) == 0 {
			log.Printf("No fields changed for %s ID %d, skipping update.", t.info.TableName, id)
			return nil // Nothing to update
		}

		// Build UPDATE query
		setClauses := []string{}
		vals := []interface{}{}
		for col, val := range changedFields {
			setClauses = append(setClauses, fmt.Sprintf("%s = ?", col))
			vals = append(vals, val)
		}

		// Ensure ID is non-zero for update
		if id == 0 {
			return errors.New("cannot update record with zero ID")
		}

		vals = append(vals, id) // Add ID for WHERE clause

		query = t.builder.BuildUpdateSQL(t.info.TableName, setClauses, t.info.PkName)
		args = vals

		// Execute the UPDATE query
		result, err = t.db.Exec(ctx, query, args...)
	}

	// --- Handle DB Error ---
	if err != nil {
		log.Printf("ERROR: Failed to execute save operation for %s: %v", t.info.TableName, err)
		return fmt.Errorf("database save operation failed: %w", err)
	}

	// --- Update Model State (ID, NewRecord) ---
	if isNew {
		lastID, errID := result.LastInsertId()
		if errID != nil {
			log.Printf("WARN: Could not get LastInsertId for %s: %v", t.info.TableName, errID)
			// Consider returning error? Or just log?
		} else {
			// Use reflection to set ID if needed
			if setter, ok := any(value).(interface{ SetID(int64) }); ok {
				setter.SetID(lastID)
			}
		}
	}
	// Use reflection to set NewRecord flag if needed
	if setter, ok := any(value).(interface{ SetNewRecordFlag(bool) }); ok {
		setter.SetNewRecordFlag(false)
	}

	// --- Update Cache ---
	if t.cache != nil {
		// Invalidate/Update the single object cache
		cacheKey := generateCacheKey(t.info.TableName, value.GetID())

		// Use a lock to prevent race conditions during cache update
		lockKey := cacheKey + ":lock"
		errLock := withLock(ctx, t.cache, lockKey, func(ctx context.Context) error {
			// Re-set the model in the cache with the latest data and TTL
			if errCache := t.cache.SetModel(ctx, cacheKey, value, globalCacheTTL); errCache != nil { // USE globalCacheTTL
				log.Printf("WARN: Failed to update cache for %s after save: %v", cacheKey, errCache)
				// Don't return error, DB succeeded, log is sufficient for cache warn
			}
			return nil // Lock action successful
		})

		if errLock != nil {
			log.Printf("WARN: Failed to acquire lock for cache update %s: %v", lockKey, errLock)
			// Log warning, but don't fail the whole save operation
		}

		// --- Update Query Caches (Incremental) ---
		t.invalidateAffectedQueryCaches(ctx, value, original, isNew, false)
		// --- End Update Query Caches ---
	}

	// --- Trigger AfterSave/AfterCreate Hooks ---
	if isNew {
		if err := triggerEvent(ctx, EventTypeAfterCreate, value, nil); err != nil { // Uses helper defined later
			log.Printf("WARN: AfterCreate hook failed: %v", err)
		}
	}
	if err := triggerEvent(ctx, EventTypeAfterSave, value, changedFields); err != nil { // Uses helper defined later
		log.Printf("WARN: AfterSave hook failed: %v", err)
	}

	return nil // Success
}

// deleteInternal handles deleting records.
func (t *Thing[T]) deleteInternal(ctx context.Context, value T) error {
	if t.db == nil || t.cache == nil {
		return errors.New("Thing not properly initialized with DBAdapter and CacheClient")
	}

	id := value.GetID()
	if id == 0 {
		return errors.New("deleteInternal: cannot delete record with zero ID")
	}

	tableName := t.info.TableName // Get table name from cached info

	// --- Trigger BeforeDelete hook ---
	if err := triggerEvent(ctx, EventTypeBeforeDelete, value, nil); err != nil { // Uses helper defined later
		return fmt.Errorf("BeforeDelete hook failed: %w", err)
	}

	// --- DB and Cache Deletion (within a lock) ---
	cacheKey := generateCacheKey(tableName, id)
	lockKey := cacheKey + ":lock"

	err := withLock(ctx, t.cache, lockKey, func(ctx context.Context) error {
		// --- DB Delete ---
		query := t.builder.BuildDeleteSQL(tableName, t.info.PkName)
		result, err := t.db.Exec(ctx, query, id)
		if err != nil {
			return fmt.Errorf("database delete failed for %s %d: %w", tableName, id, err)
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			// Row didn't exist in DB, still need to clean up cache.
			// But signal that the record wasn't found in the first place.
			// We'll clear cache outside the error check.
			log.Printf("DEBUG DB DELETE: Record %s %d not found.", tableName, id)
			// return ErrNotFound // Don't return yet, clear cache first
		}

		// --- Object Cache Invalidate ---
		if t.cache != nil {
			// Delete the primary cache entry for this object
			if errCache := t.cache.Delete(ctx, cacheKey); errCache != nil && !errors.Is(errCache, common.ErrNotFound) {
				// Log warning but don't fail the DB delete
				log.Printf("WARN: Failed to delete cache key %s during delete operation: %v", cacheKey, errCache)
			}
		}
		// --- End Object Cache Invalidate ---

		// If we got here, DB delete was attempted. Check if it actually deleted something.
		if rowsAffected == 0 {
			return common.ErrNotFound // Now return NotFound if DB didn't affect rows
		}

		// --- Incremental Query Cache Update for Delete ---
		// Requires access to the global index or passing it in.
		// Placeholder call, assumes handleDeleteInQueryCaches exists on Thing
		t.invalidateAffectedQueryCaches(ctx, value, value, false, true)
		return nil // Success
	})

	if err != nil {
		// Don't trigger AfterDelete hook if the lock or DB/Cache operation failed
		if errors.Is(err, common.ErrNotFound) {
			log.Printf("Attempted to delete non-existent record %s %d", tableName, id)
			return common.ErrNotFound // Propagate not found error
		}
		return fmt.Errorf("delete operation failed (lock or db/cache exec): %w", err)
	}

	// --- Trigger AfterDelete hook (only if lock and DB/Cache action succeeded) ---
	if errHook := triggerEvent(ctx, EventTypeAfterDelete, value, nil); errHook != nil { // Uses helper defined later
		log.Printf("WARN: AfterDelete hook failed: %v", errHook)
	}

	return nil // Success
}

// sliceContains checks if a string slice contains a specific string.
func sliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ByID fetches a single model by its ID.
func (t *Thing[T]) ByID(id int64) (T, error) {
	var dest T
	err := t.byIDInternal(t.ctx, id, &dest)
	return dest, err
}

// Save creates or updates a record in the database.
func (t *Thing[T]) Save(value T) error {
	return t.saveInternal(t.ctx, value) // value is already *User (T)
}

// SoftDelete performs a soft delete on the record by setting the 'deleted' flag to true
// and updating the 'updated_at' timestamp. It uses saveInternal to persist only these changes.
func (t *Thing[T]) SoftDelete(value T) error {
	id := value.GetID()
	if id == 0 {
		return errors.New("SoftDelete: cannot soft delete record with zero ID")
	}

	// --- Trigger BeforeSoftDelete hook ---
	if err := triggerEvent(t.ctx, EventTypeBeforeSoftDelete, value, nil); err != nil {
		return fmt.Errorf("BeforeSoftDelete hook failed: %w", err)
	}

	// Mark for soft deletion
	now := time.Now()
	// Set Deleted = true and UpdatedAt = now using reflection
	val := reflect.ValueOf(value).Elem()
	deletedField := val.FieldByName("Deleted")
	if deletedField.IsValid() && deletedField.CanSet() {
		deletedField.SetBool(true)
	} else {
		return errors.New("SoftDelete: could not set Deleted field via reflection")
	}
	updatedAtField := val.FieldByName("UpdatedAt")
	if updatedAtField.IsValid() && updatedAtField.CanSet() {
		updatedAtField.Set(reflect.ValueOf(now))
	} else {
		log.Printf("WARN: SoftDelete could not set UpdatedAt via reflection for %s ID %d", t.info.TableName, id)
	}

	// Call saveInternal - it will detect only Deleted and UpdatedAt changed
	err := t.saveInternal(t.ctx, value)
	if err != nil {
		return fmt.Errorf("SoftDelete failed during save operation: %w", err)
	}

	// --- Trigger AfterSoftDelete hook (only if save succeeded) ---
	if errHook := triggerEvent(t.ctx, EventTypeAfterSoftDelete, value, nil); errHook != nil {
		log.Printf("WARN: AfterSoftDelete hook failed: %v", errHook)
	}

	return nil // Success
}

// Delete performs a hard delete on the record from the database.
func (t *Thing[T]) Delete(value T) error {
	// Call the internal hard delete logic
	return t.deleteInternal(t.ctx, value)
}

// ByIDs retrieves multiple records by their primary keys and optionally preloads relations.
func (t *Thing[T]) ByIDs(ids []int64, preloads ...string) (map[int64]T, error) {
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	resultsMapReflect, err := fetchModelsByIDsInternal(t.ctx, t.cache, t.db, t.info, modelType, ids)
	if err != nil {
		return nil, fmt.Errorf("ByIDs failed during internal fetch: %w", err)
	}

	// Convert map[int64]reflect.Value (containing T) to map[int64]T
	resultsMapTyped := make(map[int64]T, len(resultsMapReflect))
	// Also collect results in a slice for preloading
	resultsSliceForPreload := make([]T, 0, len(resultsMapReflect))
	for id, modelVal := range resultsMapReflect {
		if typedModel, ok := modelVal.Interface().(T); ok {
			resultsMapTyped[id] = typedModel
			resultsSliceForPreload = append(resultsSliceForPreload, typedModel)
		} else {
			log.Printf("WARN: ByIDs: Could not assert type for ID %d", id)
			log.Printf("DEBUG: modelVal.Interface() type: %T, reflect.TypeOf((*T)(nil)).Elem(): %v", modelVal.Interface(), reflect.TypeOf((*T)(nil)).Elem())
		}
	}

	// Apply preloads if requested
	if len(preloads) > 0 && len(resultsSliceForPreload) > 0 {
		for _, preloadName := range preloads {
			if preloadErr := t.preloadRelations(t.ctx, resultsSliceForPreload, preloadName); preloadErr != nil {
				log.Printf("WARN: ByIDs: failed to apply preload '%s': %v", preloadName, preloadErr)
			}
		}
	}

	return resultsMapTyped, nil
}
