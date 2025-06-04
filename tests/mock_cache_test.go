package thing_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"fmt"
	"log"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/burugo/thing"
	"github.com/burugo/thing/common"
	"github.com/burugo/thing/drivers/db/sqlite"

	"github.com/stretchr/testify/assert"
)

func init() {
	gob.Register(time.Time{})
}

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
	log.Printf("[MOCKCACHE DEBUG Exists] Called for key: %s", key)
	val, loaded := m.store.Load(key)
	log.Printf("[MOCKCACHE DEBUG Exists] store.Load result - Key: %s, ExistsInMap: %v, ValueType: %T", key, loaded, val)
	if !loaded {
		log.Printf("[MOCKCACHE DEBUG Exists] Key %s NOT found in store. Returning false.", key)
		return false
	}
	if expiryTime, expiryExists := m.expiryStore.Load(key); expiryExists {
		if time.Now().After(expiryTime.(time.Time)) {
			// Expired, delete key
			log.Printf("[MOCKCACHE DEBUG Exists] Key %s was EXPIRED. Attempting to delete from store and expiryStore.", key)
			m.store.Delete(key)
			m.expiryStore.Delete(key)
			log.Printf("[MOCKCACHE DEBUG Exists] Key %s deleted due to expiry. Returning false.", key)
			return false
		}
		log.Printf("[MOCKCACHE DEBUG Exists] Key %s has expiry and is NOT expired.", key)
	} else {
		log.Printf("[MOCKCACHE DEBUG Exists] Key %s has NO expiry.", key)
	}
	log.Printf("[MOCKCACHE DEBUG Exists] Key %s EXISTS and is NOT expired. Returning true.", key)
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

// Helper to marshal data for storage using GOB based on specified fields
func marshalForMockWithGob(modelInstance interface{}, fieldsToCache []string) ([]byte, error) {
	val := reflect.ValueOf(modelInstance)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("marshalForMockWithGob: modelInstance must be a struct or pointer to struct, got %T", modelInstance)
	}

	dataMap := make(map[string]interface{})
	for _, fieldName := range fieldsToCache {
		fieldVal := val.FieldByName(fieldName)
		if fieldVal.IsValid() && fieldVal.CanInterface() {
			dataMap[fieldName] = fieldVal.Interface()
		} else {
			// Log or handle fields that are not found or not exportable, though ModelInfo.Fields should only give us valid ones.
			log.Printf("WARN: marshalForMockWithGob: Field '%s' not found or not exportable in type %s", fieldName, val.Type().Name())
		}
	}

	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	if err := encoder.Encode(dataMap); err != nil {
		return nil, fmt.Errorf("gob encoding failed: %w", err)
	}
	return buf.Bytes(), nil
}

// Helper to unmarshal data from storage using GOB
func unmarshalFromMockWithGob(data []byte, destModelPtr interface{}) error {
	destVal := reflect.ValueOf(destModelPtr)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() {
		return fmt.Errorf("unmarshalFromMockWithGob: destModelPtr must be a non-nil pointer, got %T", destModelPtr)
	}
	destElem := destVal.Elem()
	if destElem.Kind() != reflect.Struct {
		return fmt.Errorf("unmarshalFromMockWithGob: destModelPtr must point to a struct, got %T", destModelPtr)
	}

	var dataMap map[string]interface{}
	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)
	if err := decoder.Decode(&dataMap); err != nil {
		return fmt.Errorf("gob decoding to map failed: %w", err)
	}

	for key, val := range dataMap {
		field := destElem.FieldByName(key)
		if field.IsValid() && field.CanSet() {
			// Handle type assertion/conversion carefully. Gob map might store float64 for numbers.
			// reflect.ValueOf(val) will give the type from the map.
			// field.Type() is the type of the struct field.
			valueToSet := reflect.ValueOf(val)
			switch {
			case valueToSet.Type().AssignableTo(field.Type()):
				field.Set(valueToSet)
			case valueToSet.Type().ConvertibleTo(field.Type()):
				field.Set(valueToSet.Convert(field.Type()))
			default:
				// Attempt common numeric conversion: float64 in map to int/int64/uint etc. in struct
				if valueToSet.Kind() == reflect.Float64 && (field.Kind() >= reflect.Int && field.Kind() <= reflect.Uint64) {
					floatVal := valueToSet.Float()
					switch field.Kind() {
					case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
						field.SetInt(int64(floatVal))
					case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
						field.SetUint(uint64(floatVal))
					default:
						// Should not happen based on outer if
					}
				} else {
					// Add more specific type conversions if necessary
					log.Printf("WARN: Unhandled type mismatch for field %s: map type %T, struct field type %s", key, val, field.Type())
				}
			}
		} else {
			log.Printf("WARN: unmarshalFromMockWithGob: Field '%s' not found in destination struct %s or cannot be set", key, destElem.Type().Name())
		}
	}
	return nil
}

// Get retrieves a raw string value from the mock cache.
func (m *mockCacheClient) Get(ctx context.Context, key string) (string, error) {
	m.incrementCounter("Get") // Use helper (total calls)

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
	// NoneResult 写保护：如果已是 NoneResult，不重复 set/计数
	if value == common.NoneResult {
		if v, ok := m.store.Load(key); ok {
			if s, ok := v.([]byte); ok && string(s) == common.NoneResult {
				return nil
			}
		}
	}
	m.incrementCounter("Set") // Use helper

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
	log.Printf("[MOCKCACHE DEBUG Delete] Called for key: %s", key)
	m.incrementCounter("Delete") // Use helper

	// log.Printf("DEBUG Delete: Deleting key: %s", key)
	_, loaded := m.store.Load(key)
	if loaded {
		log.Printf("[MOCKCACHE DEBUG Delete] Key %s was FOUND in store before deletion.", key)
	} else {
		log.Printf("[MOCKCACHE DEBUG Delete] Key %s was NOT FOUND in store before attempting deletion.", key)
	}
	m.store.Delete(key)
	m.expiryStore.Delete(key)
	_, stillExists := m.store.Load(key)
	if stillExists {
		log.Printf("[MOCKCACHE DEBUG Delete] Key %s STILL EXISTS in store AFTER deletion attempt. This is unexpected!", key)
	} else {
		log.Printf("[MOCKCACHE DEBUG Delete] Key %s successfully removed from store (or was already absent).", key)
	}
	return nil
}

// GetModel retrieves a model from the mock cache.
func (m *mockCacheClient) GetModel(ctx context.Context, key string, modelPtr interface{}) error {
	m.incrementCounter("GetModel")
	// log.Printf("[MOCKCACHE GetModel] key=%s", key)

	storedBytes, exists := m.GetValue(key) // GetValue handles expiry
	if !exists {
		m.incrementCounter("GetModelMiss")
		// log.Printf("[MOCKCACHE GetModel] MISS for key=%s", key)
		return common.ErrNotFound // Consistent with other cache misses
	}

	// log.Printf("[MOCKCACHE GetModel] HIT for key=%s, len=%d", key, len(storedBytes))
	// err := unmarshalFromMock(storedBytes, modelPtr) // Old way
	err := unmarshalFromMockWithGob(storedBytes, modelPtr) // New GOB way
	if err != nil {
		log.Printf("ERROR: mockCacheClient.GetModel failed to unmarshal for key %s: %v", key, err)
		m.incrementCounter("GetModelError") // Or a more specific error counter
		return fmt.Errorf("failed to unmarshal model from mock cache for key %s: %w", key, err)
	}
	m.incrementCounter("GetModelHit")
	return nil
}

// SetModel stores a model in the mock cache.
func (m *mockCacheClient) SetModel(ctx context.Context, key string, model interface{}, fieldsToCache []string, expiration time.Duration) error {
	m.incrementCounter("SetModel")
	// log.Printf("[MOCKCACHE SetModel] key=%s, expiration=%v, fieldsToCache=%v", key, expiration, fieldsToCache)

	// storedBytes, err := marshalForMock(model) // Old way
	storedBytes, err := marshalForMockWithGob(model, fieldsToCache) // New GOB way
	if err != nil {
		log.Printf("ERROR: mockCacheClient.SetModel failed to marshal model for key %s: %v", key, err)
		return fmt.Errorf("failed to marshal model for mock cache (key %s): %w", key, err)
	}

	m.store.Store(key, storedBytes)
	if expiration > 0 {
		m.expiryStore.Store(key, time.Now().Add(expiration))
	} else {
		// If expiration is 0 or negative, remove any existing expiry to make it non-expiring
		m.expiryStore.Delete(key)
	}
	return nil
}

func (m *mockCacheClient) DeleteModel(ctx context.Context, key string) error {
	m.incrementCounter("DeleteModel") // Use helper

	// log.Printf("DEBUG DeleteModel: Deleting key: %s", key)
	m.store.Delete(key)
	m.expiryStore.Delete(key)
	return nil
}

// GetQueryIDs retrieves a list of IDs associated with a query cache key.
// It now checks for the NoneResult marker and returns ErrQueryCacheNoneResult if found.
func (m *mockCacheClient) GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error) {
	m.incrementCounter("GetQueryIDs") // Use helper
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
	buf := bytes.NewBuffer(storedBytes)
	decoder := gob.NewDecoder(buf)
	if err := decoder.Decode(&ids); err != nil { // Directly decode into &ids
		// log.Printf("DEBUG GetQueryIDs: Error unmarshaling for query key %s: %v", queryKey, err)
		m.lastQueryCacheHit = false
		return nil, fmt.Errorf("mock cache GetQueryIDs gob decoding failed for query key '%s': %w", queryKey, err)
	}

	return ids, nil
}

func (m *mockCacheClient) SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error {
	m.incrementCounter("SetQueryIDs") // Use helper

	// log.Printf("DEBUG SetQueryIDs: Setting query key: %s with %d IDs, expiration: %v", queryKey, len(ids), expiration)
	if len(ids) > 0 {
		// log.Printf("DEBUG SetQueryIDs: First few IDs: %v", ids[:min(3, len(ids))])
	}

	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	if err := encoder.Encode(ids); err != nil { // Directly encode ids
		// log.Printf("DEBUG SetQueryIDs: Error marshaling for query key %s: %v", queryKey, err)
		return fmt.Errorf("mock cache SetQueryIDs gob encoding failed for query key '%s': %w", queryKey, err)
	}
	data := buf.Bytes()

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

func (m *mockDBAdapter) GetCount(ctx context.Context, tableName string, where string, args []interface{}) (int64, error) {
	m.GetCountCalls++
	return m.SQLiteAdapter.GetCount(ctx, tableName, where, args)
}

func (m *mockDBAdapter) BeginTx(ctx context.Context, opts *sql.TxOptions) (thing.Tx, error) {
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

type mockSQLResult struct {
	rowsAffected int64
	lastInsertID int64
}

func (r mockSQLResult) LastInsertId() (int64, error) {
	return r.lastInsertID, nil
}

func (r mockSQLResult) RowsAffected() (int64, error) {
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
	return mockSQLResult{rowsAffected: 1, lastInsertID: 0}, nil
}

func (tx *mockTx) Commit() error {
	return nil
}

func (tx *mockTx) Rollback() error {
	return nil
}

// FlushAll simulates flushing the cache (clears store and expiry).
func (m *mockCacheClient) FlushAll(ctx context.Context) error {
	m.Reset() // Reuse Reset logic
	return nil
}

// SetCount simulates storing a count value for a key.
func (m *mockCacheClient) SetCount(ctx context.Context, key string, count int64, expiration time.Duration) error {
	m.incrementCounter("SetCount")
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	if err := encoder.Encode(count); err != nil {
		return fmt.Errorf("mock SetCount gob encoding failed: %w", err)
	}
	data := buf.Bytes()

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
	buf := bytes.NewBuffer(storedBytes)
	decoder := gob.NewDecoder(buf)
	if err := decoder.Decode(&count); err != nil {
		return 0, fmt.Errorf("mock GetCount gob decoding failed: %w", err)
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
func (m *mockCacheClient) GetCacheStats(ctx context.Context) thing.CacheStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy of the counters
	stats := make(map[string]int)
	for k, v := range m.Counters {
		stats[k] = v
	}
	return thing.CacheStats{Counters: stats}
}

// Incr is a mock implementation for CacheClient.Incr
func (m *mockCacheClient) Incr(ctx context.Context, key string) (int64, error) {
	m.incrementCounter("Incr")
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isExpired(ctx, key) { // Check expiry under full lock
		m.store.Delete(key)
		m.expiryStore.Delete(key)
	}

	val, _ := m.store.LoadOrStore(key, "0")
	valStr, ok := val.(string)
	if !ok {
		// Should not happen if LoadOrStore stores "0" or if existing value was string
		m.store.Store(key, "1") // Initialize to 1 if type was wrong
		return 1, nil
	}

	currentVal, err := strconv.ParseInt(valStr, 10, 64)
	if err != nil {
		// If parsing fails (e.g., value was not a number), start from 1
		log.Printf("mockCacheClient: Incr: ParseInt error for key '%s' value '%s': %v. Setting to 1.", key, valStr, err)
		m.store.Store(key, "1")
		return 1, nil
	}

	currentVal++
	m.store.Store(key, strconv.FormatInt(currentVal, 10))
	return currentVal, nil
}

// Expire is a mock implementation for CacheClient.Expire
func (m *mockCacheClient) Expire(ctx context.Context, key string, expiration time.Duration) error {
	m.incrementCounter("Expire")
	m.mu.Lock()
	defer m.mu.Unlock()

	if expiration <= 0 {
		m.expiryStore.Delete(key)
		// log.Printf("mockCacheClient: Expire called for key '%s' with non-positive duration, removing expiry.", key)
		return nil
	}
	m.expiryStore.Store(key, time.Now().Add(expiration))
	// log.Printf("mockCacheClient: Expire called for key '%s' with duration %v, new expiry set.", key, expiration)
	return nil
}

// isExpired checks if a key is expired and deletes it if so.
// It expects m.mu (RLock or Lock) to be held by the caller if necessary for atomicity with the Load.
// Returns true if expired.
func (m *mockCacheClient) isExpired(ctx context.Context, key string) bool {
	if exp, ok := m.expiryStore.Load(key); ok {
		if time.Now().After(exp.(time.Time)) {
			// To avoid deadlock if Delete calls this or acquires full lock,
			// we must release the RLock before calling Delete.
			// This implies callers of isExpired need to handle locking carefully.
			// For simplicity in mock, let's assume Delete can be called.
			// A more robust way would be for Delete to handle its own locks
			// and for this function to just return expiry status.
			// Let's make Delete handle its own lock and this just check.
			// The caller of Get/GetModel/etc. will then call Delete.
			return true
		}
	}
	return false
}

var _ thing.CacheClient = (*mockCacheClient)(nil) // Compile-time check
