package thing_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/burugo/thing/common"
	"github.com/burugo/thing/internal/drivers/db/sqlite"
	interfaces "github.com/burugo/thing/internal/interfaces"
	"github.com/burugo/thing/internal/schema"
	"github.com/burugo/thing/internal/types"

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

	// Call counters for assertions (Exported)
	Counters map[string]int // Operation counters for stats (e.g., "Get", "GetMiss"). Thread-safe via mu.

	// Additional fields for call counters
	GetCalls            int
	SetCalls            int
	DeleteCalls         int
	GetModelCalls       int
	SetModelCalls       int
	DeleteModelCalls    int
	GetQueryIDsCalls    int
	SetQueryIDsCalls    int
	DeleteQueryIDsCalls int
	AcquireLockCalls    int
	ReleaseLockCalls    int
	DeleteByPrefixCalls int
	// New counters for query_test.go
	SetCountCalls int
	GetCountCalls int
}

// newMockCacheClient creates a new initialized mock cache client.

// Reset clears the mock cache's internal store and counters.
func (m *mockCacheClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Log cache state before clearing
	// log.Printf("DEBUG Reset: Cache state BEFORE reset:")
	var keyCount int
	m.store.Range(func(key, value interface{}) bool {
		keyCount++
		// if bytes, ok := value.([]byte); ok {
		// 	log.Printf("  - Key: %v (%d bytes)", key, len(bytes))
		// } else {
		// 	log.Printf("  - Key: %v (type: %T)", key, value)
		// }
		return true
	})
	// log.Printf("DEBUG Reset: Total keys before reset: %d", keyCount)

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
	m.Counters = make(map[string]int) // Reset map
	m.ResetCalls()                    // Call the dedicated reset for calls

	// Verify the cache is empty after reset
	// keyCount = 0
	m.store.Range(func(key, value interface{}) bool {
		keyCount++
		// log.Printf("  - WARNING: Key still exists after reset: %v", key)
		return true
	})
	// log.Printf("DEBUG Reset: Cache cleared. Keys remaining: %d", keyCount)
}

// ResetCalls resets only the call counters.
// It assumes the caller (e.g., Reset) already holds the necessary lock (m.mu).
func (m *mockCacheClient) ResetCalls() {
	// Reset the map used by assertions
	m.Counters = make(map[string]int)

	// Reset individual counters (optional, as map is now primary)
	m.GetCalls = 0
	m.SetCalls = 0
	m.DeleteCalls = 0
	m.GetModelCalls = 0
	m.SetModelCalls = 0
	m.DeleteModelCalls = 0
	m.GetQueryIDsCalls = 0
	m.SetQueryIDsCalls = 0
	m.DeleteQueryIDsCalls = 0
	m.AcquireLockCalls = 0
	m.ReleaseLockCalls = 0
	m.DeleteByPrefixCalls = 0
	// Reset new counters
	m.SetCountCalls = 0
	m.GetCountCalls = 0
}

// ResetCounts is an alias for ResetCalls, used in query_test
// TODO: Consolidate ResetCalls/ResetCounts if possible
func (m *mockCacheClient) ResetCounts() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ResetCalls() // Reuse existing logic
}

// Helper method to increment a counter safely
func (m *mockCacheClient) incrementCounter(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Counters == nil { // Initialize map if nil
		m.Counters = make(map[string]int)
	}
	m.Counters[name]++
}

// Exists checks if a non-expired key is present in the mock cache store.
// Note: This doesn't check lock prefixes.
func (m *mockCacheClient) Exists(key string) bool {
	// log.Printf("MockCache.Exists: Checking key: %s", key) // DEBUG LOGGING
	_, exists := m.store.Load(key)
	if !exists {
		// log.Printf("DEBUG Exists: Key %s NOT found in store", key)
		return false
	}

	// Check expiry
	if expiryTime, expiryExists := m.expiryStore.Load(key); expiryExists {
		if time.Now().After(expiryTime.(time.Time)) {
			// log.Printf("DEBUG Exists: Key %s IS expired. Deleting.", key)
			// Expired, treat as non-existent (and delete)
			m.store.Delete(key)
			m.expiryStore.Delete(key)
			return false
		}
		// log.Printf("DEBUG Exists: Key %s NOT expired.", key)
	} else {
		// log.Printf("DEBUG Exists: Key %s has NO expiry.", key)
	}
	// Exists and not expired (or no expiry set)
	// log.Printf("DEBUG Exists: Key %s FOUND and valid.", key)
	return true
}

// GetValue retrieves the raw stored bytes for a non-expired key from the mock cache.
// Note: This doesn't check lock prefixes.
func (m *mockCacheClient) GetValue(key string) ([]byte, bool) {
	// log.Printf("DEBUG GetValue: Checking key: %s", key)
	val, ok := m.store.Load(key)
	if !ok {
		// log.Printf("DEBUG GetValue: Key %s NOT found in store", key)
		return nil, false
	}

	// Check expiry
	if expiryVal, expOk := m.expiryStore.Load(key); expOk {
		if expiry, timeOk := expiryVal.(time.Time); timeOk {
			if time.Now().After(expiry) {
				// log.Printf("DEBUG GetValue: Key %s IS expired. Deleting.", key)
				// Expired, delete key
				m.store.Delete(key)
				m.expiryStore.Delete(key)
				return nil, false // Expired
			}
			// log.Printf("DEBUG GetValue: Key %s has expiry and is NOT expired", key)
		}
	} else {
		// log.Printf("DEBUG GetValue: Key %s has NO expiry", key)
	}

	storedBytes, ok := val.([]byte)
	if !ok {
		// log.Printf("DEBUG GetValue: Key %s exists but value is not []byte", key)
		// Value exists but isn't bytes - might be a lock marker?
		return nil, false
	}

	// log.Printf("DEBUG GetValue: Key %s FOUND with valid data (%d bytes)", key, len(storedBytes))
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
	m.incrementCounter("Get") // Use helper (total calls)
	m.mu.Lock()
	m.mu.Unlock()

	// log.Printf("DEBUG Get: Checking key: %s", key)
	// Use GetValue helper which includes expiry check
	storedBytes, ok := m.GetValue(key)
	if !ok {
		m.incrementCounter("GetMiss") // cache miss
		return "", common.ErrNotFound
	}
	m.incrementCounter("GetHit") // cache hit

	// Assuming the value stored by Set is the raw string
	// For mock purposes, we stored bytes, so convert back
	val := string(storedBytes)
	return val, nil
}

// Set stores a raw string value in the mock cache.
func (m *mockCacheClient) Set(ctx context.Context, key string, value string, expiration time.Duration) error {
	m.incrementCounter("Set") // Use helper
	m.mu.Lock()
	m.mu.Unlock()

	// log.Printf("DEBUG Set: Setting key: %s with value: '%s', expiration: %v", key, value, expiration)
	// Store as bytes to be consistent with GetValue helper
	m.store.Store(key, []byte(value))
	if expiration > 0 {
		expiryTime := time.Now().Add(expiration)
		m.expiryStore.Store(key, expiryTime)
		// log.Printf("DEBUG Set: Set key %s with expiry at %v", key, expiryTime)
	} else {
		// log.Printf("DEBUG Set: Set key %s with no expiry", key)
	}
	return nil
}

// Delete removes a key from the mock cache.
func (m *mockCacheClient) Delete(ctx context.Context, key string) error {
	m.incrementCounter("Delete") // Use helper
	m.mu.Lock()
	m.mu.Unlock()

	// log.Printf("DEBUG Delete: Deleting key: %s", key)
	m.store.Delete(key)
	m.expiryStore.Delete(key)
	return nil
}

func (m *mockCacheClient) GetModel(ctx context.Context, key string, modelPtr interface{}) error {
	m.incrementCounter("GetModel") // Use helper
	m.mu.Lock()
	m.mu.Unlock()

	// Debug logging to show all keys in the store when GetModel is called
	// log.Printf("DEBUG GetModel: Current keys in cache for key: %s", key)
	m.store.Range(func(k, v interface{}) bool {
		// log.Printf("  - Key: %v", k)
		return true
	})

	// Use GetValue helper which includes expiry check
	storedBytes, ok := m.GetValue(key)
	if !ok {
		m.incrementCounter("GetModelMiss")
		return common.ErrNotFound
	}

	// Check for NoneResult marker BEFORE trying to unmarshal
	if string(storedBytes) == common.NoneResult {
		m.incrementCounter("GetModelMiss")
		return common.ErrCacheNoneResult
	}
	m.incrementCounter("GetModelHit")

	// log.Printf("DEBUG GetModel: Key %s FOUND in cache, unmarshaling...", key)
	if err := unmarshalFromMock(storedBytes, modelPtr); err != nil {
		// log.Printf("DEBUG GetModel: Error unmarshaling for key %s: %v", key, err)
		return fmt.Errorf("mock cache unmarshal error for key '%s': %w", key, err)
	}
	// log.Printf("DEBUG GetModel: Successfully unmarshaled data for key %s", key)
	return nil // Found and successfully unmarshaled
}

func (m *mockCacheClient) SetModel(ctx context.Context, key string, model interface{}, expiration time.Duration) error {
	m.incrementCounter("SetModel") // Use helper
	m.mu.Lock()                    // Lock for counter update
	m.mu.Unlock()

	// log.Printf("DEBUG SetModel: Setting key: %s with expiration: %v", key, expiration)
	data, err := marshalForMock(model)
	if err != nil {
		// log.Printf("DEBUG SetModel: Error marshaling for key %s: %v", key, err)
		return fmt.Errorf("mock cache marshal error for key '%s': %w", key, err)
	}
	// log.Printf("DEBUG MOCK SETMODEL: Marshaled data for key %s: %s", key, string(data)) // REMOVED LOG

	// Important: Store serialized data
	m.store.Store(key, data)
	if expiration > 0 {
		expiryTime := time.Now().Add(expiration)
		m.expiryStore.Store(key, expiryTime)
		// log.Printf("DEBUG SetModel: Set key %s with expiry at %v", key, expiryTime)
	} else {
		// log.Printf("DEBUG SetModel: Set key %s with no expiry", key)
	}

	return nil
}

func (m *mockCacheClient) DeleteModel(ctx context.Context, key string) error {
	m.incrementCounter("DeleteModel") // Use helper
	m.mu.Lock()
	m.mu.Unlock()

	// log.Printf("DEBUG DeleteModel: Deleting key: %s", key)
	m.store.Delete(key)
	m.expiryStore.Delete(key)
	return nil
}

// GetQueryIDs retrieves a list of IDs associated with a query cache key.
// It now checks for the NoneResult marker and returns ErrQueryCacheNoneResult if found.
func (m *mockCacheClient) GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error) {
	m.incrementCounter("GetQueryIDs") // Use helper
	m.mu.Lock()
	m.mu.Unlock()

	// log.Printf("DEBUG GetQueryIDs: Looking up query key: %s", queryKey)

	// Use GetValue helper which includes expiry check
	storedBytes, ok := m.GetValue(queryKey)
	if !ok {
		m.incrementCounter("GetQueryIDsMiss")
		m.lastQueryCacheHit = false
		return nil, common.ErrNotFound
	}
	m.incrementCounter("GetQueryIDsHit")
	m.lastQueryCacheHit = true

	var ids []int64
	if err := unmarshalFromMock(storedBytes, &ids); err != nil {
		// log.Printf("DEBUG GetQueryIDs: Error unmarshaling for query key %s: %v", queryKey, err)
		m.lastQueryCacheHit = false
		return nil, fmt.Errorf("mock cache unmarshal error for query key '%s': %w", queryKey, err)
	}

	return ids, nil
}

func (m *mockCacheClient) SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error {
	m.incrementCounter("SetQueryIDs") // Use helper
	m.mu.Lock()
	m.mu.Unlock()

	// log.Printf("DEBUG SetQueryIDs: Setting query key: %s with %d IDs, expiration: %v", queryKey, len(ids), expiration)
	if len(ids) > 0 {
		// log.Printf("DEBUG SetQueryIDs: First few IDs: %v", ids[:min(3, len(ids))])
	}

	data, err := marshalForMock(ids)
	if err != nil {
		// log.Printf("DEBUG SetQueryIDs: Error marshaling for query key %s: %v", queryKey, err)
		return fmt.Errorf("mock cache marshal error for query key '%s': %w", queryKey, err)
	}

	m.store.Store(queryKey, data)
	if expiration > 0 {
		expiryTime := time.Now().Add(expiration)
		m.expiryStore.Store(queryKey, expiryTime)
		// log.Printf("DEBUG SetQueryIDs: Set query key %s with expiry at %v", queryKey, expiryTime)
	} else {
		// log.Printf("DEBUG SetQueryIDs: Set query key %s with no expiry", queryKey)
	}

	return nil
}

func (m *mockCacheClient) DeleteQueryIDs(ctx context.Context, queryKey string) error {
	m.incrementCounter("DeleteQueryIDs") // Use helper
	m.mu.Lock()
	m.mu.Unlock()

	// log.Printf("DEBUG DeleteQueryIDs: Deleting query key: %s", queryKey)
	m.store.Delete(queryKey)
	m.expiryStore.Delete(queryKey)
	return nil
}

// AcquireLock attempts to acquire a lock for the given key.
// Matches the CacheClient interface signature.
func (m *mockCacheClient) AcquireLock(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	m.incrementCounter("AcquireLock") // Use helper
	m.mu.Lock()
	defer m.mu.Unlock()

	lockKey := key // Use the original key directly for locking in the mock

	// log.Printf("DEBUG AcquireLock: Attempting to acquire lock: %s with expiration: %v", lockKey, expiration)

	// Check if the lock exists and is not expired
	if m.Exists(lockKey) { // Use internal Exists which checks expiry
		// log.Printf("DEBUG AcquireLock: Lock %s already exists and is valid", lockKey)
		return false, nil // Lock exists and is still valid
	}

	// Since our mock uses normal keys for simplicity, we'll just use a dummy value
	// Redis would use SET NX
	m.store.Store(lockKey, []byte("LOCK")) // Store lock marker
	if expiration > 0 {
		expiryTime := time.Now().Add(expiration)
		m.expiryStore.Store(lockKey, expiryTime) // Store expiry separately
		// log.Printf("DEBUG AcquireLock: Acquired lock %s with expiry at %v", lockKey, expiryTime)
	} else {
		// log.Printf("DEBUG AcquireLock: Acquired lock %s with no expiry", lockKey)
	}

	return true, nil // Lock acquired
}

func (m *mockCacheClient) ReleaseLock(ctx context.Context, lockKey string) error {
	m.incrementCounter("ReleaseLock") // Use helper
	m.mu.Lock()
	m.mu.Unlock()

	// log.Printf("DEBUG ReleaseLock: Releasing lock: %s", lockKey)
	m.store.Delete(lockKey)
	m.expiryStore.Delete(lockKey)
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
	assert.Equal(t, expected, m.Counters["Set"], msgAndArgs...)
}

// AssertSetModelCalls checks if SetModel was called the expected number of times.
func (m *mockCacheClient) AssertSetModelCalls(t *testing.T, expected int, msgAndArgs ...interface{}) {
	t.Helper()
	m.mu.RLock()
	defer m.mu.RUnlock()
	assert.Equal(t, expected, m.Counters["SetModel"], msgAndArgs...)
}

// --- Mock DB Adapter --- (Keep basic version needed for setup)

type mockDBAdapter struct {
	*sqlite.SQLiteAdapter
	SelectCount   int // Counts calls to Select
	GetCountCalls int // Counts calls to GetCount
}

func (m *mockDBAdapter) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return m.SQLiteAdapter.Get(ctx, dest, query, args...)
}

func (m *mockDBAdapter) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	m.SelectCount++
	return m.SQLiteAdapter.Select(ctx, dest, query, args...)
}

func (m *mockDBAdapter) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return m.SQLiteAdapter.Exec(ctx, query, args...)
}

func (m *mockDBAdapter) GetCount(ctx context.Context, info *schema.ModelInfo, params types.QueryParams) (int64, error) {
	m.GetCountCalls++
	return m.SQLiteAdapter.GetCount(ctx, info, params)
}

func (m *mockDBAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (interfaces.Tx, error) {
	return m.SQLiteAdapter.BeginTx(ctx, opts)
}

func (m *mockDBAdapter) Close() error {
	return m.SQLiteAdapter.Close()
}

// Add a ResetCounts method for tests
func (m *mockDBAdapter) ResetCounts() {
	m.SelectCount = 0
	m.GetCountCalls = 0
}

// --- Mock SQL Result ---

type mockSqlResult struct {
	rowsAffected int64
	lastInsertId int64
}

func (r mockSqlResult) LastInsertId() (int64, error) {
	return r.lastInsertId, nil
}

func (r mockSqlResult) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}

// --- Mock Transaction ---

type mockTx struct {
	// Add fields if needed
}

func (tx *mockTx) Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return common.ErrNotFound
}

func (tx *mockTx) Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error {
	return nil
}

func (tx *mockTx) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return mockSqlResult{rowsAffected: 1, lastInsertId: 0}, nil
}

func (tx *mockTx) Commit() error {
	return nil
}

func (tx *mockTx) Rollback() error {
	return nil
}

// assertMockCacheCounts helper
func assertMockCacheCounts(t *testing.T, mockCache *mockCacheClient, expected map[string]int) {
	t.Helper()
	// Fetch the actual counts from the mock cache's Counters map
	actual := mockCache.Counters // Use the map

	// Provide a more informative default message
	defaultMsg := fmt.Sprintf("Mock cache call counts mismatch.\nExpected: %v\nActual:   %v", expected, actual)

	// Check if maps are equal
	assert.Equal(t, expected, actual, defaultMsg)

	/* // Keep old implementation commented out for reference
	actual := map[string]int{
		"Get":            mockCache.GetCalls,
		"Set":            mockCache.SetCalls,
		"Delete":         mockCache.DeleteCalls,
		"GetModel":       mockCache.GetModelCalls,
		"SetModel":       mockCache.SetModelCalls,
		"DeleteModel":    mockCache.DeleteModelCalls,
		"GetQueryIDs":    mockCache.GetQueryIDsCalls,
		"SetQueryIDs":    mockCache.SetQueryIDsCalls,
		"DeleteQueryIDs": mockCache.DeleteQueryIDsCalls,
		"AcquireLock":    mockCache.AcquireLockCalls,
		"ReleaseLock":    mockCache.ReleaseLockCalls,
		"DeleteByPrefix": mockCache.DeleteByPrefixCalls,
	}
	assert.Equal(t, expected, actual, "Mock cache call counts mismatch")
	*/
}

// FlushAll simulates flushing the cache (clears store and expiry).
func (m *mockCacheClient) FlushAll(ctx context.Context) error {
	m.Reset() // Reuse Reset logic
	return nil
}

// SetCount simulates storing a count value for a key.
func (m *mockCacheClient) SetCount(ctx context.Context, key string, count int64, expiration time.Duration) error {
	m.incrementCounter("SetCount")
	data, err := marshalForMock(count)
	if err != nil {
		return fmt.Errorf("mock SetCount marshal error: %w", err)
	}
	m.store.Store(key, data)
	if expiration > 0 {
		m.expiryStore.Store(key, time.Now().Add(expiration))
	}
	return nil
}

// GetCount simulates retrieving a count value for a key.
func (m *mockCacheClient) GetCount(ctx context.Context, key string) (int64, error) {
	m.incrementCounter("GetCount")
	storedBytes, ok := m.GetValue(key) // Uses expiry check
	if !ok {
		return 0, common.ErrNotFound
	}
	var count int64
	err := unmarshalFromMock(storedBytes, &count)
	if err != nil {
		return 0, fmt.Errorf("mock GetCount unmarshal error: %w", err)
	}
	return count, nil
}

// GetModelCount returns the number of times GetModel was called.
func (m *mockCacheClient) GetModelCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	m.store.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// GetCacheStats returns a snapshot of cache operation counters for the mock client.
// The returned map is a copy and safe for concurrent use.
// Typical keys: "Get", "GetMiss", "GetModel", "GetModelMiss", etc.
func (m *mockCacheClient) GetCacheStats(ctx context.Context) interfaces.CacheStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stats := make(map[string]int, len(m.Counters))
	for k, v := range m.Counters {
		stats[k] = v
	}
	return interfaces.CacheStats{Counters: stats}
}
