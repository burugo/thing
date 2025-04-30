package thing

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"thing/internal/cache" // Import internal cache package
)

// Global instance of the lock manager for cache keys.
// Defined here to avoid import cycles.
var GlobalCacheKeyLocker = NewCacheKeyLockManagerInternal()

// --- Cache Helpers ---

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

// ClearCacheByID removes the cache entry for a specific model instance by its ID.
// Note: This is now a Thing[T] method.
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
	queryCacheKeys := cache.GlobalCacheIndex.GetPotentiallyAffectedQueries(tableName)
	if len(queryCacheKeys) == 0 {
		log.Printf("DEBUG: No query caches registered for table '%s', skipping incremental update for ID %d", tableName, id)
		return
	}

	log.Printf("DEBUG: Found %d potential query caches to update for table '%s' after change to ID %d", len(queryCacheKeys), tableName, id)

	// --- Phase 1: Gather Update Tasks ---
	type cacheUpdateTask struct {
		cacheKey    string
		queryParams cache.QueryParams
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

		paramsInternal, found := cache.GlobalCacheIndex.GetQueryParamsForKey(cacheKey)
		if !found {
			log.Printf("WARN: QueryParams not found for registered cache key '%s'. Cannot perform incremental update.", cacheKey)
			continue
		}
		// Convert internal QueryParams back to root QueryParams for the wrapper method call
		paramsRoot := cache.QueryParams{
			Where:    paramsInternal.Where,
			Args:     paramsInternal.Args,
			Order:    paramsInternal.Order,
			Preloads: paramsInternal.Preloads,
		}

		// Check if the model matches the query conditions using the wrapper method
		matchesCurrent, matchesOriginal, err := t.checkModelMatchAgainstQuery(model, originalModel, paramsRoot, isCreate)
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
				queryParams: paramsRoot, // Store root params in the task for consistency?
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
				// Inline GetCachedCount logic
				countStr, err := t.cache.Get(ctx, task.cacheKey)
				var count int64
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						count = 0 // Not found is not an error here, count is 0
						err = nil // Clear the error
					} else {
						log.Printf("ERROR: Failed to read initial count cache for key %s: %v", task.cacheKey, err)
						readErrors[task.cacheKey] = err        // Record error
						initialCountValues[task.cacheKey] = -1 // Indicate error state
						continue
					}
				} else if countStr == "" {
					count = 0 // Handle empty string case
				} else {
					parsedCount, parseErr := strconv.ParseInt(countStr, 10, 64)
					if parseErr != nil {
						log.Printf("ERROR: Failed to parse count cache value '%s' for key %s: %v", countStr, task.cacheKey, parseErr)
						readErrors[task.cacheKey] = parseErr   // Record error
						initialCountValues[task.cacheKey] = -1 // Indicate error state
						continue
					} else {
						count = parsedCount
					}
				}

				log.Printf("DEBUG Read (%s): Read initial count: %d.", task.cacheKey, count)
				initialCountValues[task.cacheKey] = count
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

			changed := false
			if task.needsAdd {
				if !containsID(initialIDs, id) {
					changed = true
					log.Printf("DEBUG Compute (%s): Needs Add - Adding ID %d. Marking changed.", task.cacheKey, id)
				} else {
					log.Printf("DEBUG Compute (%s): Skipping list add as ID %d already exists. No change needed.", task.cacheKey, id)
				}
			} else if task.needsRemove {
				// If removal is needed (either due to query mismatch OR KeepItem=false),
				// we always need to invalidate the cache, regardless of whether the ID
				// was found in the potentially stale initial read.
				changed = true
				log.Printf("DEBUG Compute (%s): Needs Remove (due to query mismatch or soft delete). Marking changed.", task.cacheKey)
			}

			if changed {
				writesNeeded[task.cacheKey] = finalWriteTask{
					cacheKey:  task.cacheKey,
					newCount:  -1,
					isListKey: true,
				}
				log.Printf("DEBUG Compute (%s): Identified list invalidation needed.", task.cacheKey)
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
	locker := GlobalCacheKeyLocker // Use locker defined in this package

	for key, writeTask := range writesNeeded {
		// Lock the specific cache key before modifying
		log.Printf("DEBUG Write (%s): Acquiring lock...", key)
		locker.Lock(key) // Assume Lock doesn't return an error to check here
		log.Printf("DEBUG Write (%s): Acquired lock.", key)

		var writeErr error
		if writeTask.isListKey {
			// Reverted Optimization for Update: Always invalidate list cache if a change (add/remove) is needed.
			// The optimization (checking if ID exists first) is only applied in handleDeleteInQueryCaches.
			log.Printf("DEBUG Write (%s): Invalidating list cache due to computed change (Add/Remove).", key)
			writeErr = t.cache.Delete(ctx, key)
			// writeErr = t.cache.SetQueryIDs(ctx, key, writeTask.newList, globalCacheTTL) // Keep commented
			if writeErr == nil {
				log.Printf("DEBUG Write (%s): Successfully invalidated list cache.", key)
			} else if errors.Is(writeErr, ErrNotFound) {
				// Not finding the key when trying to delete is okay.
				log.Printf("DEBUG Write (%s): List cache key not found during invalidation (already gone?).", key)
				writeErr = nil // Treat as success
			}
		} else { // isCountKey
			// Inline SetCachedCount logic
			countStr := strconv.FormatInt(writeTask.newCount, 10)
			writeErr = t.cache.Set(ctx, key, countStr, globalCacheTTL)
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
	queryCacheKeys := cache.GlobalCacheIndex.GetPotentiallyAffectedQueries(tableName)
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

		paramsInternal, found := cache.GlobalCacheIndex.GetQueryParamsForKey(cacheKey)
		if !found {
			log.Printf("WARN Delete Cache Update: QueryParams not found for key '%s'. Skipping.", cacheKey)
			continue
		}

		// Check if the model *would have matched* the query before deletion
		// Need to call the internal cache.CheckQueryMatch function now
		infoInternal := &cache.ModelInfo{
			TableName:        t.info.TableName,
			ColumnToFieldMap: t.info.ColumnToFieldMap,
		}
		matches, err := cache.CheckQueryMatch(model, infoInternal, paramsInternal) // Call internal func with internal params
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
				// Inline GetCachedCount logic
				countStr, err := t.cache.Get(ctx, task.cacheKey)
				var count int64
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						count = 0 // Not found is not an error here, count is 0
						err = nil // Clear the error
					} else {
						log.Printf("ERROR Delete Cache Update: Failed read initial count for key %s: %v", task.cacheKey, err)
						readErrors[task.cacheKey] = err        // Record error
						initialCountValues[task.cacheKey] = -1 // Indicate error state
						continue
					}
				} else if countStr == "" {
					count = 0 // Handle empty string case
				} else {
					parsedCount, parseErr := strconv.ParseInt(countStr, 10, 64)
					if parseErr != nil {
						log.Printf("ERROR Delete Cache Update: Failed to parse count cache value '%s' for key %s: %v", countStr, task.cacheKey, parseErr)
						readErrors[task.cacheKey] = parseErr   // Record error
						initialCountValues[task.cacheKey] = -1 // Indicate error state
						continue
					} else {
						count = parsedCount
					}
				}

				initialCountValues[task.cacheKey] = count
				log.Printf("DEBUG Delete Read (%s): Read initial count (%d).", task.cacheKey, count)
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
				log.Printf("DEBUG Delete Compute (%s): ID %d found. List invalidation will be attempted.", task.cacheKey, id)
				// Mark that a write *might* be needed (Phase 4 checks again)
				writesNeeded[task.cacheKey] = finalWriteTask{
					cacheKey:  task.cacheKey,
					newCount:  -1,
					isListKey: true,
				}
			} else {
				log.Printf("DEBUG Delete Compute (%s): Skipping list invalidation as ID %d not found or cache empty.", task.cacheKey, id)
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
	locker := GlobalCacheKeyLocker // Use locker defined in this package

	for key, writeTask := range writesNeeded {
		log.Printf("DEBUG Delete Write (%s): Acquiring lock...", key)
		locker.Lock(key) // Assume Lock doesn't return an error
		log.Printf("DEBUG Delete Write (%s): Acquired lock.", key)

		var writeErr error
		if writeTask.isListKey {
			// REVERTED OPTIMIZATION: Always invalidate if the deleted item matched the query.
			// The check (currentIDs, readErr := ...) and shouldDelete logic is removed.
			log.Printf("DEBUG Delete Write (%s): Invalidating list cache because deleted item matched query.", key)
			writeErr = t.cache.Delete(ctx, key)
			if writeErr == nil {
				log.Printf("DEBUG Delete Write (%s): Successfully invalidated list cache.", key)
			} else if errors.Is(writeErr, ErrNotFound) {
				log.Printf("DEBUG Delete Write (%s): List cache key not found during invalidation (already gone?).", key)
				writeErr = nil // Treat as success
			}
		} else {
			// Inline SetCachedCount logic
			countStr := strconv.FormatInt(writeTask.newCount, 10)
			writeErr = t.cache.Set(ctx, key, countStr, globalCacheTTL)
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
// This now acts as a wrapper calling the internal cache version.
func (t *Thing[T]) checkModelMatchAgainstQuery(model *T, originalModel *T, params cache.QueryParams, isCreate bool) (bool, bool, error) {
	var matchesCurrent, matchesOriginal bool
	var matchErr error

	// Convert root QueryParams to internal QueryParams
	paramsInternal := cache.QueryParams{
		Where:    params.Where,
		Args:     params.Args,
		Order:    params.Order,
		Preloads: params.Preloads,
	}
	// Convert root ModelInfo to internal ModelInfo
	infoInternal := &cache.ModelInfo{
		TableName:        t.info.TableName,
		ColumnToFieldMap: t.info.ColumnToFieldMap,
		// Add other needed fields if internal CheckQueryMatch requires them
	}

	// Check current model state using internal function
	matchesCurrent, matchErr = cache.CheckQueryMatch(model, infoInternal, paramsInternal)
	if matchErr != nil {
		return false, false, fmt.Errorf("error checking query match for current model: %w", matchErr)
	}

	// Check original model state using internal function
	if !isCreate && originalModel != nil {
		matchesOriginal, matchErr = cache.CheckQueryMatch(originalModel, infoInternal, paramsInternal)
		if matchErr != nil {
			return false, false, fmt.Errorf("error checking query match for original model: %w", matchErr)
		}
	}
	return matchesCurrent, matchesOriginal, nil
}

// determineCacheAction determines cache add/remove actions.
// REMAINS in root package as it doesn't directly depend on internal types
func determineCacheAction(isCreate, matchesOriginal, matchesCurrent bool, isKept bool) (bool, bool) {
	needsAdd := false
	needsRemove := false

	if !isKept {
		// If item is soft-deleted, it always needs removal from standard caches.
		log.Printf("DEBUG Determine Action: Model is soft-deleted (KeepItem=false). Needs Removal.")
		needsRemove = true
		needsAdd = false
		// Return early, soft-delete removal takes precedence
		return needsAdd, needsRemove
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
	// This check is now redundant because the !isKept case returns early.
	// if needsAdd {
	// 	needsRemove = false
	// }

	return needsAdd, needsRemove
}

// --- Internal Locker Implementation (Copied from internal/cache/cache_locker.go) ---
// This is defined here to avoid import cycles.

// CacheKeyLockManagerInternal manages a map of mutexes, one for each cache key.
// It uses sync.Map for efficient concurrent access.
type CacheKeyLockManagerInternal struct {
	locks sync.Map // map[string]*sync.Mutex
}

// NewCacheKeyLockManagerInternal creates a new lock manager.
func NewCacheKeyLockManagerInternal() *CacheKeyLockManagerInternal {
	return &CacheKeyLockManagerInternal{}
}

// Lock acquires the mutex associated with the given cache key.
// If the mutex does not exist, it is created.
// This operation blocks until the lock is acquired.
func (m *CacheKeyLockManagerInternal) Lock(key string) {
	if key == "" {
		return
	}
	mutex, _ := m.locks.LoadOrStore(key, &sync.Mutex{})
	log.Printf("DEBUG: Acquiring lock for key '%s'", key)
	mutex.(*sync.Mutex).Lock()
	log.Printf("DEBUG: Acquired lock for key '%s'", key)
}

// Unlock releases the mutex associated with the given cache key.
func (m *CacheKeyLockManagerInternal) Unlock(key string) {
	if key == "" {
		return
	}
	if mutex, ok := m.locks.Load(key); ok {
		mutex.(*sync.Mutex).Unlock()
		log.Printf("DEBUG: Released lock for key '%s'", key)
	} else {
		log.Printf("WARN: Attempted to Unlock a key ('%s') that was not locked or doesn't exist.", key)
	}
}
