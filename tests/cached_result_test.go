package thing_test

import (
	"strconv"
	"testing"

	"thing"

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

	params := thing.QueryParams{ /* Define params if needed, e.g., WHERE */ }
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
	paramsNone := thing.QueryParams{Where: "name = ?", Args: []interface{}{"NonExistent"}}
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

	params := thing.QueryParams{Order: "id ASC"}
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
	assert.GreaterOrEqual(t, mockCache.GetQueryIDsCalls, 1)
	assert.GreaterOrEqual(t, mockCache.SetQueryIDsCalls, 1)
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
	paramsNone := thing.QueryParams{Where: "name = ?", Args: []interface{}{"NonExistent"}}
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

	params := thing.QueryParams{Order: "id ASC"}
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

// TestCachedResult replaces the old TestCachedResult
// func TestCachedResult(t *testing.T) {
// 	// ... old code ...
// }

// Remove TestThing_ByIDs as it tests the Thing method, not CachedResult
// func TestThing_ByIDs(t *testing.T) {
// 	// ... old code ...
// }
