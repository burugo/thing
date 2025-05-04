package thing_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/burugo/thing"
	"github.com/burugo/thing/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Basic test for the SoftDelete method
func TestThing_SoftDelete_Basic(t *testing.T) {
	th, _, _, cleanup := setupCacheTest[*User](t) // Use setupCacheTest
	defer cleanup()

	// 1. Create User
	user := &User{Name: "Soft Delete Me", Email: "softdelete@example.com"}
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)
	originalUpdatedAt := user.UpdatedAt

	// Ensure UpdatedAt changes during SoftDelete
	time.Sleep(1 * time.Millisecond)

	// 2. Soft Delete User
	err = th.SoftDelete(user)
	require.NoError(t, err)

	// 3. Verify struct state
	assert.True(t, user.Deleted, "User.Deleted flag should be true after SoftDelete")
	assert.True(t, user.UpdatedAt.After(originalUpdatedAt), "User.UpdatedAt should be updated after SoftDelete")

	// Add a small delay to allow potential cache propagation in mock (though ideally unnecessary)
	time.Sleep(1 * time.Millisecond)

	// 4. Verify fetching ByID succeeds but returns the soft-deleted record
	// Now we expect ByID to SUCCEED, returning the record marked as deleted.
	fetchedUser, err := th.ByID(user.ID)
	require.NoError(t, err, "Fetching soft-deleted user by ID should succeed")
	require.NotNil(t, fetchedUser, "Fetched user should not be nil")
	assert.True(t, fetchedUser.Deleted, "Fetched user via ByID should have Deleted flag set to true")
	assert.Equal(t, user.ID, fetchedUser.ID, "Fetched user ID should match")
	assert.Equal(t, user.Name, fetchedUser.Name, "Fetched user Name should match") // Verify other fields too

	// 5. Verify standard Query does not find the user
	params := thing.QueryParams{Where: "id = ?", Args: []interface{}{user.ID}}
	queryResult, err := th.Query(params)
	require.NoError(t, err)

	count, err := queryResult.Count()
	require.NoError(t, err)
	assert.EqualValues(t, 0, count, "Standard query count should be 0 for soft-deleted user")

	fetched, err := queryResult.Fetch(0, 1)
	require.NoError(t, err)
	assert.Len(t, fetched, 0, "Standard query fetch should find 0 soft-deleted users")
}

// Test the WithDeleted option for Query
func TestThing_Query_WithDeleted(t *testing.T) {
	th, _, _, cleanup := setupCacheTest[*User](t) // Use setupCacheTest
	defer cleanup()

	// 1. Create User
	user := &User{Name: "Include Deleted Me", Email: "withdeleted@example.com"}
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)

	// 2. Soft Delete User
	err = th.SoftDelete(user)
	require.NoError(t, err)

	// 3. Standard Query should not find the user
	params := thing.QueryParams{Where: "id = ?", Args: []interface{}{user.ID}}
	stdQueryResult, err := th.Query(params)
	require.NoError(t, err)

	stdCount, err := stdQueryResult.Count()
	require.NoError(t, err)
	assert.EqualValues(t, 0, stdCount, "Standard query count should be 0")

	stdFetched, err := stdQueryResult.Fetch(0, 1)
	require.NoError(t, err)
	assert.Len(t, stdFetched, 0, "Standard query fetch should find 0")

	// 4. Query with WithDeleted should find the user
	baseQueryResult, err := th.Query(params)
	require.NoError(t, err, "Base query for WithDeleted failed")
	deletedQueryResult := baseQueryResult.WithDeleted() // Chain WithDeleted
	require.NotNil(t, deletedQueryResult, "WithDeleted should return a new query result")

	deletedCount, err := deletedQueryResult.Count()
	require.NoError(t, err)
	assert.EqualValues(t, 1, deletedCount, "WithDeleted query count should be 1")

	deletedFetched, err := deletedQueryResult.Fetch(0, 1)
	require.NoError(t, err)
	require.Len(t, deletedFetched, 1, "WithDeleted query fetch should find 1")
	assert.Equal(t, user.ID, deletedFetched[0].ID)
	assert.True(t, deletedFetched[0].Deleted, "Fetched user should have Deleted flag set")
}

// Test cache interactions during SoftDelete
func TestThing_SoftDelete_CacheInteraction(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[*User](t) // Use cache setup
	defer cleanup()

	// 1. Create User
	user := &User{Name: "Soft Delete Cache", Email: "softcache@example.com"}
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)
	modelKey := "users:" + mockCache.FormatID(user.ID)

	// 2. Populate Caches
	// Fetch ByID to populate model cache
	_, err = th.ByID(user.ID)
	require.NoError(t, err)
	require.True(t, mockCache.Exists(modelKey), "Model should be in cache before soft delete")

	// Query to populate list/count cache
	params := thing.QueryParams{Where: "name = ?", Args: []interface{}{"Soft Delete Cache"}}
	listCacheKey := generateQueryCacheKey(t, "list", user.TableName(), params)
	countCacheKey := generateQueryCacheKey(t, "count", user.TableName(), params)

	queryResult, err := th.Query(params)
	require.NoError(t, err)
	_, err = queryResult.Fetch(0, 1) // Fetch to populate
	require.NoError(t, err)
	count, err := queryResult.Count() // Count to populate
	require.NoError(t, err)
	require.EqualValues(t, 1, count)

	// Check caches are populated
	_, err = mockCache.GetQueryIDs(context.Background(), listCacheKey)
	require.NoError(t, err, "List cache should exist before soft delete")
	_, err = mockCache.Get(context.Background(), countCacheKey)
	require.NoError(t, err, "Count cache should exist before soft delete")

	// Reset call counts before soft delete
	mockCache.ResetCalls()

	// 3. Soft Delete User
	err = th.SoftDelete(user)
	require.NoError(t, err)

	// 4. Verify Cache Interactions
	// SoftDelete uses Save -> updateAffectedQueryCaches
	// Model cache should be updated (SetModel)
	assert.Equal(t, 1, mockCache.Counters["SetModel"], "SoftDelete should call SetModel via Save")
	assert.Equal(t, 0, mockCache.Counters["DeleteModel"], "SoftDelete should not call DeleteModel")
	// List cache should be invalidated (Delete)
	assert.Equal(t, 1, mockCache.Counters["Delete"], "SoftDelete should trigger list cache invalidation (Delete)")
	// Count cache should be updated (Set)
	assert.Equal(t, 1, mockCache.Counters["Set"], "SoftDelete should trigger count cache update (Set)")

	// 5. Verify Cache State
	// Model cache exists but is marked deleted (or updated by SetModel)
	assert.True(t, mockCache.Exists(modelKey), "Model cache should still exist after SoftDelete (SetModel was called)")
	var cachedUserVal User
	err = mockCache.GetModel(context.Background(), modelKey, &cachedUserVal)             // Pass pointer to struct
	require.NoError(t, err, "GetModel from cache after soft delete failed unexpectedly") // Should find it
	assert.True(t, cachedUserVal.Deleted, "Cached model should have Deleted = true")

	// List cache should be gone
	_, err = mockCache.GetQueryIDs(context.Background(), listCacheKey)
	require.Error(t, err, "List cache should be invalidated")
	assert.True(t, errors.Is(err, common.ErrNotFound), "Expected ErrNotFound for list cache")

	// Count cache should be updated to 0
	countStr, err := mockCache.Get(context.Background(), countCacheKey)
	require.NoError(t, err, "Count cache should still exist")
	assert.Equal(t, "0", countStr, "Count cache should be updated to 0")

	// 6. Verify Fetching Behavior
	// ByID should now SUCCEED and return the soft-deleted record
	fetchedUserByID, errByID := th.ByID(user.ID)
	require.NoError(t, errByID, "ByID should succeed for soft-deleted user")
	require.NotNil(t, fetchedUserByID, "ByID result should not be nil")
	assert.True(t, fetchedUserByID.Deleted, "Fetched user via ByID should have Deleted flag set")

	// Standard Query should find 0
	queryResultStd, err := th.Query(params)
	require.NoError(t, err)
	countStd, err := queryResultStd.Count()
	require.NoError(t, err)
	assert.EqualValues(t, 0, countStd)
	fetchStd, err := queryResultStd.Fetch(0, 1)
	require.NoError(t, err)
	assert.Len(t, fetchStd, 0)

	// Query with WithDeleted should find 1
	baseQueryResultDel, err := th.Query(params)
	require.NoError(t, err, "Base query for WithDeleted failed")
	queryResultDel := baseQueryResultDel.WithDeleted()
	countDel, err := queryResultDel.Count()
	require.NoError(t, err)
	assert.EqualValues(t, 1, countDel) // Count includes deleted
	fetchDel, err := queryResultDel.Fetch(0, 1)
	require.NoError(t, err)
	require.Len(t, fetchDel, 1)
	assert.Equal(t, user.ID, fetchDel[0].ID)
	assert.True(t, fetchDel[0].Deleted)
}

// Test that using WithDeleted doesn't affect the original query object
func TestThing_WithDeleted_Immutability(t *testing.T) {
	th, _, _, cleanup := setupCacheTest[*User](t) // Use setupCacheTest
	defer cleanup()

	// Create and soft delete a user
	user := &User{Name: "Immutable Test", Email: "immutable@example.com"}
	require.NoError(t, th.Save(user))
	require.NoError(t, th.SoftDelete(user))

	// Original query
	params := thing.QueryParams{Where: "id = ?", Args: []interface{}{user.ID}}
	originalQuery, err := th.Query(params)
	require.NoError(t, err)

	// Create WithDeleted query
	baseQueryResultDel, err := th.Query(params)
	require.NoError(t, err, "Base query for WithDeleted failed")
	deletedQuery := baseQueryResultDel.WithDeleted()
	require.NotNil(t, deletedQuery)
	require.NotEqual(t, originalQuery, deletedQuery, "WithDeleted should return a new instance")

	// Verify behavior reflects the params
	// Original
	countOrig, _ := originalQuery.Count()
	fetchOrig, _ := originalQuery.Fetch(0, 1)
	assert.EqualValues(t, 0, countOrig)
	assert.Len(t, fetchOrig, 0)

	// WithDeleted
	countDel, _ := deletedQuery.Count()
	fetchDel, _ := deletedQuery.Fetch(0, 1)
	assert.EqualValues(t, 1, countDel)
	assert.Len(t, fetchDel, 1)
}
