package thing_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	// Import the package we are testing
	"github.com/burugo/thing/internal/cache"
	"github.com/burugo/thing/internal/types"
)

func TestCacheIndex_RegisterAndGet(t *testing.T) {
	// Reset before test
	cache.ResetGlobalCacheIndex()

	index := cache.GlobalCacheIndex // Use exported global

	params1 := types.QueryParams{Where: "id = ?", Args: []interface{}{1}}        // Use internal type
	params2 := types.QueryParams{Where: "name = ?", Args: []interface{}{"test"}} // Use internal type
	params3 := types.QueryParams{Where: "status = ?", Args: []interface{}{1}}

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

func TestCacheIndex_GetQueryParamsForKey_NotFound(t *testing.T) {
	cache.ResetGlobalCacheIndex()
	index := cache.GlobalCacheIndex // Use exported global

	params, found := index.GetQueryParamsForKey("nonexistent_key")
	assert.False(t, found)
	assert.Equal(t, types.QueryParams{}, params) // Use internal type
}

func TestParseExactMatchFields(t *testing.T) {
	testCases := []struct {
		name   string
		params types.QueryParams
		expect map[string][]interface{}
	}{
		{
			name:   "single =",
			params: types.QueryParams{Where: "id = ?", Args: []interface{}{1}},
			expect: map[string][]interface{}{"id": {1}},
		},
		{
			name:   "multiple =",
			params: types.QueryParams{Where: "id = ? AND status = ?", Args: []interface{}{1, "active"}},
			expect: map[string][]interface{}{"id": {1}, "status": {"active"}},
		},
		{
			name:   "single IN",
			params: types.QueryParams{Where: "user_id IN (?)", Args: []interface{}{[]int{1, 2, 3}}},
			expect: map[string][]interface{}{"user_id": {1, 2, 3}},
		},
		{
			name:   "mixed = and IN",
			params: types.QueryParams{Where: "user_id IN (?) AND status = ?", Args: []interface{}{[]int{1, 2}, "active"}},
			expect: map[string][]interface{}{"user_id": {1, 2}, "status": {"active"}},
		},
		{
			name:   "non-exact operator ignored",
			params: types.QueryParams{Where: "age > ? AND id = ?", Args: []interface{}{18, 2}},
			expect: map[string][]interface{}{"id": {2}},
		},
		{
			name:   "empty where",
			params: types.QueryParams{Where: "", Args: nil},
			expect: map[string][]interface{}{},
		},
		{
			name:   "IN with string slice",
			params: types.QueryParams{Where: "name IN (?)", Args: []interface{}{[]string{"a", "b"}}},
			expect: map[string][]interface{}{"name": {"a", "b"}},
		},
		{
			name:   "IN with interface{} slice",
			params: types.QueryParams{Where: "tag IN (?)", Args: []interface{}{[]interface{}{1, "x"}}},
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
	params1 := types.QueryParams{Where: "user_id = ?", Args: []interface{}{1}}
	params2 := types.QueryParams{Where: "user_id = ?", Args: []interface{}{2}}
	params3 := types.QueryParams{Where: "status = ?", Args: []interface{}{"active"}}
	params4 := types.QueryParams{Where: "status = ?", Args: []interface{}{"inactive"}}

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
	idx.RegisterQuery("posts", "list:posts:by_user_id_1", types.QueryParams{Where: "user_id = ?", Args: []interface{}{1}})
	keys := idx.GetKeysByValue("posts", "user_id", 1)
	assert.ElementsMatch(t, []string{"list:posts:by_user_id_1"}, keys)
}

// Test Full-Table List Keys registration and retrieval
func TestCacheIndex_FullTableListKeys(t *testing.T) {
	cache.ResetGlobalCacheIndex()
	idx := cache.GlobalCacheIndex

	table := "users"
	listKey1 := "list:users:all1"
	listKey2 := "list:users:all2"
	countKey := "count:users:all"
	// Register full-table list keys
	idx.RegisterQuery(table, listKey1, types.QueryParams{Where: "", Args: nil})
	idx.RegisterQuery(table, listKey2, types.QueryParams{Where: "", Args: nil})
	// Register count key and filtered list key to ensure they are not included
	idx.RegisterQuery(table, countKey, types.QueryParams{Where: "", Args: nil})
	idx.RegisterQuery(table, "list:users:filtered", types.QueryParams{Where: "id = ?", Args: []interface{}{1}})

	keys := idx.GetFullTableListKeys(table)
	assert.ElementsMatch(t, []string{listKey1, listKey2}, keys)
}

// Test FieldIndex and valueIndex behaviors for mixed exact and non-exact conditions
func TestCacheIndex_FieldIndexAndValueIndex(t *testing.T) {
	cache.ResetGlobalCacheIndex()
	idx := cache.GlobalCacheIndex

	table := "items"
	key := "list:items:by_id_and_age"
	params := types.QueryParams{Where: "id = ? AND age > ?", Args: []interface{}{5, 30}}
	idx.RegisterQuery(table, key, params)

	// valueIndex should index only 'id' for exact match
	idKeys := idx.GetKeysByValue(table, "id", 5)
	assert.ElementsMatch(t, []string{key}, idKeys)
	// 'age' is non-exact operator, so valueIndex should not index it
	ageKeys := idx.GetKeysByValue(table, "age", 30)
	assert.Empty(t, ageKeys)

	// FieldIndex should index both 'id' and 'age'
	fieldMap, ok := idx.FieldIndex[table]
	assert.True(t, ok, "Expected field index for table")
	idFieldKeys, ok := fieldMap["id"]
	assert.True(t, ok, "Expected field index entry for 'id'")
	assert.Contains(t, idFieldKeys, key)
	ageFieldKeys, ok := fieldMap["age"]
	assert.True(t, ok, "Expected field index entry for 'age'")
	assert.Contains(t, ageFieldKeys, key)
}

// Test IN clause valueIndex registration for multiple values
func TestCacheIndex_INValueIndex(t *testing.T) {
	cache.ResetGlobalCacheIndex()
	idx := cache.GlobalCacheIndex

	table := "orders"
	key := "list:orders:by_status_in"
	params := types.QueryParams{Where: "status IN (?)", Args: []interface{}{[]string{"pending", "complete"}}}
	idx.RegisterQuery(table, key, params)

	pendingKeys := idx.GetKeysByValue(table, "status", "pending")
	completeKeys := idx.GetKeysByValue(table, "status", "complete")
	assert.ElementsMatch(t, []string{key}, pendingKeys)
	assert.ElementsMatch(t, []string{key}, completeKeys)
}
