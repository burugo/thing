package thing_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"thing"

	"github.com/stretchr/testify/assert"
)

// --- Cache Mock Implementation ---

// Mock CacheClient for testing purposes
type mockCacheClient struct {
	// Use sync.Map for thread-safe storage of models, query IDs, and locks
	store sync.Map
	// Use separate map for expiry times (sync.Map not ideal for TTL checks)
	expiryStore sync.Map // Stores expiry times (time.Time) for keys

	// Mutex to protect counters and non-sync.Map fields
	mu sync.RWMutex
	// Simple flag to track if the last GetQueryIDs call was a cache hit
	lastQueryCacheHit bool

	// Call counters for assertions
	GetModelCalls                      int
	SetModelCalls                      int
	DeleteModelCalls                   int
	GetQueryIDsCalls                   int
	SetQueryIDsCalls                   int
	DeleteQueryIDsCalls                int
	AcquireLockCalls                   int
	ReleaseLockCalls                   int
	DeleteByPrefixCalls                int
	InvalidateQueriesContainingIDCalls int
	// Add counters for new methods
	GetCalls    int
	SetCalls    int
	DeleteCalls int
}

// Reset clears the mock cache's internal store and counters.
func (m *mockCacheClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Log cache state before clearing
	log.Printf("DEBUG Reset: Cache state BEFORE reset:")
	var keyCount int
	m.store.Range(func(key, value interface{}) bool {
		keyCount++
		if bytes, ok := value.([]byte); ok {
			log.Printf("  - Key: %v (%d bytes)", key, len(bytes))
		} else {
			log.Printf("  - Key: %v (type: %T)", key, value)
		}
		return true
	})
	log.Printf("DEBUG Reset: Total keys before reset: %d", keyCount)

	// Range and delete seems the most straightforward way to clear sync.Map
	m.store.Range(func(key, value interface{}) bool {
		m.store.Delete(key)
		return true
	})
	m.expiryStore.Range(func(key, value interface{}) bool {
		m.expiryStore.Delete(key)
		return true
	})
	m.lastQueryCacheHit = false // Reset flag as well

	// Reset counters
	m.GetModelCalls = 0
	m.SetModelCalls = 0
	m.DeleteModelCalls = 0
	m.GetQueryIDsCalls = 0
	m.SetQueryIDsCalls = 0
	m.DeleteQueryIDsCalls = 0
	m.AcquireLockCalls = 0
	m.ReleaseLockCalls = 0
	m.DeleteByPrefixCalls = 0
	m.InvalidateQueriesContainingIDCalls = 0
	// Reset new counters
	m.GetCalls = 0
	m.SetCalls = 0
	m.DeleteCalls = 0

	// Verify the cache is empty after reset
	keyCount = 0
	m.store.Range(func(key, value interface{}) bool {
		keyCount++
		log.Printf("  - WARNING: Key still exists after reset: %v", key)
		return true
	})
	log.Printf("DEBUG Reset: Cache cleared. Keys remaining: %d", keyCount)
}

// ResetCounts only resets the call counters, not the cache content.
func (m *mockCacheClient) ResetCounts() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reset counters
	m.GetModelCalls = 0
	m.SetModelCalls = 0
	m.DeleteModelCalls = 0
	m.GetQueryIDsCalls = 0
	m.SetQueryIDsCalls = 0
	m.DeleteQueryIDsCalls = 0
	m.AcquireLockCalls = 0
	m.ReleaseLockCalls = 0
	m.DeleteByPrefixCalls = 0
	m.InvalidateQueriesContainingIDCalls = 0
	m.GetCalls = 0
	m.SetCalls = 0
	m.DeleteCalls = 0
	log.Printf("DEBUG ResetCounts: Call counters reset.")
}

// Exists checks if a non-expired key is present in the mock cache store.
// Note: This doesn't check lock prefixes.
func (m *mockCacheClient) Exists(key string) bool {
	// log.Printf("MockCache.Exists: Checking key: %s", key) // DEBUG LOGGING
	_, exists := m.store.Load(key)
	if !exists {
		log.Printf("DEBUG Exists: Key %s NOT found in store", key)
		return false
	}

	// Check expiry
	if expiryTime, expiryExists := m.expiryStore.Load(key); expiryExists {
		if time.Now().After(expiryTime.(time.Time)) {
			log.Printf("DEBUG Exists: Key %s IS expired. Deleting.", key)
			// Expired, treat as non-existent (and delete)
			m.store.Delete(key)
			m.expiryStore.Delete(key)
			return false
		}
		log.Printf("DEBUG Exists: Key %s NOT expired.", key)
	} else {
		log.Printf("DEBUG Exists: Key %s has NO expiry.", key)
	}
	// Exists and not expired (or no expiry set)
	log.Printf("DEBUG Exists: Key %s FOUND and valid.", key)
	return true
}

// GetValue retrieves the raw stored bytes for a non-expired key from the mock cache.
// Note: This doesn't check lock prefixes.
func (m *mockCacheClient) GetValue(key string) ([]byte, bool) {
	log.Printf("DEBUG GetValue: Checking key: %s", key)
	val, ok := m.store.Load(key)
	if !ok {
		log.Printf("DEBUG GetValue: Key %s NOT found in store", key)
		return nil, false
	}

	// Check expiry
	if expiryVal, expOk := m.expiryStore.Load(key); expOk {
		if expiry, timeOk := expiryVal.(time.Time); timeOk {
			if time.Now().After(expiry) {
				log.Printf("DEBUG GetValue: Key %s IS expired. Deleting.", key)
				// Expired, delete key
				m.store.Delete(key)
				m.expiryStore.Delete(key)
				return nil, false // Expired
			}
			log.Printf("DEBUG GetValue: Key %s has expiry and is NOT expired", key)
		}
	} else {
		log.Printf("DEBUG GetValue: Key %s has NO expiry", key)
	}

	storedBytes, ok := val.([]byte)
	if !ok {
		log.Printf("DEBUG GetValue: Key %s exists but value is not []byte", key)
		// Value exists but isn't bytes - might be a lock marker?
		return nil, false
	}

	log.Printf("DEBUG GetValue: Key %s FOUND with valid data (%d bytes)", key, len(storedBytes))
	return storedBytes, true
}

// Helper to marshal data for storage
func marshalForMock(data interface{}) ([]byte, error) {
	return json.Marshal(data)
}

// Helper to unmarshal data from storage
func unmarshalFromMock(stored []byte, dest interface{}) error {
	return json.Unmarshal(stored, dest)
}

// Get retrieves a raw string value from the mock cache.
func (m *mockCacheClient) Get(ctx context.Context, key string) (string, error) {
	m.mu.Lock()
	m.GetCalls++
	m.mu.Unlock()

	log.Printf("DEBUG Get: Checking key: %s", key)
	// Use GetValue helper which includes expiry check
	storedBytes, ok := m.GetValue(key)
	if !ok {
		log.Printf("DEBUG Get: Key %s NOT FOUND in cache", key)
		return "", thing.ErrNotFound
	}

	// Assuming the value stored by Set is the raw string
	// For mock purposes, we stored bytes, so convert back
	val := string(storedBytes)
	log.Printf("DEBUG Get: Key %s FOUND in cache, value: '%s'", key, val)
	return val, nil
}

// Set stores a raw string value in the mock cache.
func (m *mockCacheClient) Set(ctx context.Context, key string, value string, expiration time.Duration) error {
	m.mu.Lock()
	m.SetCalls++
	m.mu.Unlock()

	log.Printf("DEBUG Set: Setting key: %s with value: '%s', expiration: %v", key, value, expiration)
	// Store as bytes to be consistent with GetValue helper
	m.store.Store(key, []byte(value))
	if expiration > 0 {
		expiryTime := time.Now().Add(expiration)
		m.expiryStore.Store(key, expiryTime)
		log.Printf("DEBUG Set: Set key %s with expiry at %v", key, expiryTime)
	} else {
		log.Printf("DEBUG Set: Set key %s with no expiry", key)
	}
	return nil
}

// Delete removes a key from the mock cache.
func (m *mockCacheClient) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	m.DeleteCalls++
	m.mu.Unlock()

	log.Printf("DEBUG Delete: Deleting key: %s", key)
	m.store.Delete(key)
	m.expiryStore.Delete(key)
	return nil
}

func (m *mockCacheClient) GetModel(ctx context.Context, key string, modelPtr interface{}) error {
	m.mu.Lock()
	m.GetModelCalls++
	m.mu.Unlock()

	// Debug logging to show all keys in the store when GetModel is called
	log.Printf("DEBUG GetModel: Current keys in cache for key: %s", key)
	m.store.Range(func(k, v interface{}) bool {
		log.Printf("  - Key: %v", k)
		return true
	})

	// Use GetValue helper which includes expiry check
	storedBytes, ok := m.GetValue(key)
	if !ok {
		log.Printf("DEBUG GetModel: Key %s NOT FOUND in cache", key)
		return thing.ErrNotFound
	}

	// Check for NoneResult marker BEFORE trying to unmarshal
	if string(storedBytes) == thing.NoneResult {
		log.Printf("DEBUG GetModel: Found NoneResult marker for key %s", key)
		return thing.ErrCacheNoneResult // <-- FIX: Return specific error
	}

	log.Printf("DEBUG GetModel: Key %s FOUND in cache, unmarshaling...", key)
	if err := unmarshalFromMock(storedBytes, modelPtr); err != nil {
		log.Printf("DEBUG GetModel: Error unmarshaling for key %s: %v", key, err)
		return fmt.Errorf("mock cache unmarshal error for key '%s': %w", key, err)
	}
	log.Printf("DEBUG GetModel: Successfully unmarshaled data for key %s", key)
	return nil // Found and successfully unmarshaled
}

func (m *mockCacheClient) SetModel(ctx context.Context, key string, model interface{}, expiration time.Duration) error {
	m.mu.Lock() // Lock for counter update
	m.SetModelCalls++
	m.mu.Unlock()

	log.Printf("DEBUG SetModel: Setting key: %s with expiration: %v", key, expiration)
	data, err := marshalForMock(model)
	if err != nil {
		log.Printf("DEBUG SetModel: Error marshaling for key %s: %v", key, err)
		return fmt.Errorf("mock cache marshal error for key '%s': %w", key, err)
	}

	// Important: Store serialized data
	m.store.Store(key, data)
	if expiration > 0 {
		expiryTime := time.Now().Add(expiration)
		m.expiryStore.Store(key, expiryTime)
		log.Printf("DEBUG SetModel: Set key %s with expiry at %v", key, expiryTime)
	} else {
		log.Printf("DEBUG SetModel: Set key %s with no expiry", key)
	}

	return nil
}

func (m *mockCacheClient) DeleteModel(ctx context.Context, key string) error {
	m.mu.Lock()
	m.DeleteModelCalls++
	m.mu.Unlock()

	log.Printf("DEBUG DeleteModel: Deleting key: %s", key)
	m.store.Delete(key)
	m.expiryStore.Delete(key)
	return nil
}

// GetQueryIDs retrieves a list of IDs associated with a query cache key.
// It now checks for the NoneResult marker and returns ErrQueryCacheNoneResult if found.
func (m *mockCacheClient) GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error) {
	m.mu.Lock()
	m.GetQueryIDsCalls++
	m.mu.Unlock()

	log.Printf("DEBUG GetQueryIDs: Looking up query key: %s", queryKey)

	// Use GetValue helper which includes expiry check
	storedBytes, ok := m.GetValue(queryKey)
	if !ok {
		log.Printf("DEBUG GetQueryIDs: Query key %s NOT FOUND in cache", queryKey)
		m.mu.Lock()
		m.lastQueryCacheHit = false
		m.mu.Unlock()
		return nil, thing.ErrNotFound
	}

	// Check for NoneResult marker
	if string(storedBytes) == thing.NoneResult {
		log.Printf("DEBUG GetQueryIDs: Found NoneResult marker for query key %s", queryKey)
		m.mu.Lock()
		m.lastQueryCacheHit = true // Considered a hit, even though it's empty
		m.mu.Unlock()
		return nil, thing.ErrQueryCacheNoneResult // Return the specific error
	}

	var ids []int64
	if err := unmarshalFromMock(storedBytes, &ids); err != nil {
		log.Printf("DEBUG GetQueryIDs: Error unmarshaling for query key %s: %v", queryKey, err)
		m.mu.Lock()
		m.lastQueryCacheHit = false // Treat unmarshal error as a miss
		m.mu.Unlock()
		return nil, fmt.Errorf("mock cache unmarshal error for query key '%s': %w", queryKey, err)
	}

	m.mu.Lock()
	m.lastQueryCacheHit = true
	m.mu.Unlock()
	log.Printf("DEBUG GetQueryIDs: Found %d IDs for query key %s", len(ids), queryKey)
	return ids, nil
}

func (m *mockCacheClient) SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error {
	m.mu.Lock()
	m.SetQueryIDsCalls++
	m.mu.Unlock()

	log.Printf("DEBUG SetQueryIDs: Setting query key: %s with %d IDs, expiration: %v", queryKey, len(ids), expiration)
	if len(ids) > 0 {
		log.Printf("DEBUG SetQueryIDs: First few IDs: %v", ids[:min(3, len(ids))])
	}

	data, err := marshalForMock(ids)
	if err != nil {
		log.Printf("DEBUG SetQueryIDs: Error marshaling for query key %s: %v", queryKey, err)
		return fmt.Errorf("mock cache marshal error for query key '%s': %w", queryKey, err)
	}

	m.store.Store(queryKey, data)
	if expiration > 0 {
		expiryTime := time.Now().Add(expiration)
		m.expiryStore.Store(queryKey, expiryTime)
		log.Printf("DEBUG SetQueryIDs: Set query key %s with expiry at %v", queryKey, expiryTime)
	} else {
		log.Printf("DEBUG SetQueryIDs: Set query key %s with no expiry", queryKey)
	}

	return nil
}

func (m *mockCacheClient) DeleteQueryIDs(ctx context.Context, queryKey string) error {
	m.mu.Lock()
	m.DeleteQueryIDsCalls++
	m.mu.Unlock()

	log.Printf("DEBUG DeleteQueryIDs: Deleting query key: %s", queryKey)
	m.store.Delete(queryKey)
	m.expiryStore.Delete(queryKey)
	return nil
}

func (m *mockCacheClient) AcquireLock(ctx context.Context, lockKey string, expiration time.Duration) (bool, error) {
	m.mu.Lock()
	m.AcquireLockCalls++
	m.mu.Unlock()

	log.Printf("DEBUG AcquireLock: Attempting to acquire lock: %s with expiration: %v", lockKey, expiration)

	// Check if the lock exists and is not expired
	if m.Exists(lockKey) {
		log.Printf("DEBUG AcquireLock: Lock %s already exists and is valid", lockKey)
		return false, nil // Lock exists and is still valid
	}

	// Since our mock uses normal keys for simplicity, we'll just use a dummy value
	// Redis would use SET NX, memcached would use ADD
	m.store.Store(lockKey, []byte("LOCK"))
	if expiration > 0 {
		expiryTime := time.Now().Add(expiration)
		m.expiryStore.Store(lockKey, expiryTime)
		log.Printf("DEBUG AcquireLock: Acquired lock %s with expiry at %v", lockKey, expiryTime)
	} else {
		log.Printf("DEBUG AcquireLock: Acquired lock %s with no expiry", lockKey)
	}

	return true, nil
}

func (m *mockCacheClient) ReleaseLock(ctx context.Context, lockKey string) error {
	m.mu.Lock()
	m.ReleaseLockCalls++
	m.mu.Unlock()

	log.Printf("DEBUG ReleaseLock: Releasing lock: %s", lockKey)
	m.store.Delete(lockKey)
	m.expiryStore.Delete(lockKey)
	return nil
}

func (m *mockCacheClient) DeleteByPrefix(ctx context.Context, prefix string) error {
	m.mu.Lock()
	m.DeleteByPrefixCalls++
	m.mu.Unlock()

	log.Printf("DEBUG DeleteByPrefix: Deleting keys with prefix: %s", prefix)
	var keysToDelete []interface{}

	// First collect keys (to avoid concurrent modification)
	m.store.Range(func(key, _ interface{}) bool {
		keyStr, ok := key.(string)
		if ok && keyStr != "" && len(keyStr) >= len(prefix) && keyStr[:len(prefix)] == prefix {
			log.Printf("DEBUG DeleteByPrefix: Found matching key: %s", keyStr)
			keysToDelete = append(keysToDelete, key)
		}
		return true
	})

	// Then delete them
	deletedCount := 0
	for _, key := range keysToDelete {
		m.store.Delete(key)
		m.expiryStore.Delete(key)
		deletedCount++
	}
	log.Printf("DEBUG DeleteByPrefix: Deleted %d keys with prefix: %s", deletedCount, prefix)

	return nil
}

func (m *mockCacheClient) InvalidateQueriesContainingID(ctx context.Context, prefix string, idToInvalidate int64) error {
	m.mu.Lock()
	m.InvalidateQueriesContainingIDCalls++
	m.mu.Unlock()

	log.Printf("DEBUG InvalidateQueriesContainingID: Finding and invalidating query cache entries with prefix '%s' containing ID %d", prefix, idToInvalidate)

	var matchingQueryKeys []string

	// First, find all query keys matching the prefix
	m.store.Range(func(key, _ interface{}) bool {
		// Only process string keys (should be all of them, but just to be safe)
		keyStr, ok := key.(string)
		if !ok {
			return true // Skip non-string keys
		}

		// Check if this key has the query prefix
		if len(keyStr) >= len(prefix) && keyStr[:len(prefix)] == prefix {
			log.Printf("DEBUG InvalidateQueriesContainingID: Found key with matching prefix: %s", keyStr)

			// Get the cached IDs for this query
			storedBytes, found := m.GetValue(keyStr)
			if !found {
				log.Printf("DEBUG InvalidateQueriesContainingID: Key %s not found or expired", keyStr)
				return true // Continue range
			}

			// Decode the IDs
			var ids []int64
			if err := unmarshalFromMock(storedBytes, &ids); err != nil {
				log.Printf("DEBUG InvalidateQueriesContainingID: Error unmarshaling IDs for key %s: %v", keyStr, err)
				return true // Continue range
			}

			// Check if the ID to invalidate is in this query's result set
			for _, id := range ids {
				if id == idToInvalidate {
					log.Printf("DEBUG InvalidateQueriesContainingID: Query key %s contains ID %d, marking for invalidation", keyStr, idToInvalidate)
					matchingQueryKeys = append(matchingQueryKeys, keyStr)
					break
				}
			}
		}
		return true
	})

	// Delete all matching query keys
	for _, keyStr := range matchingQueryKeys {
		log.Printf("DEBUG InvalidateQueriesContainingID: Invalidating query key: %s", keyStr)
		m.store.Delete(keyStr)
		m.expiryStore.Delete(keyStr)
	}

	log.Printf("DEBUG InvalidateQueriesContainingID: Invalidated %d query cache entries", len(matchingQueryKeys))
	return nil
}

// Helper function to return the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// AssertSetCalls checks if Set was called the expected number of times.
func (m *mockCacheClient) AssertSetCalls(t *testing.T, expected int, msgAndArgs ...interface{}) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	assert.Equal(t, expected, m.SetCalls, msgAndArgs...)
}

// AssertSetModelCalls checks if SetModel was called the expected number of times.
func (m *mockCacheClient) AssertSetModelCalls(t *testing.T, expected int, msgAndArgs ...interface{}) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	assert.Equal(t, expected, m.SetModelCalls, msgAndArgs...)
}
