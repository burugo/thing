package thing_test

import (
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Import the package we are testing
	"thing/internal/cache"
)

func TestCacheIndex_RegisterAndGet(t *testing.T) {
	// Reset before test
	cache.ResetGlobalCacheIndex()

	index := cache.GlobalCacheIndex // Use exported global

	params1 := cache.QueryParams{Where: "id = ?", Args: []interface{}{1}}        // Use internal type
	params2 := cache.QueryParams{Where: "name = ?", Args: []interface{}{"test"}} // Use internal type
	params3 := cache.QueryParams{Where: "status = ?", Args: []interface{}{1}}

	table1 := "users"
	table2 := "posts"

	key1_1 := "list:users:hash1"
	key1_2 := "count:users:hash1"
	key2_1 := "list:posts:hash2"
	key3_1 := "list:users:hash3"

	// Register queries
	index.RegisterQuery(table1, key1_1, params1)
	index.RegisterQuery(table1, key1_2, params1)
	index.RegisterQuery(table2, key2_1, params2)
	index.RegisterQuery(table1, key3_1, params3)

	// --- Test GetPotentiallyAffectedQueries ---
	affectedTable1 := index.GetPotentiallyAffectedQueries(table1)
	sort.Strings(affectedTable1) // Sort for consistent comparison
	expectedTable1 := []string{key1_1, key1_2, key3_1}
	sort.Strings(expectedTable1)
	assert.Equal(t, expectedTable1, affectedTable1, "Incorrect affected queries for table1")

	affectedTable2 := index.GetPotentiallyAffectedQueries(table2)
	sort.Strings(affectedTable2)
	expectedTable2 := []string{key2_1}
	sort.Strings(expectedTable2)
	assert.Equal(t, expectedTable2, affectedTable2, "Incorrect affected queries for table2")

	affectedNonExistent := index.GetPotentiallyAffectedQueries("non_existent_table")
	assert.Empty(t, affectedNonExistent, "Expected empty slice for non-existent table")

	// --- Test GetQueryParamsForKey ---
	retrievedParams1, found1 := index.GetQueryParamsForKey(key1_1)
	assert.True(t, found1, "Expected to find params for key1_1")
	assert.Equal(t, params1, retrievedParams1, "Params mismatch for key1_1")

	retrievedParams1Count, found1Count := index.GetQueryParamsForKey(key1_2)
	assert.True(t, found1Count, "Expected to find params for key1_2 (count)")
	assert.Equal(t, params1, retrievedParams1Count, "Params mismatch for key1_2 (count)")

	retrievedParams2, found2 := index.GetQueryParamsForKey(key2_1)
	assert.True(t, found2, "Expected to find params for key2_1")
	assert.Equal(t, params2, retrievedParams2, "Params mismatch for key2_1")

	retrievedParams3, found3 := index.GetQueryParamsForKey(key3_1)
	assert.True(t, found3, "Expected to find params for key3_1")
	assert.Equal(t, params3, retrievedParams3, "Params mismatch for key3_1")

	_, foundNonExistent := index.GetQueryParamsForKey("non_existent_key")
	assert.False(t, foundNonExistent, "Expected not to find params for non-existent key")
}

func TestCacheIndex_RegisterQuery_EmptyInputs(t *testing.T) {
	cache.ResetGlobalCacheIndex()
	index := cache.GlobalCacheIndex

	// Test empty table name
	index.RegisterQuery("", "key1", cache.QueryParams{})
	affected := index.GetPotentiallyAffectedQueries("")
	assert.Nil(t, affected, "Expected nil slice for empty table name query")
	params, found := index.GetQueryParamsForKey("key1")
	assert.False(t, found, "Should not store params with empty table name")
	assert.Equal(t, cache.QueryParams{}, params)

	// Test empty cache key
	index.RegisterQuery("table1", "", cache.QueryParams{Where: "a=1"})
	affected = index.GetPotentiallyAffectedQueries("table1")
	assert.Empty(t, affected, "Affected queries should be empty after registering with empty key")
	params, found = index.GetQueryParamsForKey("")
	assert.False(t, found, "Should not store params with empty key")
	assert.Equal(t, cache.QueryParams{}, params)

	// Test both empty
	index.RegisterQuery("", "", cache.QueryParams{Where: "b=2"})
	affected = index.GetPotentiallyAffectedQueries("")
	assert.Nil(t, affected, "Expected nil slice for empty table name query")
	params, found = index.GetQueryParamsForKey("")
	assert.False(t, found, "Should not store params with empty key")
	assert.Equal(t, cache.QueryParams{}, params)
}

// Helper function to run index operations concurrently
func runConcurrently(t *testing.T, index *cache.CacheIndex, numGoroutines int, opsPerGoroutine int) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				table := fmt.Sprintf("table_%d", (goroutineID+j)%10) // Spread across a few tables
				key := fmt.Sprintf("key_%d_%d", goroutineID, j)
				params := cache.QueryParams{Where: "col = ?", Args: []interface{}{goroutineID*opsPerGoroutine + j}}

				// Mix of operations
				if j%3 == 0 {
					index.RegisterQuery(table, key, params)
				} else if j%3 == 1 {
					_ = index.GetPotentiallyAffectedQueries(table)
				} else {
					_, _ = index.GetQueryParamsForKey(key)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestCacheIndex_Concurrency(t *testing.T) {
	cache.ResetGlobalCacheIndex()
	index := cache.GlobalCacheIndex // Use exported global
	numGoroutines := 50
	opsPerGoroutine := 100

	// No explicit assertions needed here, the goal is to ensure no race conditions occur.
	// Run `go test -race ./...` to detect races.
	require.NotPanics(t, func() {
		runConcurrently(t, index, numGoroutines, opsPerGoroutine)
	}, "Concurrent access to CacheIndex panicked")

	// Optional: Add some basic checks after concurrent operations to ensure state seems reasonable
	// (e.g., check if some expected keys/tables exist), but the primary goal is race detection.
}

func TestCacheIndex_GetPotentiallyAffectedQueries_Empty(t *testing.T) {
	cache.ResetGlobalCacheIndex()
	index := cache.GlobalCacheIndex // Use exported global

	keys := index.GetPotentiallyAffectedQueries("nonexistent_table")
	assert.Empty(t, keys)
}

func TestCacheIndex_GetQueryParamsForKey_NotFound(t *testing.T) {
	cache.ResetGlobalCacheIndex()
	index := cache.GlobalCacheIndex // Use exported global

	params, found := index.GetQueryParamsForKey("nonexistent_key")
	assert.False(t, found)
	assert.Equal(t, cache.QueryParams{}, params) // Use internal type
}

func TestParseExactMatchFields(t *testing.T) {
	testCases := []struct {
		name   string
		params cache.QueryParams
		expect map[string][]interface{}
	}{
		{
			name:   "single =",
			params: cache.QueryParams{Where: "id = ?", Args: []interface{}{1}},
			expect: map[string][]interface{}{"id": {1}},
		},
		{
			name:   "multiple =",
			params: cache.QueryParams{Where: "id = ? AND status = ?", Args: []interface{}{1, "active"}},
			expect: map[string][]interface{}{"id": {1}, "status": {"active"}},
		},
		{
			name:   "single IN",
			params: cache.QueryParams{Where: "user_id IN (?)", Args: []interface{}{[]int{1, 2, 3}}},
			expect: map[string][]interface{}{"user_id": {1, 2, 3}},
		},
		{
			name:   "mixed = and IN",
			params: cache.QueryParams{Where: "user_id IN (?) AND status = ?", Args: []interface{}{[]int{1, 2}, "active"}},
			expect: map[string][]interface{}{"user_id": {1, 2}, "status": {"active"}},
		},
		{
			name:   "non-exact operator ignored",
			params: cache.QueryParams{Where: "age > ? AND id = ?", Args: []interface{}{18, 2}},
			expect: map[string][]interface{}{"id": {2}},
		},
		{
			name:   "empty where",
			params: cache.QueryParams{Where: "", Args: nil},
			expect: map[string][]interface{}{},
		},
		{
			name:   "IN with string slice",
			params: cache.QueryParams{Where: "name IN (?)", Args: []interface{}{[]string{"a", "b"}}},
			expect: map[string][]interface{}{"name": {"a", "b"}},
		},
		{
			name:   "IN with interface{} slice",
			params: cache.QueryParams{Where: "tag IN (?)", Args: []interface{}{[]interface{}{1, "x"}}},
			expect: map[string][]interface{}{"tag": {1, "x"}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := cache.ParseExactMatchFields(tc.params)
			assert.Equal(t, tc.expect, got)
		})
	}
}

func TestCacheIndex_GetKeysByValue(t *testing.T) {
	cache.ResetGlobalCacheIndex()
	idx := cache.GlobalCacheIndex

	table := "users"
	key1 := "list:users:by_user_id_1"
	key2 := "list:users:by_user_id_2"
	key3 := "list:users:by_status_active"
	key4 := "list:users:by_status_inactive"
	params1 := cache.QueryParams{Where: "user_id = ?", Args: []interface{}{1}}
	params2 := cache.QueryParams{Where: "user_id = ?", Args: []interface{}{2}}
	params3 := cache.QueryParams{Where: "status = ?", Args: []interface{}{"active"}}
	params4 := cache.QueryParams{Where: "status = ?", Args: []interface{}{"inactive"}}

	idx.RegisterQuery(table, key1, params1)
	idx.RegisterQuery(table, key2, params2)
	idx.RegisterQuery(table, key3, params3)
	idx.RegisterQuery(table, key4, params4)

	t.Run("single value hit", func(t *testing.T) {
		keys := idx.GetKeysByValue(table, "user_id", 1)
		assert.ElementsMatch(t, []string{key1}, keys)
	})
	t.Run("single value miss", func(t *testing.T) {
		keys := idx.GetKeysByValue(table, "user_id", 99)
		assert.Empty(t, keys)
	})
	t.Run("multiple values", func(t *testing.T) {
		keys := idx.GetKeysByValue(table, "status", "active")
		assert.ElementsMatch(t, []string{key3}, keys)
		keys2 := idx.GetKeysByValue(table, "status", "inactive")
		assert.ElementsMatch(t, []string{key4}, keys2)
	})
	t.Run("different field", func(t *testing.T) {
		keys := idx.GetKeysByValue(table, "nonexistent", "x")
		assert.Empty(t, keys)
	})
	// 跨表测试
	idx.RegisterQuery("posts", "list:posts:by_user_id_1", cache.QueryParams{Where: "user_id = ?", Args: []interface{}{1}})
	keys := idx.GetKeysByValue("posts", "user_id", 1)
	assert.ElementsMatch(t, []string{"list:posts:by_user_id_1"}, keys)
}
