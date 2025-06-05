package thing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
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

	"github.com/burugo/thing/common" // Added import for common errors/constants
	"github.com/burugo/thing/internal/cache"
	"github.com/burugo/thing/internal/schema" // Import internal cache package
	"github.com/burugo/thing/internal/utils"
	// "thing/internal/helpers" // Removed import
)

func init() {
	gob.Register(time.Time{})
}

// --- Cache Constants ---
// Represents a non-existent entry in the cache - moved to common/errors.go
// const NoneResult = "NoneResult" // Migrated

// Lock duration constants for cache locking
const (
	LockDuration   = 5 * time.Second
	LockRetryDelay = 50 * time.Millisecond
	LockMaxRetries = 5
)

// Global instance of the lock manager for cache keys.
// Defined here to avoid import cycles.
// var GlobalCacheKeyLocker = NewCacheKeyLockManagerInternal()

// --- Cache Helpers ---

// WithLock acquires a lock, executes the action, and releases the lock.
func WithLock(ctx context.Context, cache CacheClient, lockKey string, action func(ctx context.Context) error) error {
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
			log.Printf("Context canceled while waiting for lock: %s", lockKey)
			return ctx.Err()
		case <-time.After(LockRetryDelay):
			// Retry
		}
	}
	if !acquired {
		return fmt.Errorf("failed to acquire lock for key '%s' after %d retries: %w", lockKey, LockMaxRetries, common.ErrLockNotAcquired)
	}
	defer func() {
		releaseErr := cache.ReleaseLock(ctx, lockKey)
		if releaseErr != nil {
			log.Printf("Warning: Failed to release lock '%s': %v", lockKey, releaseErr)
		}
	}()
	return action(ctx)
}

// GenerateQueryHash generates a unique hash for a given query.
func GenerateQueryHash(params QueryParams) string {
	paramsJson, err := json.Marshal(params)
	if err != nil {
		log.Printf("ERROR: Failed to marshal query params for hash: %v", err)
		return fmt.Sprintf("error_hash_%d", time.Now().UnixNano())
	}
	hasher := sha256.New()
	hasher.Write(paramsJson)
	fullHash := hex.EncodeToString(hasher.Sum(nil))
	// Return only first 8 characters for shorter cache keys (32-bit hex = 8 chars)
	if len(fullHash) >= 8 {
		return fullHash[:8]
	}
	return fullHash
}

// GenerateCacheKey generates a cache key for list or count queries with normalized arguments.
func GenerateCacheKey(prefix, tableName string, params QueryParams) string {
	normalizedParams := params
	normalizedArgs := make([]interface{}, len(params.Args))
	for i, arg := range params.Args {
		normalizedArgs[i] = utils.NormalizeValue(arg)
	}
	normalizedParams.Args = normalizedArgs

	hash := GenerateQueryHash(normalizedParams)
	return fmt.Sprintf("%s:%s:%s", prefix, tableName, hash)
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

// --- Helper structs for cache update tasks (moved out of generic function for Go 1.18 compatibility) ---
type cacheTask struct {
	cacheKey    string
	queryParams QueryParams
	isListKey   bool
	isCountKey  bool
	needsAdd    bool
	needsRemove bool
}

type finalWriteTask struct {
	cacheKey  string
	newList   []int64
	newCount  int64
	isListKey bool
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
	info, err := schema.GetCachedModelInfo(modelType)
	if err != nil {
		log.Printf("ERROR: Failed to get model metadata for cache invalidation: %v", err)
		return
	}

	// Collect all field values for this model
	fieldValues := make(map[string]interface{})
	for _, f := range info.CompareFields {
		if f.IgnoreInDiff || f.DBColumn == info.PkName {
			continue
		}
		// log.Printf("[DEBUG] cache.go: field=%s, Index=%v, modelVal.Type=%v", f.DBColumn, f.Index, modelVal.Type())
		if modelVal.Kind() != reflect.Struct || len(f.Index) == 0 {
			// log.Printf("WARN: field %s has empty index, cannot check for match", f.GoName)
			continue
		}
		if f.Index[0] >= modelVal.NumField() {
			// log.Printf("WARN: field %s has Index[0] out of bounds. Index=%v, modelNumFields=%d", f.GoName, f.Index, modelVal.NumField())
			continue
		}

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
	// Always union full-table count cache keys (where is empty)
	for _, k := range cache.GlobalCacheIndex.GetFullTableCountKeys(tableName) {
		cacheKeySet[k] = struct{}{}
	}
	// Convert set to slice
	queryCacheKeys := make([]string, 0, len(cacheKeySet))
	for k := range cacheKeySet {
		queryCacheKeys = append(queryCacheKeys, k)
	}

	if len(queryCacheKeys) == 0 {
		// log.Printf("DEBUG: No query caches registered for table '%s', skipping cache update for ID %d", tableName, id)
		return
	}

	// log.Printf("DEBUG: Found %d potential query caches to update for table '%s' (isDelete=%v) for ID %d", len(queryCacheKeys), tableName, isDelete, id)

	// --- Phase 1: Gather Tasks ---
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
		paramsRoot := QueryParams{
			Where:    paramsInternal.Where,
			Args:     paramsInternal.Args,
			Order:    paramsInternal.Order,
			Preloads: paramsInternal.Preloads,
		}

		if isDelete {
			// Only check if the model would have matched before deletion
			matches, err := cache.CheckQueryMatch(model, t.info.TableName, t.info.ColumnToFieldMap, toInternalQueryParams(paramsRoot))
			if err != nil {
				log.Printf("WARN: Error checking query match for deleted model (key: %s): %v. Skipping.", cacheKey, err)
				continue
			}
			if matches {
				// log.Printf("DEBUG Gather Task (%s): Deleted item matched query. Needs Remove.", cacheKey)
				tasks = append(tasks, cacheTask{
					cacheKey:    cacheKey,
					queryParams: paramsRoot,
					isListKey:   isListKey,
					isCountKey:  isCountKey,
					needsAdd:    false,
					needsRemove: true,
				})
			} else {
				// log.Printf("DEBUG Gather Task (%s): Deleted item did not match query. No cache change needed.", cacheKey)
			}
		} else {
			// Save/Update: check both current and original model
			matchesCurrent, err := cache.CheckQueryMatch(model, t.info.TableName, t.info.ColumnToFieldMap, toInternalQueryParams(paramsRoot))
			if err != nil {
				log.Printf("ERROR CheckQueryMatch Failed: Query check failed for cache key '%s'. Deleting this cache entry due to error: %v", cacheKey, err)
				if delErr := t.cache.Delete(ctx, cacheKey); delErr != nil && !errors.Is(delErr, common.ErrNotFound) {
					log.Printf("ERROR Failed to delete cache key '%s' after CheckQueryMatch error: %v", cacheKey, delErr)
				}
				continue
			}
			matchesOriginal := false
			originalReflect := reflect.ValueOf(originalModel)
			if !isCreate && originalReflect.Kind() == reflect.Ptr && !originalReflect.IsNil() {
				matchesOriginal, err = cache.CheckQueryMatch(originalModel, t.info.TableName, t.info.ColumnToFieldMap, toInternalQueryParams(paramsRoot))
				if err != nil {
					log.Printf("ERROR CheckQueryMatch Failed: Query check failed for original model, cache key '%s'. Deleting this cache entry due to error: %v", cacheKey, err)
					if delErr := t.cache.Delete(ctx, cacheKey); delErr != nil && !errors.Is(delErr, common.ErrNotFound) {
						log.Printf("ERROR Failed to delete cache key '%s' after CheckQueryMatch error: %v", cacheKey, delErr)
					}
					continue
				}
			}
			needsAdd, needsRemove := determineCacheAction(isCreate, matchesOriginal, matchesCurrent, model.KeepItem())
			if needsAdd || needsRemove {
				// log.Printf("DEBUG Gather Task (%s): Action determined: Add=%v, Remove=%v", cacheKey, needsAdd, needsRemove)
				tasks = append(tasks, cacheTask{
					cacheKey:    cacheKey,
					queryParams: paramsRoot,
					isListKey:   isListKey,
					isCountKey:  isCountKey,
					needsAdd:    needsAdd,
					needsRemove: needsRemove,
				})
			} else {
				// log.Printf("DEBUG Gather Task (%s): No cache change needed.", cacheKey)
			}
		}
	}

	if len(tasks) == 0 {
		// log.Printf("DEBUG: No actual cache updates required for ID %d after checks.", id)
		return
	}

	// log.Printf("DEBUG: Processing %d cache update tasks for ID %d.", len(tasks), id)

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
				if err != nil && !errors.Is(err, common.ErrNotFound) {
					log.Printf("ERROR: Failed to read initial list cache for key %s: %v", task.cacheKey, err)
					readErrors[task.cacheKey] = err
					initialListValues[task.cacheKey] = nil
				} else {
					switch {
					case errors.Is(err, common.ErrNotFound):
						initialListValues[task.cacheKey] = []int64{}
					case cachedIDs == nil:
						initialListValues[task.cacheKey] = []int64{}
					default:
						initialListValues[task.cacheKey] = cachedIDs
					}
					// log.Printf("DEBUG Read (%s): Read initial list (size %d).", task.cacheKey, len(initialListValues[task.cacheKey]))
				}
			}
		} else if task.isCountKey {
			if _, exists := initialCountValues[task.cacheKey]; !exists && readErrors[task.cacheKey] == nil {
				countStr, err := t.cache.Get(ctx, task.cacheKey)
				var count int64
				switch {
				case err != nil && errors.Is(err, common.ErrNotFound):
					count = 0
				case err != nil:
					log.Printf("ERROR: Failed to read initial count cache for key %s: %v", task.cacheKey, err)
					readErrors[task.cacheKey] = err
					initialCountValues[task.cacheKey] = -1
					continue
				case countStr == "":
					count = 0
				default:
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
				// log.Printf("DEBUG Read (%s): Read initial count (%d).", task.cacheKey, count)
			}
		}
	}

	// --- Phase 3: Compute Updates & Identify Changes ---
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
					// log.Printf("DEBUG Compute (%s): Needs Add - Adding ID %d. Marking changed.", task.cacheKey, id)
				} else {
					// log.Printf("DEBUG Compute (%s): Skipping list add as ID %d already exists. No change needed.", task.cacheKey, id)
				}
			} else if task.needsRemove {
				changed = true
				// log.Printf("DEBUG Compute (%s): Needs Remove (due to query mismatch, soft delete, or delete op). Marking changed.", task.cacheKey)
			}
			if changed {
				writesNeeded[task.cacheKey] = finalWriteTask{
					cacheKey:  task.cacheKey,
					newCount:  -1,
					isListKey: true,
				}
				// log.Printf("DEBUG Compute (%s): Identified list invalidation needed.", task.cacheKey)
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
				// log.Printf("DEBUG Compute (%s): Incrementing count %d -> %d", task.cacheKey, initialCount, newCount)
			} else if task.needsRemove {
				if newCount > 0 {
					newCount--
					changed = true
					// log.Printf("DEBUG Compute (%s): Decrementing count %d -> %d", task.cacheKey, initialCount, newCount)
				} else {
					// log.Printf("DEBUG Compute (%s): Skipping count decrement as it's already zero.", task.cacheKey)
				}
			}
			if changed {
				writesNeeded[task.cacheKey] = finalWriteTask{
					cacheKey:  task.cacheKey,
					newList:   nil,
					newCount:  newCount,
					isListKey: false,
				}
				// log.Printf("DEBUG Compute (%s): Identified count write needed. New value: %d", task.cacheKey, newCount)
			}
		}
	}

	// --- Phase 4: Conditional Batch Write (with Locking) ---
	if len(writesNeeded) == 0 {
		// log.Printf("DEBUG: No actual cache writes required for ID %d after computation.", id)
		return
	}

	// log.Printf("DEBUG: Attempting to write %d cache updates for ID %d.", len(writesNeeded), id)
	locker := cache.GlobalCacheKeyLocker

	for key, writeTask := range writesNeeded {
		// log.Printf("DEBUG Write (%s): Acquiring lock...", key)
		locker.Lock(key)
		// log.Printf("DEBUG Write (%s): Acquired lock.", key)

		var writeErr error
		if writeTask.isListKey {
			// log.Printf("DEBUG Write (%s): Invalidating list cache due to computed change (Add/Remove/Delete).", key)
			writeErr = t.cache.Delete(ctx, key)
			if writeErr == nil {
				// log.Printf("DEBUG Write (%s): Successfully invalidated list cache.", key)
			} else if errors.Is(writeErr, common.ErrNotFound) {
				// log.Printf("DEBUG Write (%s): List cache key not found during invalidation (already gone?).", key)
				writeErr = nil
			}
		} else {
			countStr := strconv.FormatInt(writeTask.newCount, 10)
			writeErr = t.cache.Set(ctx, key, countStr, globalCacheTTL)
			if writeErr == nil {
				// log.Printf("DEBUG Write (%s): Successfully updated cached count (%d).", key, writeTask.newCount)
			}
		}

		// log.Printf("DEBUG Write (%s): Releasing lock...", key)
		locker.Unlock(key)
		// log.Printf("DEBUG Write (%s): Released lock.", key)

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
	// log.Printf("DEBUG: Completed cache update processing for ID %d.", id)
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

	switch {
	case !isKept:
		// If item is soft-deleted, it always needs removal from standard caches.
		// log.Printf("DEBUG Determine Action: Model is soft-deleted (KeepItem=false). Needs Removal.")
		needsRemove = true
		needsAdd = false
		// Return early, soft-delete removal takes precedence
		return needsAdd, needsRemove
	case isCreate:
		if matchesCurrent {
			needsAdd = true
			// log.Printf("DEBUG Determine Action: Create matches query. Needs Add.")
		}
	default:
		// Update
		if matchesCurrent && !matchesOriginal {
			needsAdd = true
			// log.Printf("DEBUG Determine Action: Update now matches query (didn't before). Needs Add.")
		} else if !matchesCurrent && matchesOriginal {
			needsRemove = true
			// log.Printf("DEBUG Determine Action: Update no longer matches query (did before). Needs Remove.")
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
	// log.Printf("DEBUG: Acquiring lock for key '%s'", key)
	mutex.(*sync.Mutex).Lock()
	// log.Printf("DEBUG: Acquired lock for key '%s'", key)
}

// Unlock releases the mutex associated with the given cache key.
func (m *CacheKeyLockManagerInternal) Unlock(key string) {
	if key == "" {
		return
	}
	if mutex, ok := m.locks.Load(key); ok {
		mutex.(*sync.Mutex).Unlock()
		// log.Printf("DEBUG: Released lock for key '%s'", key)
	} else {
		log.Printf("WARN: Attempted to Unlock a key ('%s') that was not locked or doesn't exist.", key)
	}
}

// CacheStats returns cache operation statistics for monitoring and hit/miss analysis.
func (t *Thing[T]) CacheStats(ctx context.Context) CacheStats {
	return t.cache.GetCacheStats(ctx)
}

// --- Default Local Cache Implementation ---

// ChannelLock implements a mutex using channel semantics
type ChannelLock struct {
	ch chan struct{}
}

// NewChannelLock creates a new channel-based lock
func NewChannelLock() *ChannelLock {
	return &ChannelLock{ch: make(chan struct{}, 1)}
}

// localCache implements CacheClient using in-memory sync.Map.
type localCache struct {
	store      sync.Map   // map[string]string
	lockPool   sync.Map   // map[string]*ChannelLock
	counters   sync.Map   // map[string]int, now each counter is a separate key
	countersMu sync.Mutex // protects counters increment
}

// DefaultLocalCache is the default in-memory cache client for Thing ORM.
var DefaultLocalCache CacheClient = &localCache{}

func (m *localCache) Get(ctx context.Context, key string) (string, error) {
	m.incrCounter("Get")
	if v, ok := m.store.Load(key); ok {
		if s, ok := v.(string); ok {
			m.incrCounter("GetHit")
			return s, nil
		}
	}
	m.incrCounter("GetMiss")
	return "", common.ErrNotFound
}

func (m *localCache) Set(ctx context.Context, key, value string, expiration time.Duration) error {
	m.incrCounter("Set")
	if value == common.NoneResult {
		if v, ok := m.store.Load(key); ok {
			if s, ok := v.(string); ok && s == common.NoneResult {
				return nil
			}
		}
		m.incrCounter("SetNoneResult")
	}
	m.store.Store(key, value)
	return nil
}

func (m *localCache) Delete(ctx context.Context, key string) error {
	m.incrCounter("Delete")
	m.store.Delete(key)
	return nil
}

func (m *localCache) Exists(key string) bool {
	m.incrCounter("Exists")
	_, ok := m.store.Load(key)
	return ok
}

func (m *localCache) GetModel(ctx context.Context, key string, dest interface{}) error {
	m.incrCounter("GetModel")

	raw, ok := m.store.Load(key)
	if !ok {
		m.incrCounter("GetModelMiss")
		return common.ErrNotFound
	}

	// Assuming raw is []byte from Gob encoding
	b, ok := raw.([]byte)
	if !ok {
		m.incrCounter("GetModelError")
		log.Printf("ERROR: localCache.GetModel: value for key '%s' is not []byte, but %T", key, raw)
		return fmt.Errorf("localCache: cached value for key '%s' is not []byte (%T)", key, raw)
	}

	// Unmarshal using Gob (similar to unmarshalFromMockWithGob or redis client)
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() {
		return fmt.Errorf("localCache.GetModel: dest must be a non-nil pointer, got %T", dest)
	}
	destElem := destVal.Elem()
	// We expect destElem to be a struct for SetModel/GetModel typically.
	// If it's not, direct Gob decode might work for simple types (e.g. GetCount)

	var dataMap map[string]interface{}
	buf := bytes.NewBuffer(b)
	decoder := gob.NewDecoder(buf)
	if err := decoder.Decode(&dataMap); err != nil {
		// Fallback: if not a map, try direct decode (e.g. for int64 from GetCount)
		log.Printf("WARN: localCache.GetModel: failed to decode Gob to map for key '%s', trying direct decode. Error: %v", key, err)
		buf.Reset()
		buf.Write(b)
		decoder = gob.NewDecoder(buf) // New decoder for the reset buffer
		if directDecodeErr := decoder.Decode(dest); directDecodeErr != nil {
			log.Printf("ERROR: localCache.GetModel: direct Gob decode also failed for key '%s': %v", key, directDecodeErr)
			return fmt.Errorf("localCache: Gob Unmarshal error for key '%s' (map decode error: %v, direct decode error: %w)", key, err, directDecodeErr)
		}
		// Direct decode successful
		m.incrCounter("GetModelHit")
		return nil
	}

	// If decoded to map, populate struct (only if destElem is a struct)
	if destElem.Kind() == reflect.Struct {
		for k, v := range dataMap {
			field := destElem.FieldByName(k)
			if field.IsValid() && field.CanSet() {
				valueToSet := reflect.ValueOf(v)
				switch {
				case valueToSet.Type().AssignableTo(field.Type()):
					field.Set(valueToSet)
				case valueToSet.Type().ConvertibleTo(field.Type()):
					field.Set(valueToSet.Convert(field.Type()))
				default:
					// Simplified numeric conversion, assuming map values are int64/float64 from Gob
					if (valueToSet.Kind() == reflect.Float64 || valueToSet.Kind() == reflect.Int64) && (field.Kind() >= reflect.Int && field.Kind() <= reflect.Uint64) {
						var numericVal int64
						if valueToSet.Kind() == reflect.Float64 {
							numericVal = int64(valueToSet.Float())
						} else {
							numericVal = valueToSet.Int()
						}
						switch field.Kind() {
						case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
							field.SetInt(numericVal)
						case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
							if numericVal < 0 {
								log.Printf("WARN: localCache.GetModel: trying to set negative value %d to unsigned field %s", numericVal, k)
							}
							field.SetUint(uint64(numericVal))
						}
					} else {
						log.Printf("WARN: localCache.GetModel: Unhandled type mismatch for field %s: map type %T, struct field type %s", k, v, field.Type())
					}
				}
			}
		}
	} else {
		// Decoded to map, but dest is not a struct - this case should ideally not happen if GetModel is for structs.
		// But if it was a simple type that somehow got wrapped in a map by SetModel, this is an issue.
		log.Printf("WARN: localCache.GetModel: decoded Gob to map, but dest is %s, not a struct. Key: %s", destElem.Kind(), key)
		// We might need to return an error here or try to extract a single value if map has one key.
		// For now, if map decode succeeded but dest isn't struct, it's a state mismatch.
		return fmt.Errorf("localCache.GetModel: inconsistent state for key '%s', decoded to map but dest is not struct", key)
	}

	m.incrCounter("GetModelHit")
	return nil
}

func (m *localCache) SetModel(ctx context.Context, key string, model interface{}, fieldsToCache []string, expiration time.Duration) error {
	m.incrCounter("SetModel")

	val := reflect.ValueOf(model)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	var dataToCache []byte
	// var err error // Removed unused variable

	if val.Kind() == reflect.Struct {
		dataMap := make(map[string]interface{})
		for _, fieldName := range fieldsToCache {
			fieldVal := val.FieldByName(fieldName)
			if fieldVal.IsValid() && fieldVal.CanInterface() {
				dataMap[fieldName] = fieldVal.Interface()
			} else {
				log.Printf("WARN: localCache.SetModel: Field '%s' not found or not exportable in type %s for key '%s'", fieldName, val.Type().Name(), key)
			}
		}
		var buf bytes.Buffer
		encoder := gob.NewEncoder(&buf)
		if encErr := encoder.Encode(dataMap); encErr != nil {
			return fmt.Errorf("localCache: Gob map encode error for key '%s': %w", key, encErr)
		}
		dataToCache = buf.Bytes()
	} else {
		// If it's not a struct, attempt direct Gob encode (e.g., for int64 from SetCount)
		log.Printf("WARN: localCache.SetModel called with non-struct type %T for key '%s'. Attempting direct Gob encode.", model, key)
		var buf bytes.Buffer
		encoder := gob.NewEncoder(&buf)
		if encErr := encoder.Encode(model); encErr != nil {
			return fmt.Errorf("localCache: Gob direct encode error for key '%s': %w", key, encErr)
		}
		dataToCache = buf.Bytes()
	}

	m.store.Store(key, dataToCache)
	// Note: localCache in cache.go doesn't seem to have expiryStore like mockCacheClient.
	// It uses sync.Map for store, locks, counters. Expiration handling needs review for localCache.
	// For now, this matches the old SetModel which didn't handle expiration for localCache specifically.
	return nil
}

func (m *localCache) DeleteModel(ctx context.Context, key string) error {
	m.incrCounter("DeleteModel")
	m.store.Delete(key)
	return nil
}

func (m *localCache) GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error) {
	m.incrCounter("GetQueryIDs")

	raw, ok := m.store.Load(queryKey)
	if !ok {
		m.incrCounter("GetQueryIDsMiss")
		return nil, common.ErrNotFound
	}

	b, ok := raw.([]byte)
	if !ok {
		m.incrCounter("GetQueryIDsError")
		return nil, fmt.Errorf("localCache: cached query IDs for key '%s' is not []byte (%T)", queryKey, raw)
	}

	var ids []int64
	buf := bytes.NewBuffer(b)
	decoder := gob.NewDecoder(buf)
	if err := decoder.Decode(&ids); err != nil {
		return nil, fmt.Errorf("localCache: Gob Unmarshal error for query key '%s': %w", queryKey, err)
	}
	m.incrCounter("GetQueryIDsHit")
	return ids, nil
}

func (m *localCache) SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error {
	m.incrCounter("SetQueryIDs")

	if ids == nil {
		ids = []int64{}
	}
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	if err := encoder.Encode(ids); err != nil {
		return fmt.Errorf("localCache: Gob Marshal error for query key '%s': %w", queryKey, err)
	}

	m.store.Store(queryKey, buf.Bytes())
	// Expiration not handled by this localCache impl for SetQueryIDs (same as SetModel)
	return nil
}

func (m *localCache) DeleteQueryIDs(ctx context.Context, queryKey string) error {
	m.incrCounter("DeleteQueryIDs")
	m.store.Delete(queryKey)
	return nil
}

// getLock retrieves or creates a channel lock for the given key
func (m *localCache) getLock(key string) *ChannelLock {
	actual, _ := m.lockPool.LoadOrStore(key, NewChannelLock())
	return actual.(*ChannelLock)
}

func (m *localCache) AcquireLock(ctx context.Context, lockKey string, expiration time.Duration) (bool, error) {
	m.incrCounter("AcquireLock")
	lock := m.getLock(lockKey)

	// Create a timeout context based on expiration
	timeoutCtx := ctx
	if expiration > 0 {
		var cancel context.CancelFunc
		timeoutCtx, cancel = context.WithTimeout(ctx, expiration)
		defer cancel()
	}

	select {
	case lock.ch <- struct{}{}:
		// Successfully acquired the lock
		return true, nil
	case <-timeoutCtx.Done():
		// Context canceled or timeout while waiting for lock
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return false, nil // Timeout is not an error, just couldn't acquire lock
		}
		return false, timeoutCtx.Err()
	}
}

func (m *localCache) ReleaseLock(ctx context.Context, lockKey string) error {
	m.incrCounter("ReleaseLock")
	lock := m.getLock(lockKey)

	select {
	case <-lock.ch:
		// Successfully released the lock
		return nil
	case <-ctx.Done():
		// Context canceled while trying to release
		return ctx.Err()
	default:
		// Lock was not held, this is a warning but not an error
		log.Printf("WARN: Attempted to release lock '%s' that was not held", lockKey)
		return nil
	}
}

func (m *localCache) Incr(ctx context.Context, key string) (int64, error) {
	m.incrCounter("Incr")

	lockKey := "incr:" + key // Incr method uses specific lock prefix

	// Call AcquireLock to get the lock, expiration=0 means wait indefinitely until ctx is canceled
	acquired, err := m.AcquireLock(ctx, lockKey, 0)
	if err != nil {
		return 0, err // Error occurred while acquiring lock
	}
	if !acquired {
		// If expiration=0, AcquireLock should block until success or ctx.Done()
		// So theoretically if err is nil, acquired should be true. This is more of a defensive check.
		return 0, fmt.Errorf("could not acquire lock for Incr on key '%s' (context: %v)", key, ctx.Err())
	}

	// Successfully acquired lock
	defer m.ReleaseLock(ctx, lockKey) // Ensure lock is released

	// --- Original atomic increment logic here ---
	val, loaded := m.store.Load(key)
	var currentVal int64
	if loaded {
		if strVal, ok := val.(string); ok {
			if parsed, pErr := strconv.ParseInt(strVal, 10, 64); pErr == nil {
				currentVal = parsed
			}
		}
	}
	newVal := currentVal + 1
	newValStr := strconv.FormatInt(newVal, 10)
	m.store.Store(key, newValStr)
	// --- End of atomic increment logic ---

	return newVal, nil
}

func (m *localCache) Expire(ctx context.Context, key string, expiration time.Duration) error {
	m.incrCounter("Expire")
	// For localCache (in-memory), we don't implement expiration mechanism
	// This is a no-op for the local cache implementation
	// In a production environment, you might want to implement a proper expiration mechanism
	return nil
}

func (m *localCache) incrCounter(name string) {
	m.countersMu.Lock()
	defer m.countersMu.Unlock()
	val, _ := m.counters.LoadOrStore(name, 0)
	m.counters.Store(name, val.(int)+1)
}

func (m *localCache) GetCacheStats(ctx context.Context) CacheStats {
	clonedCounters := make(map[string]int)
	m.counters.Range(func(key, value any) bool {
		k, ok1 := key.(string)
		v, ok2 := value.(int)
		if ok1 && ok2 {
			clonedCounters[k] = v
		}
		return true
	})
	return CacheStats{Counters: clonedCounters}
}
