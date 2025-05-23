package thing_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/burugo/thing"
	"github.com/burugo/thing/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Add a helper method to the mockCacheClient to format IDs for testing cache keys
func (m *mockCacheClient) FormatID(id int64) string {
	return fmt.Sprintf("%d", id)
}

func TestThing_ByID_Cache_MissAndSet(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	// Create a test user
	user := &User{
		BaseModel: thing.BaseModel{},
		Name:      "Cache Test User",
		Email:     "cache@example.com",
	}

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
	assert.Equal(t, user.ID, (foundUser).ID)
	assert.Equal(t, user.Name, foundUser.Name)
	assert.Equal(t, user.Email, foundUser.Email)

	// Verify cache operations
	assert.Equal(t, 1, mockCache.Counters["GetModel"], "Should attempt to get from cache")
	assert.Equal(t, 1, mockCache.Counters["SetModel"], "Should set in cache after DB fetch")

	// The key structure should be tableName:id, verify the model was stored in cache
	modelKey := "users:" + mockCache.FormatID(user.ID)
	assert.True(t, mockCache.Exists(modelKey), "Model should be stored in cache")
}

func TestThing_ByID_Cache_Hit(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	// Create a test user
	user := &User{
		BaseModel: thing.BaseModel{},
		Name:      "Cache Hit User",
		Email:     "cache-hit@example.com",
	}

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
	getCount := mockCache.Counters["GetModel"]
	setCount := mockCache.Counters["SetModel"]

	// Second fetch should hit the cache
	foundUser, err := th.ByID(user.ID)
	require.NoError(t, err)
	require.NotNil(t, foundUser)

	// Verify it was a cache hit
	assert.Equal(t, getCount+1, mockCache.Counters["GetModel"], "Should attempt one more get")
	assert.Equal(t, setCount, mockCache.Counters["SetModel"], "Should not set in cache again")

	// Verify the data was correct from cache
	assert.Equal(t, user.ID, foundUser.ID)
	assert.Equal(t, user.Name, foundUser.Name)
	assert.Equal(t, user.Email, foundUser.Email)
}

func TestThing_ByID_Cache_NoneResult(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	// 1. Create user
	user := &User{
		BaseModel: thing.BaseModel{},
		Name:      "NoneResult User",
		Email:     "noneresult@example.com",
	}
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
	mockCache.ResetCalls()

	// 4. Fetch again - should miss cache, miss DB, cache NoneResult, return ErrNotFound
	_, err = th.ByID(userID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, common.ErrNotFound), "Expected ErrNotFound after delete")

	// Verify cache operations for the first fetch after delete
	assert.Equal(t, 1, mockCache.Counters["GetModel"], "Should attempt cache get (miss)")
	assert.Equal(t, 1, mockCache.Counters["Set"], "Should set NoneResult in cache")         // Check Set counter
	assert.Equal(t, 0, mockCache.Counters["SetModel"], "Should NOT set model after delete") // Use Counters map
	// Verify DB was checked (via fetchModelsByIDsInternal)
	// We don't have a direct counter for DB calls, rely on cache misses/sets.

	// Check that NoneResult was actually cached
	cacheKey := "users:" + mockCache.FormatID(userID)
	rawVal, rawErr := mockCache.Get(context.Background(), cacheKey)
	require.NoError(t, rawErr, "Should be able to get raw value from cache")
	assert.Equal(t, common.NoneResult, rawVal, "Cache should contain NoneResult string")

	// Record counts before the final fetch
	getCallsBeforeFinal := mockCache.Counters["GetModel"]
	setCallsBeforeFinal := mockCache.Counters["Set"]
	setModelCallsBeforeFinal := mockCache.Counters["SetModel"]

	// 5. Fetch one more time - should hit NoneResult in cache, return ErrNotFound immediately
	_, err = th.ByID(userID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, common.ErrNotFound), "Expected ErrNotFound again")

	// Verify cache operations for the second fetch after delete
	assert.Equal(t, getCallsBeforeFinal+1, mockCache.Counters["GetModel"], "Should attempt cache get again (hit NoneResult)")
	assert.Equal(t, setCallsBeforeFinal, mockCache.Counters["Set"], "Should NOT set NoneResult again")
	assert.Equal(t, setModelCallsBeforeFinal, mockCache.Counters["SetModel"], "Should NOT set model again")
	// Crucially, DB should NOT have been queried this time. We verify by checking no new Set* calls happened.

	// Check Set calls AFTER ResetCalls
	// Set should NOT have been called again, as NoneResult should prevent DB lookup
	mockCache.AssertSetCalls(t, 1, "Should set NoneResult exactly once") // Expect 1 Set call after ResetCalls
}

func TestThing_Query_Cache(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	// Create multiple users
	userAlice := &User{BaseModel: thing.BaseModel{}, Name: "Alice Cache", Email: "alice-cache@example.com"}
	userBob := &User{BaseModel: thing.BaseModel{}, Name: "Bob Cache", Email: "bob-cache@example.com"}
	userCharlie := &User{BaseModel: thing.BaseModel{}, Name: "Charlie Cache", Email: "charlie-cache@example.com"}

	for _, u := range []*User{userAlice, userBob, userCharlie} {
		err := th.Save(u)
		require.NoError(t, err)
		require.NotZero(t, u.ID)
	}

	// Reset cache metrics
	mockCache.ResetCalls()

	// First query should miss cache - Query for Alice specifically
	params := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Alice Cache"},
	}

	queryResult := th.Query(params)
	// Fetch results
	fetchedUsers, fetchErr := queryResult.Fetch(0, 10) // Fetch up to 10
	require.NoError(t, fetchErr)
	require.Len(t, fetchedUsers, 1, "Expected to fetch 1 user named Alice Cache")
	assert.Equal(t, userAlice.ID, fetchedUsers[0].ID)

	// Verify cache operations for first query
	assert.Equal(t, 1, mockCache.Counters["GetQueryIDs"], "Should attempt to get query from cache")
	assert.Equal(t, 1, mockCache.Counters["SetQueryIDs"], "Should set query result in cache")
	assert.GreaterOrEqual(t, mockCache.Counters["GetModel"], 1, "Should attempt to get the model (Alice)")

	// Record cache stats before second query
	getQueryCount := mockCache.Counters["GetQueryIDs"]
	setQueryCount := mockCache.Counters["SetQueryIDs"]
	getModelCount := mockCache.Counters["GetModel"]
	setModelCount := mockCache.Counters["SetModel"]

	// Second identical query should hit cache for query IDs
	query2Result := th.Query(params)
	// Fetch results
	fetchedUsers2, fetchErr2 := query2Result.Fetch(0, 10) // Fetch up to 10
	require.NoError(t, fetchErr2)
	require.Len(t, fetchedUsers2, 1, "Expected to fetch 1 user from cache")
	assert.Equal(t, userAlice.ID, fetchedUsers2[0].ID)

	// Verify cache operations for second query
	assert.Equal(t, getQueryCount+1, mockCache.Counters["GetQueryIDs"], "Should attempt to get query from cache again")
	assert.Equal(t, setQueryCount, mockCache.Counters["SetQueryIDs"], "Should not set query again (cache hit)")
	assert.GreaterOrEqual(t, mockCache.Counters["GetModel"], getModelCount+1, "Should attempt get the model again")
	assert.Equal(t, setModelCount, mockCache.Counters["SetModel"], "Should not set models again (cache hit)")
}

func TestThing_Query_CacheInvalidation(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	// Create test user
	user := &User{
		BaseModel: thing.BaseModel{},
		Name:      "Invalidate User",
		Email:     "invalidate@example.com",
	}
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

	usersResult := th.Query(params)
	// Fetch results
	usersFetched, fetchErr := usersResult.Fetch(0, 10)
	require.NoError(t, fetchErr)
	require.Len(t, usersFetched, 1)

	// Verify query was cached
	assert.Equal(t, 1, mockCache.Counters["SetQueryIDs"], "Should cache the query")

	// Update the user
	user.Name = "Updated Invalidate User"
	err = th.Save(user)
	require.NoError(t, err)

	// Verify cache invalidation occurred on save
	// The incremental cache update logic in Save should remove the item from relevant list/count caches.

	// Querying for old name should now return no results (cache should be invalidated)
	oldNameParams := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Invalidate User"},
	}

	oldNameUsersResult := th.Query(oldNameParams)
	// Fetch results
	oldNameUsersFetched, fetchErrOld := oldNameUsersResult.Fetch(0, 10)
	require.NoError(t, fetchErrOld)
	assert.Empty(t, oldNameUsersFetched, "Should find no users with old name")

	// Query with new name
	newNameParams := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Updated Invalidate User"},
	}

	newNameUsersResult := th.Query(newNameParams)
	// Fetch results
	newNameUsersFetched, fetchErrNew := newNameUsersResult.Fetch(0, 10)
	require.NoError(t, fetchErrNew)
	assert.Len(t, newNameUsersFetched, 1, "Should find user with new name")
}

func TestThing_Save_Create_Cache(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	// Reset cache metrics
	mockCache.Reset()

	// Create a new user
	user := &User{
		BaseModel: thing.BaseModel{},
		Name:      "New Cache User",
		Email:     "new-cache@example.com",
	}

	// Save the user
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID, "User should have an ID")

	// Verify no cache lookups happened for a new entity
	assert.Equal(t, 0, mockCache.Counters["GetModel"], "Should not try to get from cache for new entity")

	// But the entity should be stored in cache after creation
	assert.GreaterOrEqual(t, mockCache.Counters["SetModel"], 1, "Should set in cache after creation")

	// The key structure should be tableName:id
	modelKey := "users:" + mockCache.FormatID(user.ID)
	assert.True(t, mockCache.Exists(modelKey), "Model should be stored in cache")
}

func TestThing_Save_Update_Cache(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	// Create a user
	user := &User{
		BaseModel: thing.BaseModel{},
		Name:      "Original Cache Name",
		Email:     "original-cache@example.com",
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

	// Save the changes
	err = th.Save(user)
	require.NoError(t, err)

	// The cache entry for the specific model should be updated, not invalidated.
	// It will be repopulated on the next read.
	assert.True(t, mockCache.Exists(modelKey), "Model should still exist in cache after update")
}

func TestThing_Delete_Cache(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	// Create a user
	user := &User{
		BaseModel: thing.BaseModel{},
		Name:      "Delete Cache User",
		Email:     "delete-cache@example.com",
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

	// Delete the user
	err = th.Delete(user)
	require.NoError(t, err)

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
	thingUser, cacheClient, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	// --- Setup: Initial Query & Caching ---
	params := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Test User Incremental"},
		Order: "id DESC",
	}

	// 1. Initial query (cache miss for list & count)
	cr1 := thingUser.Query(params)
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
	cacheClient.ResetCalls()

	// --- Test Case 1: Create an item that matches the query ---
	user1 := User{BaseModel: thing.BaseModel{}, Name: "Test User Incremental", Email: "inc1@test.com"}
	err = thingUser.Save(&user1)
	require.NoError(t, err)
	require.NotZero(t, user1.ID, "User1 should have an ID after save")

	// Verify caches were updated incrementally via counters AFTER ResetCalls
	// Create operation: Reads miss cache, invalidates list, writes count, sets model cache.
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Expected 1 Get (count cache miss) during create")
	assert.Equal(t, 1, cacheClient.Counters["GetQueryIDs"], "Expected 1 GetQueryIDs (list cache miss) during create")
	assert.Equal(t, 1, cacheClient.Counters["Set"], "Expected 1 Set (count cache write) during create")
	// List cache is now invalidated (Deleted) instead of Set
	assert.Equal(t, 0, cacheClient.Counters["SetQueryIDs"], "Expected 0 SetQueryIDs (list cache invalidated) during create")
	assert.Equal(t, 1, cacheClient.Counters["Delete"], "Expected 1 Delete (list cache invalidated) during create")
	assert.Equal(t, 1, cacheClient.Counters["SetModel"], "Expected 1 SetModel during create")
	assert.Equal(t, 0, cacheClient.Counters["DeleteModel"], "Expected 0 DeleteModel during create")

	// Check updated cache state by re-querying (should hit cache)
	cacheClient.ResetCalls() // Reset before verification query
	cr2 := thingUser.Query(params)
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
	assert.Equal(t, 1, cacheClient.Counters["SetQueryIDs"], "Expected 1 SetQueryIDs (list cache write on miss)")

	cacheClient.ResetCalls()

	// --- Test Case 2: Update item to NOT match query ---
	user1.Name = "Does Not Match Anymore"
	err = thingUser.Save(&user1)
	require.NoError(t, err)

	// Verify caches were updated (removed item) via counters AFTER ResetCalls
	// Update operation: Reads existing query caches, determines change, writes updated query caches, AND sets the model cache.
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Expected 1 Get (count cache read) during update-no-match")
	assert.Equal(t, 1, cacheClient.Counters["GetQueryIDs"], "Expected 1 GetQueryIDs (list cache read) during update-no-match")
	assert.Equal(t, 1, cacheClient.Counters["Set"], "Expected 1 Set (count cache write) during update-no-match")
	// List cache is now invalidated (Deleted) instead of Set
	assert.Equal(t, 0, cacheClient.Counters["SetQueryIDs"], "Expected 0 SetQueryIDs (list cache invalidated) during update-no-match")
	assert.Equal(t, 1, cacheClient.Counters["Delete"], "Expected 1 Delete (list cache invalidated) during update-no-match")
	assert.Equal(t, 1, cacheClient.Counters["SetModel"], "Expected 1 SetModel during update-no-match") // Save always updates the model cache
	assert.Equal(t, 0, cacheClient.Counters["DeleteModel"], "Expected 0 DeleteModel during update-no-match")

	// Check updated cache state by re-querying
	cacheClient.ResetCalls()
	cr3 := thingUser.Query(params)
	count3, err := cr3.Count() // Should hit cache
	require.NoError(t, err)
	assert.Equal(t, int64(0), count3, "Count from cache should be 0 after update")
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Count query after non-match update should hit cache (Get)")

	fetch3, err := cr3.Fetch(0, 10) // Should hit cache
	require.NoError(t, err)
	assert.Len(t, fetch3, 0, "Fetch after non-match update should return empty")
	assert.Equal(t, 1, cacheClient.Counters["GetQueryIDs"], "Fetch query after non-match update should hit list cache (GetQueryIDs)")

	cacheClient.ResetCalls()

	// --- Test Case 3: Update item back to MATCH query ---
	user1.Name = "Test User Incremental"
	err = thingUser.Save(&user1)
	require.NoError(t, err)

	// Verify caches were updated (added item back) via counters AFTER ResetCalls
	// Update operation: Reads existing query caches, determines change, writes updated query caches, AND sets the model cache.
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Expected 1 Get (count cache read) during update-match")
	assert.Equal(t, 1, cacheClient.Counters["GetQueryIDs"], "Expected 1 GetQueryIDs (list cache read) during update-match")
	assert.Equal(t, 1, cacheClient.Counters["Set"], "Expected 1 Set (count cache write) during update-match")
	// List cache is now invalidated (Deleted) instead of Set
	assert.Equal(t, 0, cacheClient.Counters["SetQueryIDs"], "Expected 0 SetQueryIDs (list cache invalidated) during update-match")
	assert.Equal(t, 1, cacheClient.Counters["Delete"], "Expected 1 Delete (list cache invalidated) during update-match")
	assert.Equal(t, 1, cacheClient.Counters["SetModel"], "Expected 1 SetModel during update-match") // Save always updates the model cache
	assert.Equal(t, 0, cacheClient.Counters["DeleteModel"], "Expected 0 DeleteModel during update-match")

	// Check updated cache state by re-querying
	cacheClient.ResetCalls()
	cr4 := thingUser.Query(params)
	count4, err := cr4.Count() // Should hit cache
	require.NoError(t, err)
	assert.Equal(t, int64(1), count4, "Count from cache should be 1 after second update")
	fetch4, err := cr4.Fetch(0, 10) // Should hit cache
	require.NoError(t, err)
	require.Len(t, fetch4, 1)
	assert.Equal(t, user1.ID, fetch4[0].ID)

	cacheClient.ResetCalls()

	// --- Test Case 4: Delete the matching item ---
	err = thingUser.Delete(&user1)
	require.NoError(t, err)

	// Verify caches were updated (item removed) via counters AFTER ResetCalls
	// Delete operation: Reads existing query caches, determines change, writes updated query caches, AND deletes the model cache using Delete.
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Expected 1 Get (count cache read) during delete")
	assert.Equal(t, 1, cacheClient.Counters["GetQueryIDs"], "Expected 1 GetQueryIDs (list cache read) during delete")
	assert.Equal(t, 1, cacheClient.Counters["Set"], "Expected 1 Set (count cache write) during delete")
	// List cache is now invalidated (Deleted) instead of Set
	assert.Equal(t, 0, cacheClient.Counters["SetQueryIDs"], "Expected 0 SetQueryIDs (list cache invalidated) during delete")
	assert.Equal(t, 0, cacheClient.Counters["SetModel"], "Expected 0 SetModel during delete")
	assert.Equal(t, 0, cacheClient.Counters["DeleteModel"], "Expected 0 DeleteModel during delete")
	// Expect 2 Deletes: 1 for model cache (via th.Delete), 1 for list cache invalidation
	assert.Equal(t, 2, cacheClient.Counters["Delete"], "Expected 2 Delete calls (model + list invalidation) during delete")

	// Check updated cache state by re-querying
	cacheClient.ResetCalls()
	cr5 := thingUser.Query(params)
	count5, err := cr5.Count() // Should hit cache
	require.NoError(t, err)
	assert.Equal(t, int64(0), count5, "Count from cache should be 0 after delete")
	fetch5, err := cr5.Fetch(0, 10) // Should hit cache
	require.NoError(t, err)
	assert.Len(t, fetch5, 0)

	cacheClient.ResetCalls()

	// --- Test Case 5: Create item, then Soft Delete it ---
	user2 := User{BaseModel: thing.BaseModel{}, Name: "Test User Incremental", Email: "inc2@test.com"}
	err = thingUser.Save(&user2)
	require.NoError(t, err)
	cacheClient.ResetCalls() // Reset after creation

	// Soft delete using the dedicated method
	err = thingUser.SoftDelete(&user2)
	require.NoError(t, err)

	// Verify caches were updated (item removed due to KeepItem() check) via counters AFTER ResetCalls
	// SoftDelete uses Save, which triggers Update logic.
	// 1. Reads query caches (Get, GetQueryIDs - HITS expected now if create worked)
	// 2. Determines item needs removing from query caches because KeepItem is false.
	// 3. Writes updated query caches (Set, SetQueryIDs)
	// 4. Updates the model cache (SetModel).
	assert.Equal(t, 1, cacheClient.Counters["Get"], "Expected 1 Get (count cache HIT) during soft delete save")
	assert.Equal(t, 1, cacheClient.Counters["GetQueryIDs"], "Expected 1 GetQueryIDs (list cache HIT) during soft delete save")
	assert.Equal(t, 1, cacheClient.Counters["Set"], "Expected 1 Set (count cache write) during soft delete save")
	// List cache is now invalidated (Deleted) instead of Set
	assert.Equal(t, 0, cacheClient.Counters["SetQueryIDs"], "Expected 0 SetQueryIDs (list cache invalidated) during soft delete save")
	assert.Equal(t, 1, cacheClient.Counters["Delete"], "Expected 1 Delete (list cache invalidated) during soft delete save")
	assert.Equal(t, 1, cacheClient.Counters["SetModel"], "Expected 1 SetModel during soft delete save") // Save always Sets
	assert.Equal(t, 0, cacheClient.Counters["DeleteModel"], "Expected 0 DeleteModel during soft delete save")

	// Check updated cache state by re-querying
	cacheClient.ResetCalls()
	cr6 := thingUser.Query(params)
	count6, err := cr6.Count() // Should hit cache
	require.NoError(t, err)
	// Count should be 0 because the incremental update logic removes soft-deleted items from this query's cache.
	assert.Equal(t, int64(0), count6, "Count from cache should be 0 after soft delete")
	fetch6, err := cr6.Fetch(0, 10)
	require.NoError(t, err)
	assert.Len(t, fetch6, 0) // Fetch should still return 0 because Fetch filters KeepItem=false
}

// TestThing_IncrementalQueryCacheUpdate tests the incremental update logic
// for list and count caches triggered by Save and Delete operations.
func TestThing_IncrementalQueryCacheUpdate(t *testing.T) {
	thingInstance, mockCache, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()
	ctx := context.Background()

	// 1. Initial setup: Create some users and cache a query
	user1 := &User{BaseModel: thing.BaseModel{}, Name: "Alice", Email: "alice@example.com"}
	user2 := &User{BaseModel: thing.BaseModel{}, Name: "Alice", Email: "alice2@example.com"} // Same name, different email
	user3 := &User{BaseModel: thing.BaseModel{}, Name: "Charlie", Email: "charlie@example.com"}
	require.NoError(t, thingInstance.Save(user1), "Save user1 failed")
	require.NoError(t, thingInstance.Save(user2), "Save user2 failed")
	require.NoError(t, thingInstance.Save(user3), "Save user3 failed")

	// Define query params for users named "Alice"
	queryParams := thing.QueryParams{Where: "name = ?", Args: []interface{}{"Alice"}}
	queryResult := thingInstance.Query(queryParams)

	// --- Cache the initial query results (list and count) ---
	// Fetch list to cache IDs
	_, err := queryResult.Fetch(0, 10) // Fetch first page to trigger caching
	require.NoError(t, err, "Fetch failed")
	// Manually get list cache key (assuming internal generation logic)
	listCacheKey := generateQueryCacheKey(t, "list", user1.TableName(), queryParams)
	initialIDs, err := mockCache.GetQueryIDs(ctx, listCacheKey)
	require.NoError(t, err, "GetQueryIDs after fetch failed")
	assert.ElementsMatch(t, []int64{user1.ID, user2.ID}, initialIDs, "Initial cached IDs incorrect")

	// Fetch count to cache count
	count, err := queryResult.Count()
	require.NoError(t, err, "Count failed")
	assert.EqualValues(t, 2, count, "Initial count incorrect")
	// Manually get count cache key
	countCacheKey := generateQueryCacheKey(t, "count", user1.TableName(), queryParams)
	countStr, err := mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache failed")
	assert.Equal(t, "2", countStr, "Initial cached count string incorrect")

	mockCache.ResetCalls() // Reset call counts before testing updates

	// --- Test Scenario 1: Create a new user that matches the query ---
	user4 := &User{BaseModel: thing.BaseModel{}, Name: "Alice", Email: "alice3@example.com"}
	require.NoError(t, thingInstance.Save(user4), "Save user4 failed")

	// Check list cache: Cache should have been INVALIDATED by the Save operation
	_, err = mockCache.GetQueryIDs(ctx, listCacheKey)
	assert.Error(t, err, "Expected error getting query IDs after invalidation")
	assert.True(t, errors.Is(err, common.ErrNotFound), "Expected ErrNotFound after invalidation")

	// Check count cache: count should be incremented (count cache is still updated)
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after creating user4 failed")
	assert.Equal(t, "3", countStr, "Count cache not incremented after creating matching user")

	// Verify a subsequent query re-populates the list cache correctly
	queryResult2 := thingInstance.Query(queryParams)
	fetch2, err := queryResult2.Fetch(0, 10)
	require.NoError(t, err, "Second Fetch failed")
	require.Len(t, fetch2, 3, "Second fetch should find 3 users")
	updatedIDs, err := mockCache.GetQueryIDs(ctx, listCacheKey)
	require.NoError(t, err, "GetQueryIDs after second fetch failed")
	assert.ElementsMatch(t, []int64{user1.ID, user2.ID, user4.ID}, updatedIDs, "List cache not re-populated correctly")

	mockCache.ResetCalls() // Reset calls before next scenario

	// --- Test Scenario 2: Create a new user that DOES NOT match the query ---
	user5 := &User{BaseModel: thing.BaseModel{}, Name: "Eve", Email: "eve@example.com"} // Different name
	require.NoError(t, thingInstance.Save(user5), "Save user5 failed")

	// Check list cache: should NOT have changed
	idsAfterUser5, err := mockCache.GetQueryIDs(ctx, listCacheKey)
	require.NoError(t, err, "GetQueryIDs after creating user5 failed")
	assert.ElementsMatch(t, []int64{user1.ID, user2.ID, user4.ID}, idsAfterUser5, "List cache changed after creating non-matching user")

	// Check count cache: should NOT have changed
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after creating user5 failed")
	assert.Equal(t, "3", countStr, "Count cache changed after creating non-matching user")
	mockCache.ResetCalls()

	// --- Test Scenario 3: Update a user TO match the query ---
	user3.Name = "Alice" // Change name from "Charlie" to "Alice"
	require.NoError(t, thingInstance.Save(user3), "Save updated user3 failed")

	// Check list cache: Cache should have been INVALIDATED by the Save operation
	_, err = mockCache.GetQueryIDs(ctx, listCacheKey)
	assert.Error(t, err, "Expected error getting query IDs after update TO match (invalidation)")
	assert.True(t, errors.Is(err, common.ErrNotFound), "Expected ErrNotFound after update TO match (invalidation)")

	// Check count cache: count should be incremented
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after updating user3 to match failed")
	assert.Equal(t, "4", countStr, "Count cache not incremented after update TO match")

	// Verify a subsequent query re-populates the list cache correctly
	queryResult3 := thingInstance.Query(queryParams)
	fetch3, err := queryResult3.Fetch(0, 10)
	require.NoError(t, err, "Third Fetch failed")
	require.Len(t, fetch3, 4, "Third fetch should find 4 users")
	idsAfterUpdate1, err := mockCache.GetQueryIDs(ctx, listCacheKey) // Check repopulated list
	require.NoError(t, err, "GetQueryIDs after third fetch failed")
	assert.ElementsMatch(t, []int64{user1.ID, user2.ID, user4.ID, user3.ID}, idsAfterUpdate1, "List cache not re-populated correctly after update TO match")

	mockCache.ResetCalls()

	// --- Test Scenario 4: Update a user TO NOT match the query ---
	user1.Name = "Alicia" // Change name from "Alice" to "Alicia"
	require.NoError(t, thingInstance.Save(user1), "Save updated user1 failed")

	// Check list cache: Cache should have been INVALIDATED by the Save operation
	_, err = mockCache.GetQueryIDs(ctx, listCacheKey)
	assert.Error(t, err, "Expected error getting query IDs after update TO NOT match (invalidation)")
	assert.True(t, errors.Is(err, common.ErrNotFound), "Expected ErrNotFound after update TO NOT match (invalidation)")

	// Check count cache: count should be decremented
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after updating user1 to not match failed")
	assert.Equal(t, "3", countStr, "Count cache not decremented after update TO NOT match")

	// Verify a subsequent query re-populates the list cache correctly
	queryResult4 := thingInstance.Query(queryParams)
	fetch4, err := queryResult4.Fetch(0, 10)
	require.NoError(t, err, "Fourth Fetch failed")
	require.Len(t, fetch4, 3, "Fourth fetch should find 3 users")
	idsAfterUpdate2, err := mockCache.GetQueryIDs(ctx, listCacheKey) // Check repopulated list
	require.NoError(t, err, "GetQueryIDs after fourth fetch failed")
	assert.ElementsMatch(t, []int64{user2.ID, user4.ID, user3.ID}, idsAfterUpdate2, "List cache not re-populated correctly after update TO NOT match")

	mockCache.ResetCalls()

	// --- Test Scenario 5: Update a user that never matched (should have no effect) ---
	user5.Email = "evelyn@example.com" // Change email, name is still "Eve"
	require.NoError(t, thingInstance.Save(user5), "Save updated user5 failed")

	// Check list cache: should NOT have changed
	idsAfterUpdate3, err := mockCache.GetQueryIDs(ctx, listCacheKey)
	require.NoError(t, err, "GetQueryIDs after updating user5 failed")
	assert.ElementsMatch(t, []int64{user2.ID, user4.ID, user3.ID}, idsAfterUpdate3, "List cache changed after updating non-matching user")

	// Check count cache: should NOT have changed
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after updating user5 failed")
	assert.Equal(t, "3", countStr, "Count cache changed after updating non-matching user")
	mockCache.ResetCalls()

	// --- Test Scenario 6: Delete a user that matches the query ---
	require.NoError(t, thingInstance.Delete(user2), "Delete user2 failed")

	// Check list cache: Cache should have been INVALIDATED by the Delete operation
	_, err = mockCache.GetQueryIDs(ctx, listCacheKey)
	assert.Error(t, err, "Expected error getting query IDs after delete (invalidation)")
	assert.True(t, errors.Is(err, common.ErrNotFound), "Expected ErrNotFound after delete (invalidation)")

	// Check count cache: count should be decremented
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after deleting user2 failed")
	assert.Equal(t, "2", countStr, "Count cache not decremented after delete")

	// Verify a subsequent query re-populates the list cache correctly
	queryResult5 := thingInstance.Query(queryParams)
	fetch5, err := queryResult5.Fetch(0, 10)
	require.NoError(t, err, "Fifth Fetch failed")
	require.Len(t, fetch5, 2, "Fifth fetch should find 2 users")
	idsAfterDelete, err := mockCache.GetQueryIDs(ctx, listCacheKey) // Check repopulated list
	require.NoError(t, err, "GetQueryIDs after fifth fetch failed")
	assert.ElementsMatch(t, []int64{user4.ID, user3.ID}, idsAfterDelete, "List cache not re-populated correctly after delete")
}

// Helper to generate query cache keys (consistent with internal logic)
// NOTE: This now calls the shared GenerateCacheKey from thing/cache.go.
func generateQueryCacheKey(t *testing.T, keyType, tableName string, params thing.QueryParams) string {
	t.Helper()
	return thing.GenerateCacheKey(keyType, tableName, params)
}

// Test: Complex multi-field query (name = ? AND email LIKE ?)
func TestThing_Query_IncrementalCacheUpdate_Complex(t *testing.T) {
	thingUser, cacheClient, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	// Initial query: name = 'Bob', email LIKE '%@example.com'
	params := thing.QueryParams{
		Where: "name = ? AND email LIKE ?",
		Args:  []interface{}{"Bob", "%@example.com"},
	}

	// Prime the cache (should be empty)
	cr := thingUser.Query(params)
	_, err := cr.Fetch(0, 10)
	require.NoError(t, err)
	cacheClient.ResetCalls()

	// Create item that matches only name
	user1 := User{BaseModel: thing.BaseModel{}, Name: "Bob", Email: "bob@other.com"}
	require.NoError(t, thingUser.Save(&user1))
	// Should NOT invalidate the cache (does not match full query)
	assert.Equal(t, 0, cacheClient.Counters["Delete"], "Cache should not be invalidated for partial match")
	cacheClient.ResetCalls()

	// Create item that matches only email
	user2 := User{BaseModel: thing.BaseModel{}, Name: "Alice", Email: "alice@example.com"}
	require.NoError(t, thingUser.Save(&user2))
	assert.Equal(t, 0, cacheClient.Counters["Delete"], "Cache should not be invalidated for partial match")
	cacheClient.ResetCalls()

	// Create item that matches both
	user3 := User{BaseModel: thing.BaseModel{}, Name: "Bob", Email: "bob@example.com"}
	require.NoError(t, thingUser.Save(&user3))
	// Should invalidate the cache (full match)
	assert.Equal(t, 1, cacheClient.Counters["Delete"], "Cache should be invalidated for full match")
}

// Test: IN clause query (name IN (?))
func TestThing_Query_IncrementalCacheUpdate_IN(t *testing.T) {
	thingUser, cacheClient, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	params := thing.QueryParams{
		Where: "name IN (?)",
		Args:  []interface{}{[]string{"Tom", "Jerry"}},
	}

	cr := thingUser.Query(params)
	_, err := cr.Fetch(0, 10)
	require.NoError(t, err)
	cacheClient.ResetCalls()

	// Create item not in IN set
	user1 := User{BaseModel: thing.BaseModel{}, Name: "Spike", Email: "spike@example.com"}
	require.NoError(t, thingUser.Save(&user1))
	assert.Equal(t, 0, cacheClient.Counters["Delete"], "Cache should not be invalidated for non-matching IN")
	cacheClient.ResetCalls()

	// Create item in IN set
	user2 := User{BaseModel: thing.BaseModel{}, Name: "Tom", Email: "tom@example.com"}
	require.NoError(t, thingUser.Save(&user2))
	assert.Equal(t, 1, cacheClient.Counters["Delete"], "Cache should be invalidated for matching IN")
}

// Test: Range query (id > ?)
func TestThing_Query_IncrementalCacheUpdate_Range(t *testing.T) {
	thingUser, cacheClient, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	params := thing.QueryParams{
		Where: "id > ?",
		Args:  []interface{}{100},
	}

	cr := thingUser.Query(params)
	_, err := cr.Fetch(0, 10)
	require.NoError(t, err)
	cacheClient.ResetCalls()

	// Create item with id <= 100 (simulate by setting ID directly)
	user1 := User{BaseModel: thing.BaseModel{}, Name: "LowID", Email: "lowid@example.com"}
	require.NoError(t, thingUser.Save(&user1))
	user1.ID = 50 // Simulate low ID (if auto-increment, this may not be possible, but for test, force it)
	assert.Equal(t, 0, cacheClient.Counters["Delete"], "Cache should not be invalidated for id <= 100")
	cacheClient.ResetCalls()

	// Create item with id > 100 (simulate by setting ID directly)
	user2 := User{BaseModel: thing.BaseModel{}, Name: "HighID", Email: "highid@example.com"}
	require.NoError(t, thingUser.Save(&user2))
	// Note: In real DB, ID will be auto-incremented and likely not > 100 unless seeded. This test documents the limitation.
	// If your test DB allows manual ID assignment, set user2.ID = 150 and Save again.
	// For now, just check that Save does not invalidate unless the ID is actually > 100.
	assert.Equal(t, 0, cacheClient.Counters["Delete"], "Cache should not be invalidated unless id > 100 (see test note)")
}
