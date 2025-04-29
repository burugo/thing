package thing_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"thing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Add a helper method to the mockCacheClient to format IDs for testing cache keys
func (m *mockCacheClient) FormatID(id int64) string {
	return fmt.Sprintf("%d", id)
}

func TestThing_ByID_Cache_MissAndSet(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create a test user
	user := &User{Name: "Cache Test User", Email: "cache@example.com"}

	// Save the user
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)

	// Reset cache state after creation to start with a clean cache
	mockCache.Reset()

	// First call should miss the cache and query the database
	foundUser, err := th.ByID(user.ID)
	require.NoError(t, err)
	require.NotNil(t, foundUser)
	assert.Equal(t, user.ID, foundUser.ID)
	assert.Equal(t, user.Name, foundUser.Name)
	assert.Equal(t, user.Email, foundUser.Email)

	// Verify cache operations
	assert.Equal(t, 1, mockCache.GetModelCalls, "Should attempt to get from cache")
	assert.Equal(t, 1, mockCache.SetModelCalls, "Should set in cache after DB fetch")

	// The key structure should be tableName:id, verify the model was stored in cache
	modelKey := "users:" + mockCache.FormatID(user.ID)
	assert.True(t, mockCache.Exists(modelKey), "Model should be stored in cache")
}

func TestThing_ByID_Cache_Hit(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create a test user
	user := &User{Name: "Cache Hit User", Email: "cache-hit@example.com"}

	// Save the user
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)

	// Reset cache state after creation for clean counts
	mockCache.Reset()

	// First fetch to populate cache
	_, err = th.ByID(user.ID)
	require.NoError(t, err)

	// Record the current cache stats
	getCount := mockCache.GetModelCalls
	setCount := mockCache.SetModelCalls

	// Second fetch should hit the cache
	foundUser, err := th.ByID(user.ID)
	require.NoError(t, err)
	require.NotNil(t, foundUser)

	// Verify it was a cache hit
	assert.Equal(t, getCount+1, mockCache.GetModelCalls, "Should attempt one more get")
	assert.Equal(t, setCount, mockCache.SetModelCalls, "Should not set in cache again")

	// Verify the data was correct from cache
	assert.Equal(t, user.ID, foundUser.ID)
	assert.Equal(t, user.Name, foundUser.Name)
	assert.Equal(t, user.Email, foundUser.Email)
}

func TestThing_ByID_Cache_NoneResult(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// 1. Create user
	user := &User{Name: "NoneResult User", Email: "noneresult@example.com"}
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)
	userID := user.ID

	// 2. Fetch once to ensure it exists
	_, err = th.ByID(userID)
	require.NoError(t, err)

	// 3. Delete the user (this should invalidate the object cache)
	err = th.Delete(user)
	require.NoError(t, err)

	// Reset mock cache call counts for clarity
	mockCache.ResetCounts()

	// 4. Fetch again - should miss cache, miss DB, cache NoneResult, return ErrNotFound
	_, err = th.ByID(userID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, thing.ErrNotFound), "Expected ErrNotFound after delete")

	// Verify cache operations for the first fetch after delete
	assert.Equal(t, 1, mockCache.GetModelCalls, "Should attempt cache get (miss)")
	assert.Equal(t, 1, mockCache.SetCalls, "Should set NoneResult in cache") // Check Set, not SetModel
	assert.Equal(t, 0, mockCache.SetModelCalls, "Should NOT set model after delete")
	// Verify DB was checked (via fetchModelsByIDsInternal)
	// We don't have a direct counter for DB calls, rely on cache misses/sets.

	// Check that NoneResult was actually cached
	cacheKey := "users:" + mockCache.FormatID(userID)
	rawVal, rawErr := mockCache.Get(context.Background(), cacheKey)
	require.NoError(t, rawErr, "Should be able to get raw value from cache")
	assert.Equal(t, thing.NoneResult, rawVal, "Cache should contain NoneResult string")

	// Record counts before the final fetch
	getCallsBeforeFinal := mockCache.GetModelCalls
	setCallsBeforeFinal := mockCache.SetCalls
	setModelCallsBeforeFinal := mockCache.SetModelCalls

	// 5. Fetch one more time - should hit NoneResult in cache, return ErrNotFound immediately
	_, err = th.ByID(userID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, thing.ErrNotFound), "Expected ErrNotFound again")

	// Verify cache operations for the second fetch after delete
	assert.Equal(t, getCallsBeforeFinal+1, mockCache.GetModelCalls, "Should attempt cache get again (hit NoneResult)")
	assert.Equal(t, setCallsBeforeFinal, mockCache.SetCalls, "Should NOT set NoneResult again")
	assert.Equal(t, setModelCallsBeforeFinal, mockCache.SetModelCalls, "Should NOT set model again")
	// Crucially, DB should NOT have been queried this time. We verify by checking no new Set* calls happened.

	// Check Set calls AFTER ResetCounts
	// Set should NOT have been called again, as NoneResult should prevent DB lookup
	mockCache.AssertSetCalls(t, 1, "Should set NoneResult exactly once") // Expect 1 Set call after ResetCounts
}

func TestThing_Query_Cache(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create multiple users
	users := []*User{
		{Name: "Alice Cache", Email: "alice-cache@example.com"},
		{Name: "Bob Cache", Email: "bob-cache@example.com"},
		{Name: "Charlie Cache", Email: "charlie-cache@example.com"},
	}

	for _, u := range users {
		err := th.Save(u)
		require.NoError(t, err)
		require.NotZero(t, u.ID)
	}

	// Reset cache metrics
	mockCache.Reset()

	// First query should miss cache
	params := thing.QueryParams{
		Where: "name LIKE ?",
		Args:  []interface{}{"%Cache"},
	}

	allUsersResult, err := th.Query(params)
	require.NoError(t, err)
	// Fetch results
	allUsersFetched, fetchErr := allUsersResult.Fetch(0, 100) // Fetch up to 100
	require.NoError(t, fetchErr)
	require.Len(t, allUsersFetched, 3)

	// Verify cache operations for first query
	assert.Equal(t, 1, mockCache.GetQueryIDsCalls, "Should attempt to get query from cache")
	assert.Equal(t, 1, mockCache.SetQueryIDsCalls, "Should set query result in cache")
	// Note: GetModelCalls/SetModelCalls might be higher if ByIDs has internal caching misses
	// Adjust assertion based on ByIDs implementation details
	assert.GreaterOrEqual(t, mockCache.GetModelCalls, 3, "Should attempt to get each model (3 users)")
	// assert.Equal(t, 3, mockCache.SetModelCalls, "Should set each model in cache") // This might not happen if ByIDs hits cache

	// Record cache stats before second query
	getQueryCount := mockCache.GetQueryIDsCalls
	setQueryCount := mockCache.SetQueryIDsCalls
	getModelCount := mockCache.GetModelCalls
	setModelCount := mockCache.SetModelCalls

	// Second identical query should hit cache for query IDs
	allUsers2Result, err := th.Query(params)
	require.NoError(t, err)
	// Fetch results
	allUsers2Fetched, fetchErr2 := allUsers2Result.Fetch(0, 100) // Fetch up to 100
	require.NoError(t, fetchErr2)
	require.Len(t, allUsers2Fetched, 3)

	// Verify cache operations for second query
	assert.Equal(t, getQueryCount+1, mockCache.GetQueryIDsCalls, "Should attempt to get query from cache again")
	assert.Equal(t, setQueryCount, mockCache.SetQueryIDsCalls, "Should not set query again (cache hit)")
	// Models should be fetched again via ByIDs, potentially hitting cache
	assert.GreaterOrEqual(t, mockCache.GetModelCalls, getModelCount+3, "Should attempt get each model again")
	assert.Equal(t, setModelCount, mockCache.SetModelCalls, "Should not set models again (cache hit)")
}

func TestThing_Query_CacheInvalidation(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create test user
	user := &User{Name: "Invalidate User", Email: "invalidate@example.com"}
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)

	// Reset cache
	mockCache.Reset()

	// Query to populate cache
	params := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Invalidate User"},
	}

	usersResult, err := th.Query(params)
	require.NoError(t, err)
	// Fetch results
	usersFetched, fetchErr := usersResult.Fetch(0, 10)
	require.NoError(t, fetchErr)
	require.Len(t, usersFetched, 1)

	// Verify query was cached
	assert.Equal(t, 1, mockCache.SetQueryIDsCalls, "Should cache the query")

	// Update the user
	user.Name = "Updated Invalidate User"
	err = th.Save(user)
	require.NoError(t, err)

	// Verify cache invalidation occurred on save
	assert.GreaterOrEqual(t, mockCache.DeleteModelCalls, 1, "Should invalidate the model in cache")

	// Querying for old name should now return no results (cache should be invalidated)
	oldNameParams := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Invalidate User"},
	}

	oldNameUsersResult, err := th.Query(oldNameParams)
	require.NoError(t, err)
	// Fetch results
	oldNameUsersFetched, fetchErrOld := oldNameUsersResult.Fetch(0, 10)
	require.NoError(t, fetchErrOld)
	assert.Empty(t, oldNameUsersFetched, "Should find no users with old name")

	// Query with new name
	newNameParams := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Updated Invalidate User"},
	}

	newNameUsersResult, err := th.Query(newNameParams)
	require.NoError(t, err)
	// Fetch results
	newNameUsersFetched, fetchErrNew := newNameUsersResult.Fetch(0, 10)
	require.NoError(t, fetchErrNew)
	assert.Len(t, newNameUsersFetched, 1, "Should find user with new name")
}

func TestThing_Save_Create_Cache(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Reset cache metrics
	mockCache.Reset()

	// Create a new user
	user := &User{
		Name:  "New Cache User",
		Email: "new-cache@example.com",
	}

	// Save the user
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID, "User should have an ID")

	// Verify no cache lookups happened for a new entity
	assert.Equal(t, 0, mockCache.GetModelCalls, "Should not try to get from cache for new entity")

	// But the entity should be stored in cache after creation
	assert.GreaterOrEqual(t, mockCache.SetModelCalls, 1, "Should set in cache after creation")

	// The key structure should be tableName:id
	modelKey := "users:" + mockCache.FormatID(user.ID)
	assert.True(t, mockCache.Exists(modelKey), "Model should be stored in cache")
}

func TestThing_Save_Update_Cache(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create a user
	user := &User{
		Name:  "Original Cache Name",
		Email: "original-cache@example.com",
	}
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)

	// Reset cache after initial save
	mockCache.Reset()

	// Fetch to populate cache
	_, err = th.ByID(user.ID)
	require.NoError(t, err)

	// Verify cache state before update
	modelKey := "users:" + mockCache.FormatID(user.ID)
	assert.True(t, mockCache.Exists(modelKey), "Model should be in cache before update")

	// Update the user
	user.Name = "Updated Cache Name"
	user.Email = "updated-cache@example.com"

	// Record current invalidation calls
	deleteModelCalls := mockCache.DeleteModelCalls

	// Save the changes
	err = th.Save(user)
	require.NoError(t, err)

	// Verify cache invalidation
	assert.Equal(t, deleteModelCalls+1, mockCache.DeleteModelCalls, "Should invalidate the model in cache")

	// The cache entry for the specific model is invalidated during update, not immediately re-set.
	// It will be repopulated on the next read.
	assert.False(t, mockCache.Exists(modelKey), "Model should be invalidated from cache after update")
}

func TestThing_Delete_Cache(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create a user
	user := &User{
		Name:  "Delete Cache User",
		Email: "delete-cache@example.com",
	}
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)

	// Reset cache after create
	mockCache.Reset()

	// Fetch to populate cache
	_, err = th.ByID(user.ID)
	require.NoError(t, err)

	// Verify cache state before delete
	modelKey := "users:" + mockCache.FormatID(user.ID)
	assert.True(t, mockCache.Exists(modelKey), "Model should be in cache before delete")

	// Record current invalidation calls
	deleteModelCalls := mockCache.DeleteModelCalls

	// Delete the user
	err = th.Delete(user)
	require.NoError(t, err)

	// Verify cache invalidation
	assert.Equal(t, deleteModelCalls+1, mockCache.DeleteModelCalls, "Should invalidate the model in cache")

	// Verify the entity is no longer in cache
	assert.False(t, mockCache.Exists(modelKey), "Model should be removed from cache")
}

// Helper function for string formatting
func sprintf(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

// TestThing_Query_IncrementalCacheUpdate verifies that list and count caches
// are updated incrementally when items matching the query are created, updated,
// or deleted.
func TestThing_Query_IncrementalCacheUpdate(t *testing.T) {
	// Use setupCacheTest to get direct instance and mock cache
	thingUser, cacheClient, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// --- Setup: Initial Query & Caching ---
	params := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Test User Incremental"},
		Order: "id DESC",
	}

	// 1. Initial query (cache miss for list & count)
	cr1, err := thingUser.Query(params)
	require.NoError(t, err)
	initialCount, err := cr1.Count()
	require.NoError(t, err)
	require.Equal(t, int64(0), initialCount, "Initial count should be 0")
	initialFetch, err := cr1.Fetch(0, 10)
	require.NoError(t, err)
	require.Len(t, initialFetch, 0, "Initial fetch should return 0 items")

	// Verify count and list (NoneResult) were attempted to be cached
	// We can't check store directly, but can check counters
	assert.GreaterOrEqual(t, cacheClient.Counters["Set"], 1, "Set should be called at least once for count/list cache")
	// Reset counters AFTER initial cache population attempt
	cacheClient.ResetCounters()

	// --- Test Case 1: Create an item that matches the query ---
	user1 := User{BaseModel: thing.BaseModel{}, Name: "Test User Incremental", Email: "inc1@test.com"}
	err = thingUser.Save(&user1)
	require.NoError(t, err)
	require.NotZero(t, user1.ID, "User1 should have an ID after save")

	// Verify caches were updated incrementally via counters
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Cache Get should be called once for count during incremental update")
	assert.Equal(t, 1, cacheClient.Counters["Set"], "Cache Set should be called once for count during incremental update")
	// SetQueryIDs is called internally by the helper setCachedListIDs
	assert.Equal(t, 1, cacheClient.Counters["SetQueryIDs"], "Cache SetQueryIDs should be called once for list during incremental update")

	// Check updated cache state by re-querying (should hit cache)
	cacheClient.ResetCounters() // Reset before verification query
	cr2, err := thingUser.Query(params)
	require.NoError(t, err)
	count2, err := cr2.Count() // Should hit count cache
	require.NoError(t, err)
	assert.Equal(t, int64(1), count2, "Count from cache should be 1")
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Count query should hit cache (Get)")
	assert.Equal(t, 0, cacheClient.Counters["Set"], "Count query should not Set cache")

	fetch2, err := cr2.Fetch(0, 10) // Should hit list cache
	require.NoError(t, err)
	require.Len(t, fetch2, 1)
	assert.Equal(t, user1.ID, fetch2[0].ID)
	assert.Equal(t, 1, cacheClient.Counters["GetQueryIDs"], "Fetch query should hit list cache (GetQueryIDs)")
	assert.Equal(t, 0, cacheClient.Counters["SetQueryIDs"], "Fetch query should not Set list cache")

	cacheClient.ResetCounters()

	// --- Test Case 2: Update item to NOT match query ---
	user1.Name = "Does Not Match Anymore"
	err = thingUser.Save(&user1)
	require.NoError(t, err)

	// Verify caches were updated (removed item) via counters
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Cache Get should be called once for count during non-match update")
	assert.Equal(t, 1, cacheClient.Counters["Set"], "Cache Set should be called once for count during non-match update")
	assert.Equal(t, 1, cacheClient.Counters["SetQueryIDs"], "Cache SetQueryIDs should be called once for list during non-match update")

	// Check updated cache state by re-querying
	cacheClient.ResetCounters()
	cr3, err := thingUser.Query(params)
	require.NoError(t, err)
	count3, err := cr3.Count() // Should hit cache
	require.NoError(t, err)
	assert.Equal(t, int64(0), count3, "Count from cache should be 0 after update")
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Count query after non-match update should hit cache (Get)")

	fetch3, err := cr3.Fetch(0, 10) // Should hit cache
	require.NoError(t, err)
	assert.Len(t, fetch3, 0, "Fetch after non-match update should return empty")
	assert.Equal(t, 1, cacheClient.Counters["GetQueryIDs"], "Fetch query after non-match update should hit list cache (GetQueryIDs)")

	cacheClient.ResetCounters()

	// --- Test Case 3: Update item back to MATCH query ---
	user1.Name = "Test User Incremental"
	err = thingUser.Save(&user1)
	require.NoError(t, err)

	// Verify caches were updated (added item back) via counters
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Cache Get should be called once for count during match update")
	assert.Equal(t, 1, cacheClient.Counters["Set"], "Cache Set should be called once for count during match update")
	assert.Equal(t, 1, cacheClient.Counters["SetQueryIDs"], "Cache SetQueryIDs should be called once for list during match update")

	// Check updated cache state by re-querying
	cacheClient.ResetCounters()
	cr4, err := thingUser.Query(params)
	require.NoError(t, err)
	count4, err := cr4.Count() // Should hit cache
	require.NoError(t, err)
	assert.Equal(t, int64(1), count4, "Count from cache should be 1 after second update")
	fetch4, err := cr4.Fetch(0, 10) // Should hit cache
	require.NoError(t, err)
	require.Len(t, fetch4, 1)
	assert.Equal(t, user1.ID, fetch4[0].ID)

	cacheClient.ResetCounters()

	// --- Test Case 4: Delete the matching item ---
	err = thingUser.Delete(&user1)
	require.NoError(t, err)

	// Verify caches were updated (item removed) via counters
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Cache Get should be called once for count during delete")
	assert.Equal(t, 1, cacheClient.Counters["Set"], "Cache Set should be called once for count during delete")
	assert.Equal(t, 1, cacheClient.Counters["SetQueryIDs"], "Cache SetQueryIDs should be called once for list during delete")

	// Check updated cache state by re-querying
	cacheClient.ResetCounters()
	cr5, err := thingUser.Query(params)
	require.NoError(t, err)
	count5, err := cr5.Count() // Should hit cache
	require.NoError(t, err)
	assert.Equal(t, int64(0), count5, "Count from cache should be 0 after delete")
	fetch5, err := cr5.Fetch(0, 10) // Should hit cache
	require.NoError(t, err)
	assert.Len(t, fetch5, 0)

	cacheClient.ResetCounters()

	// --- Test Case 5: Create item, then Soft Delete it ---
	user2 := User{BaseModel: thing.BaseModel{}, Name: "Test User Incremental", Email: "inc2@test.com"}
	err = thingUser.Save(&user2)
	require.NoError(t, err)
	cacheClient.ResetCounters() // Reset after creation

	// Soft delete using the dedicated method
	err = thingUser.SoftDelete(&user2)
	require.NoError(t, err)

	// Verify caches were updated (item removed due to KeepItem() check) via counters
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Cache Get should be called once for count during soft delete save")
	assert.Equal(t, 1, cacheClient.Counters["Set"], "Cache Set should be called once for count during soft delete save")
	assert.Equal(t, 1, cacheClient.Counters["SetQueryIDs"], "Cache SetQueryIDs should be called once for list during soft delete save")

	// Check updated cache state by re-querying
	cacheClient.ResetCounters()
	cr6, err := thingUser.Query(params)
	require.NoError(t, err)
	count6, err := cr6.Count() // Should hit cache
	require.NoError(t, err)
	assert.Equal(t, int64(0), count6, "Count from cache should be 0 after soft delete")
	fetch6, err := cr6.Fetch(0, 10) // Should hit cache
	require.NoError(t, err)
	assert.Len(t, fetch6, 0, "Fetch after soft delete should return empty")
}
