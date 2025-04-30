package thing_test

import (
	"context"
	"testing"
	"thing/internal/cache"

	"thing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThing_ByID_Found(t *testing.T) {
	// Set up test DB and cache
	th, _, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create a test user
	user := &User{Name: "Test User", Email: "test@example.com"}

	// Save the user to get an ID
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID, "User ID should be set after Save")

	// Retrieve the user by ID
	foundUser, err := th.ByID(user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, foundUser.ID)
	assert.Equal(t, user.Name, foundUser.Name)
	assert.Equal(t, user.Email, foundUser.Email)
}

func TestThing_ByID_NotFound(t *testing.T) {
	// Set up test DB and cache
	th, _, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Try to retrieve a non-existent user
	nonExistentID := int64(999)
	_, err := th.ByID(nonExistentID)
	assert.Error(t, err)
	assert.Equal(t, thing.ErrNotFound, err)
}

func TestThing_Save_Create(t *testing.T) {
	// Set up test DB and cache
	th, _, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create a new user
	user := &User{
		Name:  "New User",
		Email: "new@example.com",
	}

	// Verify user has no ID yet
	assert.Zero(t, user.ID, "New user should have zero ID")

	// Save the user (create operation)
	err := th.Save(user)
	require.NoError(t, err)

	// Verify ID was assigned
	assert.NotZero(t, user.ID, "User should have non-zero ID after create")

	// Verify user was actually saved to the database
	foundUser, err := th.ByID(user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, foundUser.ID)
	assert.Equal(t, user.Name, foundUser.Name)
	assert.Equal(t, user.Email, foundUser.Email)
}

func TestThing_Save_Update(t *testing.T) {
	// Set up test DB and cache
	th, _, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create a new user
	user := &User{
		Name:  "Original Name",
		Email: "original@example.com",
	}

	// Save the user initially
	err := th.Save(user)
	require.NoError(t, err)
	originalID := user.ID
	require.NotZero(t, originalID, "User should have ID after initial save")

	// Update the user
	user.Name = "Updated Name"
	user.Email = "updated@example.com"

	// Save the changes
	err = th.Save(user)
	require.NoError(t, err)

	// Verify ID didn't change
	assert.Equal(t, originalID, user.ID, "User ID should not change after update")

	// Verify changes were saved to the database
	foundUser, err := th.ByID(user.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", foundUser.Name)
	assert.Equal(t, "updated@example.com", foundUser.Email)
}

func TestThing_Delete(t *testing.T) {
	// Set up test DB and cache
	th, mockCache, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create a new user
	user := &User{
		Name:  "Delete Me",
		Email: "delete@example.com",
	}

	// Save the user
	err := th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID, "User should have ID after save")

	// Verify user exists
	_, err = th.ByID(user.ID)
	require.NoError(t, err, "User should exist before deletion")

	// Define query params used for caching tests
	countParams := cache.QueryParams{Where: "name = ?", Args: []interface{}{user.Name}}
	listParams := cache.QueryParams{Where: "email LIKE ?", Args: []interface{}{"%example.com"}}

	// --- Populate caches BEFORE delete ---
	// Perform a count query to cache it
	countResult, err := th.Query(countParams)
	require.NoError(t, err, "Failed to perform count query before delete")
	_, err = countResult.Count() // Trigger count cache population
	require.NoError(t, err, "Failed to get count before delete")

	// Perform a list query to cache it
	listResult, err := th.Query(listParams)
	require.NoError(t, err, "Failed to perform list query before delete")
	_, err = listResult.Fetch(0, 1) // Trigger list cache population
	require.NoError(t, err, "Failed to fetch list before delete")
	// --- End cache population ---

	// Reset calls *after* populating caches but *before* the action being tested (Delete)
	mockCache.ResetCalls() // Reset counters to isolate Delete actions

	// Delete the user
	err = th.Delete(context.Background(), user)
	require.NoError(t, err)

	// Verify user no longer exists
	_, err = th.ByID(user.ID)
	assert.Error(t, err)
	assert.Equal(t, thing.ErrNotFound, err, "User should not exist after deletion")

	// Access the Counters map directly
	// Check counts *after* the Delete operation.
	assert.Equal(t, 0, mockCache.Counters["DeleteModel"], "Expected 0 DeleteModel calls")
	// Expect 2 deletes: 1 for the model itself, 1 for the invalidated list cache
	assert.Equal(t, 2, mockCache.Counters["Delete"], "Expected 2 Delete calls (model + list invalidation)")

	// Cache invalidation for list/count caches involves reads and writes (or deletes).
	// We now DELETE the list cache instead of setting it.
	assert.Equal(t, 2, mockCache.Counters["Get"], "Expected 2 Gets (count name + count email)")
	// Expect 1 GetQueryIDs: During the initial read in handleDeleteInQueryCaches Phase 2.
	assert.Equal(t, 1, mockCache.Counters["GetQueryIDs"], "Expected 1 GetQueryIDs (initial read in Delete)")
	assert.Equal(t, 0, mockCache.Counters["SetQueryIDs"], "Expected 0 SetQueryIDs (list email is now deleted)")
	// Expect 3 sets: 2 for count decrements, 1 for NoneResult caching by post-delete ByID check.
	assert.Equal(t, 3, mockCache.Counters["Set"], "Expected 3 Sets (2 count decrements + 1 NoneResult)")

	// Verify user is actually gone from DB
	_, err = th.ByID(1)
}

func TestThing_Query(t *testing.T) {
	// Set up test DB and cache
	th, _, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// Create multiple users
	users := []*User{
		{Name: "Alice", Email: "alice@example.com"},
		{Name: "Bob", Email: "bob@example.com"},
		{Name: "Charlie", Email: "charlie@example.com"},
	}

	for _, u := range users {
		err := th.Save(u)
		require.NoError(t, err)
		require.NotZero(t, u.ID)
	}

	// Query for all users
	params := cache.QueryParams{
		Where: "",
	}
	allUsersResult, err := th.Query(params)
	require.NoError(t, err)
	// Fetch results before using len
	allUsersFetched, fetchErr := allUsersResult.Fetch(0, 100) // Fetch up to 100
	require.NoError(t, fetchErr)
	assert.GreaterOrEqual(t, len(allUsersFetched), 3, "Should find at least the 3 users we created")

	// Query with a filter
	filterParams := cache.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Bob"},
	}
	bobUsersResult, err := th.Query(filterParams)
	require.NoError(t, err)
	// Fetch results before using len or indexing
	bobUsersFetched, fetchErrBob := bobUsersResult.Fetch(0, 10) // Fetch up to 10
	require.NoError(t, fetchErrBob)
	assert.Equal(t, 1, len(bobUsersFetched), "Should find only Bob")
	assert.Equal(t, "Bob", bobUsersFetched[0].Name)
}
