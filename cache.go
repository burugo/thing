package thing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"thing/internal/cache" // Import internal cache package
	"thing/internal/utils"
	// "thing/internal/helpers" // Removed import
)

// --- Cache Constants ---
const (
	// Represents a non-existent entry in the cache
	NoneResult = "NoneResult"
	// Lock duration
	LockDuration   = 5 * time.Second
	LockRetryDelay = 50 * time.Millisecond
	LockMaxRetries = 5
)

// Global instance of the lock manager for cache keys.
// Defined here to avoid import cycles.
var GlobalCacheKeyLocker = NewCacheKeyLockManagerInternal()

// --- Cache Helpers ---

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

// invalidateAffectedQueryCaches handles both Save/Update and Delete cache invalidation in a unified way.
// If isDelete is true, only removal logic is performed; otherwise, add/remove is determined by model state.
func (t *Thing[T]) invalidateAffectedQueryCaches(ctx context.Context, model T, originalModel T, isCreate bool, isDelete bool) {
	if t.cache == nil {
		return // No cache configured
	}
	id := model.GetID()
	if id == 0 {
		log.Printf("WARN: invalidateAffectedQueryCaches skipped for model with zero ID")
		return
	}
	tableName := t.info.TableName

	// --- Use valueIndex/fieldIndex for more precise cache invalidation ---
	modelVal := reflect.ValueOf(model)
	if modelVal.Kind() == reflect.Ptr {
		modelVal = modelVal.Elem()
	}
	modelType := modelVal.Type()
	info, err := GetCachedModelInfo(modelType)
	if err != nil {
		log.Printf("ERROR: Failed to get model metadata for cache invalidation: %v", err)
		return
	}

	// Collect all field values for this model
	fieldValues := make(map[string]interface{})
	for _, f := range info.CompareFields {
		v := modelVal.FieldByIndex(f.Index)
		fieldValues[f.DBColumn] = v.Interface()
	}

	// Use valueIndex for exact match fields
	cacheKeySet := make(map[string]struct{})
	for col, val := range fieldValues {
		keys := cache.GlobalCacheIndex.GetKeysByValue(tableName, col, val)
		for _, k := range keys {
			cacheKeySet[k] = struct{}{}
		}
	}
	// Use fieldIndex for range/other queries
	if cache.GlobalCacheIndex != nil && cache.GlobalCacheIndex.FieldIndex != nil {
		for col := range fieldValues {
			if fieldMap, ok := cache.GlobalCacheIndex.FieldIndex[tableName]; ok {
				if keyMap, ok := fieldMap[col]; ok {
					for k := range keyMap {
						cacheKeySet[k] = struct{}{}
					}
				}
			}
		}
	}
	// Always union full-table list cache keys (where is empty)
	for _, k := range cache.GlobalCacheIndex.GetFullTableListKeys(tableName) {
		cacheKeySet[k] = struct{}{}
	}
	// Convert set to slice
	queryCacheKeys := make([]string, 0, len(cacheKeySet))
	for k := range cacheKeySet {
		queryCacheKeys = append(queryCacheKeys, k)
	}

	if len(queryCacheKeys) == 0 {
		log.Printf("DEBUG: No query caches registered for table '%s', skipping cache update for ID %d", tableName, id)
		return
	}

	log.Printf("DEBUG: Found %d potential query caches to update for table '%s' (isDelete=%v) for ID %d", len(queryCacheKeys), tableName, isDelete, id)

	// --- Phase 1: Gather Tasks ---
	type cacheTask struct {
		cacheKey    string
		queryParams cache.QueryParams
		isListKey   bool
		isCountKey  bool
		needsAdd    bool
		needsRemove bool
	}
	tasks := make([]cacheTask, 0, len(queryCacheKeys))

	for _, cacheKey := range queryCacheKeys {
		isListKey := strings.HasPrefix(cacheKey, "list:")
		isCountKey := strings.HasPrefix(cacheKey, "count:")
		if !isListKey && !isCountKey {
			log.Printf("WARN: Skipping unknown cache key format in index: %s", cacheKey)
			continue
		}

		paramsInternal, found := cache.GlobalCacheIndex.GetQueryParamsForKey(cacheKey)
		if !found {
			log.Printf("WARN: QueryParams not found for registered cache key '%s'. Cannot perform cache update.", cacheKey)
			continue
		}
		paramsRoot := cache.QueryParams{
			Where:    paramsInternal.Where,
			Args:     paramsInternal.Args,
			Order:    paramsInternal.Order,
			Preloads: paramsInternal.Preloads,
		}

		infoInternal := &cache.ModelInfo{
			TableName:        t.info.TableName,
			ColumnToFieldMap: t.info.ColumnToFieldMap,
		}

		if isDelete {
			// Only check if the model would have matched before deletion
			matches, err := cache.CheckQueryMatch(model, infoInternal, paramsRoot)
			if err != nil {
				log.Printf("WARN: Error checking query match for deleted model (key: %s): %v. Skipping.", cacheKey, err)
				continue
			}
			if matches {
				log.Printf("DEBUG Gather Task (%s): Deleted item matched query. Needs Remove.", cacheKey)
				tasks = append(tasks, cacheTask{
					cacheKey:    cacheKey,
					queryParams: paramsRoot,
					isListKey:   isListKey,
					isCountKey:  isCountKey,
					needsAdd:    false,
					needsRemove: true,
				})
			} else {
				log.Printf("DEBUG Gather Task (%s): Deleted item did not match query. No cache change needed.", cacheKey)
			}
		} else {
			// Save/Update: check both current and original model
			matchesCurrent, err := cache.CheckQueryMatch(model, infoInternal, paramsRoot)
			if err != nil {
				log.Printf("ERROR CheckQueryMatch Failed: Query check failed for cache key '%s'. Deleting this cache entry due to error: %v", cacheKey, err)
				if delErr := t.cache.Delete(ctx, cacheKey); delErr != nil && !errors.Is(delErr, ErrNotFound) {
					log.Printf("ERROR Failed to delete cache key '%s' after CheckQueryMatch error: %v", cacheKey, delErr)
				}
				continue
			}
			matchesOriginal := false
			if !isCreate && !reflect.ValueOf(originalModel).IsNil() {
				matchesOriginal, err = cache.CheckQueryMatch(originalModel, infoInternal, paramsRoot)
				if err != nil {
					log.Printf("ERROR CheckQueryMatch Failed: Query check failed for original model, cache key '%s'. Deleting this cache entry due to error: %v", cacheKey, err)
					if delErr := t.cache.Delete(ctx, cacheKey); delErr != nil && !errors.Is(delErr, ErrNotFound) {
						log.Printf("ERROR Failed to delete cache key '%s' after CheckQueryMatch error: %v", cacheKey, delErr)
					}
					continue
				}
			}
			needsAdd, needsRemove := determineCacheAction(isCreate, matchesOriginal, matchesCurrent, model.KeepItem())
			if needsAdd || needsRemove {
				log.Printf("DEBUG Gather Task (%s): Action determined: Add=%v, Remove=%v", cacheKey, needsAdd, needsRemove)
				tasks = append(tasks, cacheTask{
					cacheKey:    cacheKey,
					queryParams: paramsRoot,
					isListKey:   isListKey,
					isCountKey:  isCountKey,
					needsAdd:    needsAdd,
					needsRemove: needsRemove,
				})
			} else {
				log.Printf("DEBUG Gather Task (%s): No cache change needed.", cacheKey)
			}
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
	readErrors := make(map[string]error)

	for _, task := range tasks {
		if !task.needsAdd && !task.needsRemove {
			continue
		}
		if task.isListKey {
			if _, exists := initialListValues[task.cacheKey]; !exists && readErrors[task.cacheKey] == nil {
				cachedIDs, err := t.cache.GetQueryIDs(ctx, task.cacheKey)
				if err != nil && !errors.Is(err, ErrNotFound) {
					log.Printf("ERROR: Failed to read initial list cache for key %s: %v", task.cacheKey, err)
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
					log.Printf("DEBUG Read (%s): Read initial list (size %d).", task.cacheKey, len(initialListValues[task.cacheKey]))
				}
			}
		} else if task.isCountKey {
			if _, exists := initialCountValues[task.cacheKey]; !exists && readErrors[task.cacheKey] == nil {
				countStr, err := t.cache.Get(ctx, task.cacheKey)
				var count int64
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						count = 0
						err = nil
					} else {
						log.Printf("ERROR: Failed to read initial count cache for key %s: %v", task.cacheKey, err)
						readErrors[task.cacheKey] = err
						initialCountValues[task.cacheKey] = -1
						continue
					}
				} else if countStr == "" {
					count = 0
				} else {
					parsedCount, parseErr := strconv.ParseInt(countStr, 10, 64)
					if parseErr != nil {
						log.Printf("ERROR: Failed to parse count cache value '%s' for key %s: %v", countStr, task.cacheKey, parseErr)
						readErrors[task.cacheKey] = parseErr
						initialCountValues[task.cacheKey] = -1
						continue
					} else {
						count = parsedCount
					}
				}
				initialCountValues[task.cacheKey] = count
				log.Printf("DEBUG Read (%s): Read initial count (%d).", task.cacheKey, count)
			}
		}
	}

	// --- Phase 3: Compute Updates & Identify Changes ---
	type finalWriteTask struct {
		cacheKey  string
		newList   []int64
		newCount  int64
		isListKey bool
	}
	writesNeeded := make(map[string]finalWriteTask)

	for _, task := range tasks {
		if readErrors[task.cacheKey] != nil {
			continue
		}
		if task.isListKey {
			initialIDs := initialListValues[task.cacheKey]
			if initialIDs == nil {
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
				changed = true
				log.Printf("DEBUG Compute (%s): Needs Remove (due to query mismatch, soft delete, or delete op). Marking changed.", task.cacheKey)
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
			if initialCount == -1 {
				continue
			}
			newCount := initialCount
			changed := false
			if task.needsAdd {
				newCount++
				changed = true
				log.Printf("DEBUG Compute (%s): Incrementing count %d -> %d", task.cacheKey, initialCount, newCount)
			} else if task.needsRemove {
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
					newList:   nil,
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
	locker := GlobalCacheKeyLocker

	for key, writeTask := range writesNeeded {
		log.Printf("DEBUG Write (%s): Acquiring lock...", key)
		locker.Lock(key)
		log.Printf("DEBUG Write (%s): Acquired lock.", key)

		var writeErr error
		if writeTask.isListKey {
			log.Printf("DEBUG Write (%s): Invalidating list cache due to computed change (Add/Remove/Delete).", key)
			writeErr = t.cache.Delete(ctx, key)
			if writeErr == nil {
				log.Printf("DEBUG Write (%s): Successfully invalidated list cache.", key)
			} else if errors.Is(writeErr, ErrNotFound) {
				log.Printf("DEBUG Write (%s): List cache key not found during invalidation (already gone?).", key)
				writeErr = nil
			}
		} else {
			countStr := strconv.FormatInt(writeTask.newCount, 10)
			writeErr = t.cache.Set(ctx, key, countStr, globalCacheTTL)
			if writeErr == nil {
				log.Printf("DEBUG Write (%s): Successfully updated cached count (%d).", key, writeTask.newCount)
			}
		}

		log.Printf("DEBUG Write (%s): Releasing lock...", key)
		locker.Unlock(key)
		log.Printf("DEBUG Write (%s): Released lock.", key)

		if writeErr != nil {
			log.Printf("ERROR: Failed cache write for key %s: %v", key, writeErr)
			deleteErr := t.cache.Delete(ctx, key)
			if deleteErr != nil {
				log.Printf("ERROR: Failed to delete potentially corrupted cache key %s after write error: %v", key, deleteErr)
			} else {
				log.Printf("INFO: Deleted potentially corrupted cache key %s after write error.", key)
			}
		}
	}
	log.Printf("DEBUG: Completed cache update processing for ID %d.", id)
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

// --- Internal Cache Key Generation ---

// GenerateQueryHash generates a unique hash for a given query.
func GenerateQueryHash(params cache.QueryParams) string {
	// Normalize and marshal params to JSON
	paramsJson, err := json.Marshal(params)
	if err != nil {
		log.Printf("ERROR: Failed to marshal query params for hash: %v", err)
		return fmt.Sprintf("error_hash_%d", time.Now().UnixNano()) // Fallback
	}

	// Generate SHA-256 hash
	hasher := sha256.New()
	hasher.Write(paramsJson)
	return hex.EncodeToString(hasher.Sum(nil))
}

// GenerateCacheKey generates a cache key for list or count queries with normalized arguments.
// prefix should be either "list" or "count". This ensures consistent cache key generation across the codebase.
func GenerateCacheKey(prefix, tableName string, params cache.QueryParams) string {
	// Normalize args for consistent hashing
	normalizedParams := params
	normalizedArgs := make([]interface{}, len(params.Args))
	for i, arg := range params.Args {
		normalizedArgs[i] = utils.NormalizeValue(arg)
	}
	normalizedParams.Args = normalizedArgs

	hash := GenerateQueryHash(normalizedParams)
	return fmt.Sprintf("%s:%s:%s", prefix, tableName, hash)
}
