package thing_test

import (
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Import the package we are testing
	"thing"
)

func TestCacheIndex_RegisterAndGet(t *testing.T) {
	// Use the exported NewCacheIndex constructor
	idx := thing.NewCacheIndex()

	params1 := thing.QueryParams{Where: "status = ?", Args: []interface{}{1}}
	params2 := thing.QueryParams{Where: "name = ?", Args: []interface{}{"test"}}
	params3 := thing.QueryParams{Where: "status = ?", Args: []interface{}{2}} // Different params, same table

	table1 := "users"
	table2 := "posts"

	key1_1 := "list:users:hash1"
	key1_2 := "count:users:hash1"
	key2_1 := "list:posts:hash2"
	key3_1 := "list:users:hash3"

	// Register queries
	idx.RegisterQuery(table1, key1_1, params1)
	idx.RegisterQuery(table1, key1_2, params1)
	idx.RegisterQuery(table2, key2_1, params2)
	idx.RegisterQuery(table1, key3_1, params3)

	// --- Test GetPotentiallyAffectedQueries ---
	affectedTable1 := idx.GetPotentiallyAffectedQueries(table1)
	sort.Strings(affectedTable1) // Sort for consistent comparison
	expectedTable1 := []string{key1_1, key1_2, key3_1}
	sort.Strings(expectedTable1)
	assert.Equal(t, expectedTable1, affectedTable1, "Incorrect affected queries for table1")

	affectedTable2 := idx.GetPotentiallyAffectedQueries(table2)
	sort.Strings(affectedTable2)
	expectedTable2 := []string{key2_1}
	sort.Strings(expectedTable2)
	assert.Equal(t, expectedTable2, affectedTable2, "Incorrect affected queries for table2")

	affectedNonExistent := idx.GetPotentiallyAffectedQueries("non_existent_table")
	assert.Empty(t, affectedNonExistent, "Expected empty slice for non-existent table")

	// --- Test GetQueryParamsForKey ---
	retrievedParams1, found1 := idx.GetQueryParamsForKey(key1_1)
	assert.True(t, found1, "Expected to find params for key1_1")
	assert.Equal(t, params1, retrievedParams1, "Params mismatch for key1_1")

	retrievedParams1Count, found1Count := idx.GetQueryParamsForKey(key1_2)
	assert.True(t, found1Count, "Expected to find params for key1_2 (count)")
	assert.Equal(t, params1, retrievedParams1Count, "Params mismatch for key1_2 (count)")

	retrievedParams2, found2 := idx.GetQueryParamsForKey(key2_1)
	assert.True(t, found2, "Expected to find params for key2_1")
	assert.Equal(t, params2, retrievedParams2, "Params mismatch for key2_1")

	retrievedParams3, found3 := idx.GetQueryParamsForKey(key3_1)
	assert.True(t, found3, "Expected to find params for key3_1")
	assert.Equal(t, params3, retrievedParams3, "Params mismatch for key3_1")

	_, foundNonExistent := idx.GetQueryParamsForKey("non_existent_key")
	assert.False(t, foundNonExistent, "Expected not to find params for non-existent key")
}

func TestCacheIndex_RegisterQuery_EmptyInputs(t *testing.T) {
	idx := thing.NewCacheIndex()

	// Test empty table name
	idx.RegisterQuery("", "key1", thing.QueryParams{})
	affected := idx.GetPotentiallyAffectedQueries("")
	assert.Nil(t, affected, "Expected nil slice for empty table name query") // GetPotentiallyAffectedQueries returns nil for empty string
	params, found := idx.GetQueryParamsForKey("key1")
	assert.False(t, found, "Should not store params with empty table name")
	assert.Equal(t, thing.QueryParams{}, params)

	// Test empty cache key
	idx.RegisterQuery("table1", "", thing.QueryParams{Where: "a=1"})
	affected = idx.GetPotentiallyAffectedQueries("table1")
	assert.Empty(t, affected, "Affected queries should be empty after registering with empty key")
	params, found = idx.GetQueryParamsForKey("")
	assert.False(t, found, "Should not store params with empty key")
	assert.Equal(t, thing.QueryParams{}, params)

	// Test both empty
	idx.RegisterQuery("", "", thing.QueryParams{Where: "b=2"})
	affected = idx.GetPotentiallyAffectedQueries("")
	assert.Nil(t, affected, "Expected nil slice for empty table name query")
	params, found = idx.GetQueryParamsForKey("")
	assert.False(t, found, "Should not store params with empty key")
	assert.Equal(t, thing.QueryParams{}, params)
}

// Helper function to run index operations concurrently
func runConcurrently(t *testing.T, idx *thing.CacheIndex, numGoroutines int, opsPerGoroutine int) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				table := fmt.Sprintf("table_%d", (goroutineID+j)%10) // Spread across a few tables
				key := fmt.Sprintf("key_%d_%d", goroutineID, j)
				params := thing.QueryParams{Where: "col = ?", Args: []interface{}{goroutineID*opsPerGoroutine + j}}

				// Mix of operations
				if j%3 == 0 {
					idx.RegisterQuery(table, key, params)
				} else if j%3 == 1 {
					_ = idx.GetPotentiallyAffectedQueries(table)
				} else {
					_, _ = idx.GetQueryParamsForKey(key)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestCacheIndex_Concurrency(t *testing.T) {
	idx := thing.NewCacheIndex()
	numGoroutines := 50
	opsPerGoroutine := 100

	// No explicit assertions needed here, the goal is to ensure no race conditions occur.
	// Run `go test -race ./...` to detect races.
	require.NotPanics(t, func() {
		runConcurrently(t, idx, numGoroutines, opsPerGoroutine)
	}, "Concurrent access to CacheIndex panicked")

	// Optional: Add some basic checks after concurrent operations to ensure state seems reasonable
	// (e.g., check if some expected keys/tables exist), but the primary goal is race detection.
}
