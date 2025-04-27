package thing_test

import (
	"testing"

	"thing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThing_ByID_Found(t *testing.T) {
	// Set up test DB and cache
	db, cache := setupTestDB(t)
	th, err := thing.New[User](db, cache)
	require.NoError(t, err)

	// Create a test user
	user := &User{Name: "Test User", Email: "test@example.com"}

	// Save the user to get an ID
	err = th.Save(user)
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
	db, cache := setupTestDB(t)
	th, err := thing.New[User](db, cache)
	require.NoError(t, err)

	// Try to retrieve a non-existent user
	nonExistentID := int64(999)
	_, err = th.ByID(nonExistentID)
	assert.Error(t, err)
	assert.Equal(t, thing.ErrNotFound, err)
}

func TestThing_Save_Create(t *testing.T) {
	// Set up test DB and cache
	db, cache := setupTestDB(t)
	th, err := thing.New[User](db, cache)
	require.NoError(t, err)

	// Create a new user
	user := &User{
		Name:  "New User",
		Email: "new@example.com",
	}

	// Verify user has no ID yet
	assert.Zero(t, user.ID, "New user should have zero ID")

	// Save the user (create operation)
	err = th.Save(user)
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
	db, cache := setupTestDB(t)
	th, err := thing.New[User](db, cache)
	require.NoError(t, err)

	// Create a new user
	user := &User{
		Name:  "Original Name",
		Email: "original@example.com",
	}

	// Save the user initially
	err = th.Save(user)
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
	db, cache := setupTestDB(t)
	th, err := thing.New[User](db, cache)
	require.NoError(t, err)

	// Create a new user
	user := &User{
		Name:  "Delete Me",
		Email: "delete@example.com",
	}

	// Save the user
	err = th.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID, "User should have ID after save")

	// Verify user exists
	_, err = th.ByID(user.ID)
	require.NoError(t, err, "User should exist before deletion")

	// Delete the user
	err = th.Delete(user)
	require.NoError(t, err)

	// Verify user no longer exists
	_, err = th.ByID(user.ID)
	assert.Error(t, err)
	assert.Equal(t, thing.ErrNotFound, err, "User should not exist after deletion")
}

func TestThing_Query(t *testing.T) {
	// Set up test DB and cache
	db, cache := setupTestDB(t)
	th, err := thing.New[User](db, cache)
	require.NoError(t, err)

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
	params := thing.QueryParams{
		Where: "",
	}
	allUsers, err := th.Query(params)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(allUsers), 3, "Should find at least the 3 users we created")

	// Query with a filter
	filterParams := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Bob"},
	}
	bobUsers, err := th.Query(filterParams)
	require.NoError(t, err)
	assert.Equal(t, 1, len(bobUsers), "Should find only Bob")
	assert.Equal(t, "Bob", bobUsers[0].Name)
}

func TestThing_IDs(t *testing.T) {
	// Set up test DB and cache
	db, cache := setupTestDB(t)
	th, err := thing.New[User](db, cache)
	require.NoError(t, err)

	// Create multiple users
	users := []*User{
		{Name: "David", Email: "david@example.com"},
		{Name: "Eva", Email: "eva@example.com"},
		{Name: "Frank", Email: "frank@example.com"},
	}

	var expectedIDs []int64
	for _, u := range users {
		err := th.Save(u)
		require.NoError(t, err)
		require.NotZero(t, u.ID)
		expectedIDs = append(expectedIDs, u.ID)
	}

	// Get IDs for users
	params := thing.QueryParams{
		Where: "name IN (?, ?, ?)",
		Args:  []interface{}{"David", "Eva", "Frank"},
	}
	ids, err := th.IDs(params)
	require.NoError(t, err)
	assert.Len(t, ids, 3, "Should find 3 user IDs")

	// Verify each expected ID is in the result (order may differ)
	for _, expectedID := range expectedIDs {
		found := false
		for _, id := range ids {
			if id == expectedID {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected ID %d not found in results", expectedID)
	}
}
