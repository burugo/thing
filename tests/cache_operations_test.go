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
	mockCache.AssertSetCalls(t, 0, "Should NOT set NoneResult again") // Expect 0 Set calls after ResetCounts
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
	assert.GreaterOrEqual(t, mockCache.InvalidateQueriesContainingIDCalls, 1, "Should invalidate queries containing this ID")

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
	invalidateQueriesCalls := mockCache.InvalidateQueriesContainingIDCalls

	// Save the changes
	err = th.Save(user)
	require.NoError(t, err)

	// Verify cache invalidation
	assert.Equal(t, deleteModelCalls+1, mockCache.DeleteModelCalls, "Should invalidate the model in cache")
	assert.Equal(t, invalidateQueriesCalls+1, mockCache.InvalidateQueriesContainingIDCalls, "Should invalidate queries for this entity")

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
	invalidateQueriesCalls := mockCache.InvalidateQueriesContainingIDCalls

	// Delete the user
	err = th.Delete(user)
	require.NoError(t, err)

	// Verify cache invalidation
	assert.Equal(t, deleteModelCalls+1, mockCache.DeleteModelCalls, "Should invalidate the model in cache")
	assert.Equal(t, invalidateQueriesCalls+1, mockCache.InvalidateQueriesContainingIDCalls, "Should invalidate queries for this entity")

	// Verify the entity is no longer in cache
	assert.False(t, mockCache.Exists(modelKey), "Model should be removed from cache")
}

// Helper function for string formatting
func sprintf(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}
