package thing_test

import (
	"context"
	"testing"
	"thing/internal/cache"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	thing "thing"
)

// TestTransaction_Commit verifies that operations within a committed transaction persist.
func TestTransaction_Commit(t *testing.T) {
	th, _, dbAdapter, cleanup := setupCacheTest[*User](t) // Get DBAdapter too
	defer cleanup()

	ctx := context.Background()
	var originalUser *User

	// 1. Create initial data outside transaction using the ORM
	userToCreate := &User{Name: "Commit Test User", Email: "commit@example.com"}
	err := th.Save(userToCreate) // Pass only the model to Save
	require.NoError(t, err, "Failed to create initial user")
	require.NotZero(t, userToCreate.ID, "Initial user ID should not be zero")

	// 2. Start Transaction using the DB Adapter
	tx, err := dbAdapter.BeginTx(ctx, nil)
	require.NoError(t, err, "Failed to begin transaction")
	require.NotNil(t, tx, "Transaction object should not be nil")

	// 3. Perform operations within transaction using the Tx object
	// 3a. Get the user using Tx.Get
	err = tx.Get(ctx, &originalUser, "SELECT id, created_at, updated_at, deleted, name, email FROM users WHERE id = ?", userToCreate.ID)
	require.NoError(t, err, "Failed to get user within transaction")
	assert.Equal(t, userToCreate.Name, originalUser.Name)

	// 3b. Update the user using Tx.Exec
	newName := "Committed Name"
	originalUser.Name = newName // Update the struct field
	res, err := tx.Exec(ctx, "UPDATE users SET name = ? WHERE id = ?", newName, originalUser.ID)
	require.NoError(t, err, "Failed to update user within transaction")
	rowsAffected, _ := res.RowsAffected()
	assert.Equal(t, int64(1), rowsAffected, "Expected 1 row to be affected by update")

	// 3c. Select the user again using Tx.Get to verify intermediate state
	var updatedUserTx *User
	err = tx.Get(ctx, &updatedUserTx, "SELECT id, created_at, updated_at, deleted, name, email FROM users WHERE id = ?", originalUser.ID)
	require.NoError(t, err, "Failed to get updated user within transaction")
	assert.Equal(t, newName, updatedUserTx.Name)

	// 4. Commit Transaction using the Tx object
	err = tx.Commit()
	require.NoError(t, err, "Failed to commit transaction")

	// Clear cache manually before checking ByID
	err = th.ClearCacheByID(ctx, originalUser.ID)
	require.NoError(t, err, "Failed to clear cache for user ID %d", originalUser.ID)

	// 5. Verify changes persisted after commit
	// 5a. Use ORM ByID (should now hit DB or get newly cached value)
	finalUserPtr, err := th.ByID(originalUser.ID) // Pass only ID to ByID
	require.NoError(t, err, "Failed to get user via ByID after commit")
	require.NotNil(t, finalUserPtr, "User pointer should not be nil after ByID")
	assert.Equal(t, newName, finalUserPtr.Name, "User name should be updated after commit (checked via ByID)")

	// 5b. Verify using raw DBAdapter.Get to bypass cache
	var finalUserDb *User
	dbErr := dbAdapter.Get(ctx, &finalUserDb, "SELECT id, created_at, updated_at, deleted, name, email FROM users WHERE id = ?", originalUser.ID)
	require.NoError(t, dbErr, "Failed raw DB get after commit")
	assert.Equal(t, newName, finalUserDb.Name, "User name should be updated in DB after commit")
}

// TestTransaction_Rollback verifies that operations within a rolled-back transaction do not persist.
func TestTransaction_Rollback(t *testing.T) {
	th, _, dbAdapter, cleanup := setupCacheTest[*User](t) // Get DBAdapter too
	defer cleanup()

	ctx := context.Background()

	// 1. Create initial data using the ORM
	userToCreate := &User{Name: "Rollback Test User", Email: "rollback@example.com"}
	err := th.Save(userToCreate) // Pass only the model to Save
	require.NoError(t, err, "Failed to create initial user")
	initialID := userToCreate.ID
	require.NotZero(t, initialID, "Initial user ID should not be zero")
	initialName := userToCreate.Name // Store initial name

	// 2. Start Transaction using the DB Adapter
	tx, err := dbAdapter.BeginTx(ctx, nil)
	require.NoError(t, err, "Failed to begin transaction")

	// 3. Perform operations within transaction using the Tx object
	// 3a. Update the user using Tx.Exec
	updateName := "Rolled Back Name"
	_, err = tx.Exec(ctx, "UPDATE users SET name = ? WHERE id = ?", updateName, initialID)
	require.NoError(t, err, "Failed to update user within transaction")

	// 3b. Select within transaction using Tx.Get to verify intermediate state
	var updatedUserTx *User
	err = tx.Get(ctx, &updatedUserTx, "SELECT id, created_at, updated_at, deleted, name, email FROM users WHERE id = ?", initialID)
	require.NoError(t, err, "Failed to get user within transaction")
	assert.Equal(t, updateName, updatedUserTx.Name, "Name should be updated within transaction")

	// 3c. Create a new user within transaction using Tx.Exec
	newUserTx := User{Name: "Temp User", Email: "temp@example.com"}
	_, err = tx.Exec(ctx, "INSERT INTO users (name, email, created_at, updated_at) VALUES (?, ?, ?, ?)", newUserTx.Name, newUserTx.Email, time.Now(), time.Now())
	require.NoError(t, err, "Failed to insert user within transaction")

	// 4. Rollback Transaction using the Tx object
	err = tx.Rollback()
	require.NoError(t, err, "Failed to rollback transaction")

	// 5. Verify changes did NOT persist after rollback
	// 5a. Check original user's name using DBAdapter.Get (bypass cache)
	var finalUser User
	err = dbAdapter.Get(ctx, &finalUser, "SELECT id, created_at, updated_at, deleted, name, email FROM users WHERE id = ?", initialID)
	require.NoError(t, err, "Failed to get original user after rollback")
	assert.Equal(t, initialName, finalUser.Name, "User name should NOT be updated after rollback")

	// 5b. Check if the temporary user exists using DBAdapter.GetCount
	// Construct minimal ModelInfo needed for GetCount
	userInfo := &thing.ModelInfo{
		TableName: "users",
		// Columns might not be strictly needed if GetCount only uses TableName and Where
		// Columns: []string{"id", "name", "email", "created_at", "updated_at"},
	}
	tempUserCount, dbErr := dbAdapter.GetCount(ctx, userInfo, cache.QueryParams{Where: "email = ?", Args: []interface{}{newUserTx.Email}})
	require.NoError(t, dbErr, "Failed to count temp user after rollback")
	assert.Zero(t, tempUserCount, "Temporary user created in transaction should not exist after rollback")
}

// TestTransaction_Select verifies that Select operations work correctly within a transaction.
func TestTransaction_Select(t *testing.T) {
	th, _, dbAdapter, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	ctx := context.Background()

	// 1. Create initial data using the ORM
	user1 := &User{Name: "Select Tx User 1", Email: "selecttx1@example.com"}
	user2 := &User{Name: "Select Tx User 2", Email: "selecttx2@example.com"}
	require.NoError(t, th.Save(user1))
	require.NoError(t, th.Save(user2))
	require.NotZero(t, user1.ID)
	require.NotZero(t, user2.ID)

	// 2. Start Transaction using the DB Adapter
	tx, err := dbAdapter.BeginTx(ctx, nil)
	require.NoError(t, err, "Failed to begin transaction")

	// 3. Perform Select within transaction using Tx.Select
	var selectedUsers []*User // Select into slice of pointers
	query := "SELECT id, created_at, updated_at, deleted, name, email FROM users WHERE email LIKE ? ORDER BY email ASC"
	args := []interface{}{"selecttx%@example.com"}
	err = tx.Select(ctx, &selectedUsers, query, args...) // Unpack args slice
	require.NoError(t, err, "Failed to Select users within transaction")

	// 4. Verify results within the transaction context
	require.Len(t, selectedUsers, 2, "Should select 2 users within transaction")
	assert.Equal(t, user1.ID, selectedUsers[0].ID)
	assert.Equal(t, user1.Name, selectedUsers[0].Name)
	assert.Equal(t, user2.ID, selectedUsers[1].ID)
	assert.Equal(t, user2.Name, selectedUsers[1].Name)

	// 5. Commit Transaction
	err = tx.Commit()
	require.NoError(t, err, "Failed to commit transaction")

	// Optional: Verify Select outside transaction (using dbAdapter) yields same result
	var finalSelectedUsers []*User
	dbErr := dbAdapter.Select(ctx, &finalSelectedUsers, query, args...) // Unpack args slice
	require.NoError(t, dbErr, "Failed to Select users outside transaction after commit")
	require.Len(t, finalSelectedUsers, 2, "Should select 2 users outside transaction after commit")
	assert.Equal(t, user1.ID, finalSelectedUsers[0].ID)
	assert.Equal(t, user2.ID, finalSelectedUsers[1].ID)
}

// TODO: Add tests for error conditions (e.g., commit after rollback, double commit/rollback)
// TODO: Add tests for nested transactions if supported/intended (likely not with standard sql.Tx)
// TODO: Test interaction with cache invalidation within transactions (if applicable/expected)
