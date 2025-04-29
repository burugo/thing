package thing_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	// Import the package we are testing
	// Adjust the import path if your module structure is different
	"thing"
)

// Use a model defined within the main thing package or define one here if needed.
// Reusing setup_test.go models if possible is good practice.

func TestThing_CheckQueryMatch(t *testing.T) {
	// Context is not needed for Save or CheckQueryMatch based on current usage
	// ctx := context.Background()
	// Get the Thing instance, ignore mockCache and dbAdapter for this test
	userThing, _, _, cleanup := setupCacheTest[User](t)
	defer cleanup()

	// --- Setup: Create initial user ---
	user := User{Name: "Charlie", Email: "charlie@example.com"}
	err := userThing.Save(&user) // Use Save for creation
	require.NoError(t, err)
	require.NotZero(t, user.ID)
	t.Logf("Created user: %+v", user)

	// --- Define Queries to check against ---
	exactMatchQuery := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Charlie"},
	}
	likeQuery := thing.QueryParams{
		Where: "email LIKE ?",
		Args:  []interface{}{"%@example.com"},
	}
	mismatchQuery := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"David"},
	}
	multiConditionMatchQuery := thing.QueryParams{
		Where: "name = ? AND email = ?",
		Args:  []interface{}{"Charlie", "charlie@example.com"},
	}
	multiConditionMismatchQuery := thing.QueryParams{
		Where: "name = ? AND email = ?",
		Args:  []interface{}{"Charlie", "wrong@example.com"},
	}

	// --- Test Case 1: Exact Match ---
	t.Run("Exact Match", func(t *testing.T) {
		match, err := userThing.CheckQueryMatch(&user, exactMatchQuery) // Pass model pointer and query
		require.NoError(t, err)
		require.True(t, match, "Expected CheckQueryMatch to return true for initial user and exact match query")
		t.Logf("CheckQueryMatch (Exact Match) returned: %v", match)
	})

	// --- Test Case 2: LIKE Match ---
	t.Run("LIKE Match", func(t *testing.T) {
		match, err := userThing.CheckQueryMatch(&user, likeQuery)
		require.NoError(t, err)
		require.True(t, match, "Expected CheckQueryMatch to return true for user and LIKE query")
		t.Logf("CheckQueryMatch (LIKE Match) returned: %v", match)
	})

	// --- Test Case 3: Mismatch ---
	t.Run("Mismatch", func(t *testing.T) {
		match, err := userThing.CheckQueryMatch(&user, mismatchQuery)
		require.NoError(t, err)
		require.False(t, match, "Expected CheckQueryMatch to return false for user and mismatch query")
		t.Logf("CheckQueryMatch (Mismatch) returned: %v", match)
	})

	// --- Test Case 4: Multi-Condition Match ---
	t.Run("Multi-Condition Match", func(t *testing.T) {
		match, err := userThing.CheckQueryMatch(&user, multiConditionMatchQuery)
		require.NoError(t, err)
		require.True(t, match, "Expected true for multi-condition match query")
		t.Logf("CheckQueryMatch (Multi-Condition Match) returned: %v", match)
	})

	// --- Test Case 5: Multi-Condition Mismatch ---
	t.Run("Multi-Condition Mismatch", func(t *testing.T) {
		match, err := userThing.CheckQueryMatch(&user, multiConditionMismatchQuery)
		require.NoError(t, err)
		require.False(t, match, "Expected false for multi-condition mismatch query")
		t.Logf("CheckQueryMatch (Multi-Condition Mismatch) returned: %v", match)
	})

	// --- Test Case 6: Match after unrelated update ---
	t.Run("Match after unrelated update", func(t *testing.T) {
		// Modify a field NOT in the exactMatchQuery
		user.Email = "charlie.updated@example.com"
		err = userThing.Save(&user) // Use Save for update
		require.NoError(t, err)
		t.Logf("Updated user email: %+v", user)

		// Check against the original query (name = "Charlie")
		match, err := userThing.CheckQueryMatch(&user, exactMatchQuery)
		require.NoError(t, err)
		require.True(t, match, "Expected CheckQueryMatch to return true after unrelated field update")
		t.Logf("CheckQueryMatch after email update returned: %v", match)
	})

	// --- Test Case 7: Mismatch after relevant update ---
	t.Run("Mismatch after relevant update", func(t *testing.T) {
		// Modify a field THAT IS in the exactMatchQuery
		user.Name = "Charles"
		err = userThing.Save(&user) // Use Save for update
		require.NoError(t, err)
		t.Logf("Updated user name: %+v", user)

		// Check against the original query (name = "Charlie")
		match, err := userThing.CheckQueryMatch(&user, exactMatchQuery)
		require.NoError(t, err)
		require.False(t, match, "Expected CheckQueryMatch to return false after relevant field (name) update")
		t.Logf("CheckQueryMatch after name update returned: %v", match)

		// Verify it matches a query for the new name
		matchWithNewNameQuery := thing.QueryParams{Where: "name = ?", Args: []interface{}{"Charles"}}
		match, err = userThing.CheckQueryMatch(&user, matchWithNewNameQuery)
		require.NoError(t, err)
		require.True(t, match, "Expected CheckQueryMatch to return true for updated user and new name query")
	})
}
