package thing_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"thing/internal/cache"

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
	assert.Equal(t, 1, mockCache.Counters["GetModel"], "Should attempt to get from cache")
	assert.Equal(t, 1, mockCache.Counters["SetModel"], "Should set in cache after DB fetch")

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
	mockCache.ResetCalls()

	// 4. Fetch again - should miss cache, miss DB, cache NoneResult, return ErrNotFound
	_, err = th.ByID(userID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, thing.ErrNotFound), "Expected ErrNotFound after delete")

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
	assert.Equal(t, thing.NoneResult, rawVal, "Cache should contain NoneResult string")

	// Record counts before the final fetch
	getCallsBeforeFinal := mockCache.Counters["GetModel"]
	setCallsBeforeFinal := mockCache.Counters["Set"]
	setModelCallsBeforeFinal := mockCache.Counters["SetModel"]

	// 5. Fetch one more time - should hit NoneResult in cache, return ErrNotFound immediately
	_, err = th.ByID(userID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, thing.ErrNotFound), "Expected ErrNotFound again")

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
	th, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create multiple users
	userAlice := &User{Name: "Alice Cache", Email: "alice-cache@example.com"}
	userBob := &User{Name: "Bob Cache", Email: "bob-cache@example.com"}
	userCharlie := &User{Name: "Charlie Cache", Email: "charlie-cache@example.com"}

	for _, u := range []*User{userAlice, userBob, userCharlie} {
		err := th.Save(u)
		require.NoError(t, err)
		require.NotZero(t, u.ID)
	}

	// Reset cache metrics
	mockCache.ResetCalls()

	// First query should miss cache - Query for Alice specifically
	params := cache.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Alice Cache"},
	}

	queryResult, err := th.Query(params)
	require.NoError(t, err)
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
	query2Result, err := th.Query(params)
	require.NoError(t, err)
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
	params := cache.QueryParams{
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
	assert.Equal(t, 1, mockCache.Counters["SetQueryIDs"], "Should cache the query")

	// Update the user
	user.Name = "Updated Invalidate User"
	err = th.Save(user)
	require.NoError(t, err)

	// Verify cache invalidation occurred on save
	// The incremental cache update logic in Save should remove the item from relevant list/count caches.

	// Querying for old name should now return no results (cache should be invalidated)
	oldNameParams := cache.QueryParams{
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
	newNameParams := cache.QueryParams{
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
	assert.Equal(t, 0, mockCache.Counters["GetModel"], "Should not try to get from cache for new entity")

	// But the entity should be stored in cache after creation
	assert.GreaterOrEqual(t, mockCache.Counters["SetModel"], 1, "Should set in cache after creation")

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

	// Save the changes
	err = th.Save(user)
	require.NoError(t, err)

	// The cache entry for the specific model should be updated, not invalidated.
	// It will be repopulated on the next read.
	assert.True(t, mockCache.Exists(modelKey), "Model should still exist in cache after update")
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
	thingUser, cacheClient, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// --- Setup: Initial Query & Caching ---
	params := cache.QueryParams{
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
	cacheClient.ResetCalls()

	// --- Test Case 1: Create an item that matches the query ---
	user1 := User{BaseModel: thing.BaseModel{}, Name: "Test User Incremental", Email: "inc1@test.com"}
	err = thingUser.Save(&user1)
	require.NoError(t, err)
	require.NotZero(t, user1.ID, "User1 should have an ID after save")

	// Verify caches were updated incrementally via counters AFTER ResetCalls
	// Create operation: Reads miss cache, writes new values, sets model cache.
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
	cr4, err := thingUser.Query(params)
	require.NoError(t, err)
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
	cr5, err := thingUser.Query(params)
	require.NoError(t, err)
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
	cr6, err := thingUser.Query(params)
	require.NoError(t, err)
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
	thingInstance, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()
	ctx := context.Background()

	// 1. Initial setup: Create some users and cache a query
	user1 := &User{Name: "Alice", Email: "alice@example.com"}
	user2 := &User{Name: "Alice", Email: "alice2@example.com"} // Same name, different email
	user3 := &User{Name: "Charlie", Email: "charlie@example.com"}
	require.NoError(t, thingInstance.Save(user1), "Save user1 failed")
	require.NoError(t, thingInstance.Save(user2), "Save user2 failed")
	require.NoError(t, thingInstance.Save(user3), "Save user3 failed")

	// Define query params for users named "Alice"
	queryParams := cache.QueryParams{Where: "name = ?", Args: []interface{}{"Alice"}}
	queryResult, err := thingInstance.Query(queryParams)
	require.NoError(t, err, "Thing.Query failed")

	// --- Cache the initial query results (list and count) ---
	// Fetch list to cache IDs
	_, err = queryResult.Fetch(0, 10) // Fetch first page to trigger caching
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
	user4 := &User{Name: "Alice", Email: "alice3@example.com"}
	require.NoError(t, thingInstance.Save(user4), "Save user4 failed")

	// Check list cache: user4.ID should be added
	updatedIDs, err := mockCache.GetQueryIDs(ctx, listCacheKey)
	require.NoError(t, err, "GetQueryIDs after creating user4 failed")
	assert.ElementsMatch(t, []int64{user1.ID, user2.ID, user4.ID}, updatedIDs, "List cache not updated correctly after creating matching user")

	// Check count cache: count should be incremented
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after creating user4 failed")
	assert.Equal(t, "3", countStr, "Count cache not incremented after creating matching user")
	mockCache.ResetCalls()

	// --- Test Scenario 2: Create a new user that DOES NOT match the query ---
	user5 := &User{Name: "Eve", Email: "eve@example.com"} // Different name
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

	// Check list cache: user3.ID should be added
	idsAfterUpdate1, err := mockCache.GetQueryIDs(ctx, listCacheKey)
	require.NoError(t, err, "GetQueryIDs after updating user3 to match failed")
	assert.ElementsMatch(t, []int64{user1.ID, user2.ID, user4.ID, user3.ID}, idsAfterUpdate1, "List cache not updated correctly after update TO match")

	// Check count cache: count should be incremented
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after updating user3 to match failed")
	assert.Equal(t, "4", countStr, "Count cache not incremented after update TO match")
	mockCache.ResetCalls()

	// --- Test Scenario 4: Update a user TO NO LONGER match the query ---
	user1.Name = "Alicia" // Change name from "Alice" to "Alicia"
	require.NoError(t, thingInstance.Save(user1), "Save updated user1 failed")

	// Check list cache: user1.ID should be removed
	idsAfterUpdate2, err := mockCache.GetQueryIDs(ctx, listCacheKey)
	require.NoError(t, err, "GetQueryIDs after updating user1 to not match failed")
	assert.ElementsMatch(t, []int64{user2.ID, user4.ID, user3.ID}, idsAfterUpdate2, "List cache not updated correctly after update TO NOT match")

	// Check count cache: count should be decremented
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after updating user1 to not match failed")
	assert.Equal(t, "3", countStr, "Count cache not decremented after update TO NOT match")
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
	require.NoError(t, thingInstance.Delete(user2), "Delete user2 failed") // user2 has name "Alice"

	// Check list cache: user2.ID should be removed
	idsAfterDelete1, err := mockCache.GetQueryIDs(ctx, listCacheKey)
	require.NoError(t, err, "GetQueryIDs after deleting user2 failed")
	assert.ElementsMatch(t, []int64{user4.ID, user3.ID}, idsAfterDelete1, "List cache not updated correctly after deleting matching user")

	// Check count cache: count should be decremented
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after deleting user2 failed")
	assert.Equal(t, "2", countStr, "Count cache not decremented after deleting matching user")
	mockCache.ResetCalls()

	// --- Test Scenario 7: Delete a user that DOES NOT match the query ---
	require.NoError(t, thingInstance.Delete(user5), "Delete user5 failed") // user5 has name "Eve"

	// Check list cache: should NOT have changed
	idsAfterDelete2, err := mockCache.GetQueryIDs(ctx, listCacheKey)
	require.NoError(t, err, "GetQueryIDs after deleting user5 failed")
	assert.ElementsMatch(t, []int64{user4.ID, user3.ID}, idsAfterDelete2, "List cache changed after deleting non-matching user")

	// Check count cache: should NOT have changed
	countStr, err = mockCache.Get(ctx, countCacheKey)
	require.NoError(t, err, "Get count from cache after deleting user5 failed")
	assert.Equal(t, "2", countStr, "Count cache changed after deleting non-matching user")
	mockCache.ResetCalls()
}

// Helper to generate query cache keys (consistent with internal logic)
// NOTE: This duplicates internal logic and might break if internal logic changes.
// Ideally, CacheClient interface might expose a way to get keys, or use exported helpers.
func generateQueryCacheKey(t *testing.T, keyType, tableName string, params cache.QueryParams) string {
	t.Helper()
	// Normalize args for consistent hashing
	normalizedArgs := make([]interface{}, len(params.Args))
	for i, arg := range params.Args {
		normalizedArgs[i] = fmt.Sprintf("%v", arg)
	}
	keyGenParams := params
	keyGenParams.Args = normalizedArgs

	paramsBytes, err := json.Marshal(keyGenParams)
	require.NoError(t, err, "Failed to marshal params for key generation")

	hasher := sha256.New()
	hasher.Write([]byte(tableName))
	hasher.Write(paramsBytes)
	hash := hex.EncodeToString(hasher.Sum(nil))

	return fmt.Sprintf("%s:%s:%s", keyType, tableName, hash)
}
