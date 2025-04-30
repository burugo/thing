package thing_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"thing"
	"thing/internal/cache"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCachedResult_Count tests the Count() method with caching.
func TestCachedResult_Count(t *testing.T) {
	th, mockCache, _, _ := setupCacheTest[User](t)

	// --- Setup Data ---
	err := th.Save(&User{Name: "Count User 1"})
	require.NoError(t, err)
	err = th.Save(&User{Name: "Count User 2"})
	require.NoError(t, err)

	params := cache.QueryParams{ /* Define params if needed, e.g., WHERE */ }
	result, err := th.Query(params)
	require.NoError(t, err)
	require.NotNil(t, result)

	// --- Test Count - Cache Miss ---
	count, err := result.Count() // No ctx
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "Count should be 2 initially")
	// Verify DB was hit (assuming mock DB tracks calls or GetCount implemented)
	// Verify cache was set (check mockCache.SetCalls or get value)

	// --- Test Count - Cache Hit ---
	// Reset DB hit counter if possible
	// mockDB.ResetCountCalls() // Assuming mock DB has this
	countCacheHit, err := result.Count() // Call again
	require.NoError(t, err)
	assert.Equal(t, int64(2), countCacheHit, "Count should be 2 from cache")
	// Verify DB was NOT hit again
	// assert.Zero(t, mockDB.GetCountCalls)

	// --- Test Count - Zero Results & NoneResult Caching ---
	mockCache.Reset() // Clear cache
	paramsNone := cache.QueryParams{Where: "name = ?", Args: []interface{}{"NonExistent"}}
	resultNone, err := th.Query(paramsNone)
	require.NoError(t, err)
	require.NotNil(t, resultNone)

	countNone, err := resultNone.Count() // DB Miss
	require.NoError(t, err)
	assert.Equal(t, int64(0), countNone, "Count for non-existent should be 0")

	// Verify NoneResult wasn't set for count, but the count "0" was set
	// We need the cache key generator logic or access to the generated key
	// countKey, _ := resultNone.generateCountCacheKey() // Need access or pre-compute
	// cacheVal, found := mockCache.GetValue(countKey)
	// assert.True(t, found)
	// assert.NotEqual(t, thing.NoneResult, string(cacheVal))
	// assert.Equal(t, "0", string(cacheVal))

	countNoneCached, err := resultNone.Count() // Cache Hit
	require.NoError(t, err)
	assert.Equal(t, int64(0), countNoneCached, "Count for non-existent should be 0 from cache")
}

// TestCachedResult_Fetch tests the Fetch() method with caching.
func TestCachedResult_Fetch(t *testing.T) {
	th, mockCache, _, _ := setupCacheTest[User](t)

	// --- Setup Data ---
	var expectedIDs []int64
	var err error
	for i := 0; i < 5; i++ {
		u := &User{Name: "Fetch User " + strconv.Itoa(i)}
		err = th.Save(u)
		require.NoError(t, err)
		expectedIDs = append(expectedIDs, u.ID)
	}

	params := cache.QueryParams{Order: "id ASC"}
	result, err := th.Query(params)
	require.NoError(t, err)
	require.NotNil(t, result)

	// --- Test Fetch - Page 1 (Cache Miss for IDs) ---
	users1, err := result.Fetch(0, 2) // No ctx
	require.NoError(t, err)
	require.Len(t, users1, 2, "Page 1 should have 2 users")
	assert.Equal(t, expectedIDs[0], users1[0].ID)
	assert.Equal(t, expectedIDs[1], users1[1].ID)
	// Verify GetQueryIDs was called (miss), SetQueryIDs was called
	t.Logf("DEBUG: Before assert - GetQueryIDsCalls: %d, SetQueryIDsCalls: %d", mockCache.Counters["GetQueryIDs"], mockCache.Counters["SetQueryIDs"])
	assert.GreaterOrEqual(t, mockCache.Counters["GetQueryIDs"], 1, "GetQueryIDs should have been called")
	assert.GreaterOrEqual(t, mockCache.Counters["SetQueryIDs"], 1, "SetQueryIDs should have been called")
	// Verify ByIDs was called for these 2 IDs (object cache miss likely)

	// --- Test Fetch - Page 2 (Cache Hit for IDs) ---
	// Reset ByIDs counter if possible
	users2, err := result.Fetch(2, 2)
	require.NoError(t, err)
	require.Len(t, users2, 2, "Page 2 should have 2 users")
	assert.Equal(t, expectedIDs[2], users2[0].ID)
	assert.Equal(t, expectedIDs[3], users2[1].ID)
	// Verify GetQueryIDs was NOT called again (IDs are cached in CachedResult)
	// Verify ByIDs was called for these 2 IDs

	// --- Test Fetch - Beyond Cached IDs (Direct DB Query) ---
	// Assuming cacheListCountLimit = 300, this test won't trigger direct fetch yet.
	// Need more setup or a specific test with offset > cacheListCountLimit.

	// --- Test Fetch - Zero Results & NoneResult Caching ---
	mockCache.Reset()
	paramsNone := cache.QueryParams{Where: "name = ?", Args: []interface{}{"NonExistent"}}
	resultNone, err := th.Query(paramsNone)
	require.NoError(t, err)
	require.NotNil(t, resultNone)

	usersNone, err := resultNone.Fetch(0, 10) // DB Miss for IDs
	require.NoError(t, err)
	assert.Len(t, usersNone, 0, "Fetch for non-existent should return empty slice")

	// Verify NoneResult was set for the list key
	// listKey, _ := resultNone.generateListCacheKey() // Need access or pre-compute
	// cacheVal, found := mockCache.GetValue(listKey)
	// assert.True(t, found)
	// assert.Equal(t, thing.NoneResult, string(cacheVal))

	usersNoneCached, err := resultNone.Fetch(0, 10) // Cache Hit (NoneResult)
	require.NoError(t, err)
	assert.Len(t, usersNoneCached, 0, "Fetch for non-existent should return empty slice from cache")
}

// TestCachedResult_All tests the Fetch() method simulating an 'All' scenario
func TestCachedResult_All(t *testing.T) {
	th, _, _, _ := setupCacheTest[User](t)
	// ctx := context.Background() // Removed as Fetch doesn't use context

	// Setup
	var expectedIDs []int64
	var err error
	for i := 0; i < 3; i++ {
		u := &User{Name: "All User " + strconv.Itoa(i)}
		err = th.Save(u)
		require.NoError(t, err)
		expectedIDs = append(expectedIDs, u.ID)
	}

	params := cache.QueryParams{Order: "id ASC"}
	result, err := th.Query(params)
	require.NoError(t, err)

	// Call Fetch instead of All
	// Fetch a reasonable number of items expected for an "All" scenario
	allUsersFetched, fetchErr := result.Fetch(0, 100) // Fetch up to 100 items
	require.NoError(t, fetchErr)
	assert.Len(t, allUsersFetched, 3)
	assert.Equal(t, expectedIDs[0], allUsersFetched[0].ID)
	assert.Equal(t, expectedIDs[1], allUsersFetched[1].ID)
	assert.Equal(t, expectedIDs[2], allUsersFetched[2].ID)

	// Call Fetch again (should hit cache if Fetch implements it, or at least reuse cached IDs)
	allUsersCachedFetched, fetchErrCached := result.Fetch(0, 100) // Fetch again
	require.NoError(t, fetchErrCached)
	assert.Len(t, allUsersCachedFetched, 3)
	// Check if underlying pointers are the same (depends on ByIDs caching)
	assert.Equal(t, allUsersFetched, allUsersCachedFetched, "Fetching again should return same result (pointers might differ based on ByIDs cache)")
}

// TestCachedResult_First tests the First() method with caching.
func TestCachedResult_First(t *testing.T) {
	db, cacheClient, cleanup := setupTestDB(t)
	defer cleanup()
	thingInstance, err := thing.New[User](db, cacheClient)
	require.NoError(t, err)

	// Type assert to *mockCacheClient for mock-only methods
	mockCache, ok := cacheClient.(*mockCacheClient)
	require.True(t, ok, "cacheClient is not a *mockCacheClient")

	// Seed data
	u1 := User{Name: "FirstUser", Email: "first@example.com"}
	require.NoError(t, thingInstance.Save(&u1))
	u2 := User{Name: "SecondUser", Email: "second@example.com"}
	require.NoError(t, thingInstance.Save(&u2))

	t.Run("Find First Match", func(t *testing.T) {
		mockCache.FlushAll(context.Background())
		params := cache.QueryParams{Where: "name LIKE ?", Args: []interface{}{"%User"}, Order: "id ASC"}
		cr, err := thingInstance.Query(params)
		require.NoError(t, err)
		firstUser, err := cr.First()
		require.NoError(t, err)
		require.NotNil(t, firstUser)
		require.Equal(t, u1.ID, firstUser.ID)
		require.Equal(t, u1.Name, firstUser.Name)
	})

	t.Run("Find First Match (Different Order)", func(t *testing.T) {
		mockCache.FlushAll(context.Background())
		params := cache.QueryParams{Where: "name LIKE ?", Args: []interface{}{"%User"}, Order: "id DESC"}
		cr, err := thingInstance.Query(params)
		require.NoError(t, err)
		firstUser, err := cr.First()
		require.NoError(t, err)
		require.NotNil(t, firstUser)
		require.Equal(t, u2.ID, firstUser.ID) // Should be u2 because of DESC order
		require.Equal(t, u2.Name, firstUser.Name)
	})

	t.Run("Not Found", func(t *testing.T) {
		mockCache.FlushAll(context.Background())
		params := cache.QueryParams{Where: "name = ?", Args: []interface{}{"NonExistent"}}
		cr, err := thingInstance.Query(params)
		require.NoError(t, err)
		firstUser, err := cr.First()
		require.Error(t, err)
		require.True(t, errors.Is(err, thing.ErrNotFound))
		require.Nil(t, firstUser)
	})

	t.Run("Cache Hit (List Cache -> ByID)", func(t *testing.T) {
		mockCache.FlushAll(context.Background())
		params := cache.QueryParams{Where: "name = ?", Args: []interface{}{u1.Name}}
		cacheKey := testGenerateListCacheKey(t, thingInstance, params)
		countCacheKey := testGenerateCountCacheKey(t, thingInstance, params)

		// Pre-populate list cache with ID
		require.NoError(t, mockCache.SetListIDs(context.Background(), cacheKey, []int64{u1.ID}, 0, time.Minute))
		require.NoError(t, mockCache.SetCount(context.Background(), countCacheKey, 1, time.Minute))
		// Ensure model cache for u1 itself is empty initially
		modelCacheKey := fmt.Sprintf("users:%d", u1.ID)
		require.NoError(t, mockCache.Delete(context.Background(), modelCacheKey))

		// Reset DB call counts
		// If you need to assert DB calls, you can type assert db to *mockDBAdapter here
		// mockDBAdapter, ok := db.(*mockDBAdapter)
		// require.True(t, ok, "Test setup error: db is not a mockDBAdapter")
		// mockDBAdapter.ResetCounts()
		mockCache.ResetCounts()

		cr, err := thingInstance.Query(params)
		require.NoError(t, err)
		firstUser, err := cr.First()
		require.NoError(t, err)
		require.NotNil(t, firstUser)
		require.Equal(t, u1.ID, firstUser.ID)

		// Assertions:
		// 1. List cache was checked (GetListIDs called)
		// 2. Model cache was checked for u1 (GetModel called for users:u1.ID)
		// 3. DB was NOT called for the list (Select count should be 0)
		// 4. DB *was* called to fetch u1 by ID (Get count should be 1)
		require.GreaterOrEqual(t, mockCache.GetListIDsCount(), 1, "GetListIDs should have been called")
		require.GreaterOrEqual(t, mockCache.GetModelCount(), 1, "GetModel should have been called for the ID")
		// DB call assertions can be added if db is a mock
		// require.Equal(t, 0, mockDBAdapter.SelectCount, "DB Select (for list) should NOT have been called")
		// require.Equal(t, 1, mockDBAdapter.GetCount, "DB Get (for ID) should have been called")
	})

	// TODO: Add test case for cache miss -> DB query with LIMIT 1
}

// Helper to generate list cache key for testing
func testGenerateListCacheKey[T any](t *testing.T, instance *thing.Thing[T], params cache.QueryParams) string {
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	info, err := thing.GetCachedModelInfo(modelType)
	require.NoError(t, err)
	return thing.GenerateCacheKey("list", info.TableName, params)
}

// Helper to generate count cache key for testing
func testGenerateCountCacheKey[T any](t *testing.T, instance *thing.Thing[T], params cache.QueryParams) string {
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	info, err := thing.GetCachedModelInfo(modelType)
	require.NoError(t, err)
	return thing.GenerateCacheKey("count", info.TableName, params)
}
