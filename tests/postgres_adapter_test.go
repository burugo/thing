package thing_test

import (
	"testing"
	"thing"

	"github.com/stretchr/testify/require"
)

// TestPostgresBasicCRUD verifies basic CRUD operations using the PostgreSQL adapter.
func TestPostgresBasicCRUD(t *testing.T) {
	db, cacheClient, cleanup := setupPostgresTestDB(t)
	defer cleanup()

	// 检查数据库连接
	if sqlDB := db.DB(); sqlDB != nil {
		if err := sqlDB.Ping(); err != nil {
			t.Logf("PostgreSQL not available, skipping test: %v", err)
			t.Skip("PostgreSQL not available")
		}
	}

	// Use the shared User model from the thing package
	thingInstance, err := thing.New[*User](db, cacheClient)
	require.NoError(t, err)

	// Create
	user := &User{Name: "Alice", Email: "alice@example.com"}
	err = thingInstance.Save(user)
	require.NoError(t, err)
	require.NotZero(t, user.ID)

	// Read
	fetched, err := thingInstance.ByID(user.ID)
	require.NoError(t, err)
	require.Equal(t, user.Name, fetched.Name)

	// Update
	fetched.Name = "Alice Updated"
	err = thingInstance.Save(fetched)
	require.NoError(t, err)
	updated, err := thingInstance.ByID(user.ID)
	require.NoError(t, err)
	require.Equal(t, "Alice Updated", updated.Name)

	// Delete
	err = thingInstance.Delete(user)
	require.NoError(t, err)
	_, err = thingInstance.ByID(user.ID)
	require.Error(t, err)
}
