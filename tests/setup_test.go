package thing_test

import (
	"context"
	"log"
	"os"
	"testing"

	"thing"
	"thing/drivers/sqlite"

	"github.com/stretchr/testify/require"
)

// setupTestDB creates a new in-memory SQLite database for testing.
// It returns the DBAdapter, a mock CacheClient, and a cleanup function.
func setupTestDB(tb testing.TB) (thing.DBAdapter, thing.CacheClient, func()) {
	// Use in-memory DB for all connections in this test
	// Add cache=shared to potentially share the in-memory db across connections from the pool
	// dsn := ":memory:?cache=shared"
	// Use a file-based DB for testing to rule out :memory: issues
	dbFile := "test_thing.db"
	_ = os.Remove(dbFile) // Remove any previous test db file
	dsn := dbFile

	adapter, err := sqlite.NewSQLiteAdapter(dsn)
	require.NoError(tb, err, "Failed to create SQLite adapter")

	// Create test tables using the adapter (so it's the same connection)
	_, err = adapter.Exec(
		context.Background(),
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY,
			name TEXT,
			email TEXT,
			created_at DATETIME,
			updated_at DATETIME
		);`,
	)
	require.NoError(tb, err, "Failed to create users table")

	_, err = adapter.Exec(
		context.Background(),
		`CREATE TABLE IF NOT EXISTS books (
			id INTEGER PRIMARY KEY,
			title TEXT,
			user_id INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
	)
	require.NoError(tb, err, "Failed to create books table")

	// Initialize mock cache
	cache := &mockCacheClient{}

	cleanup := func() {
		tb.Logf("--- setupTestDB: Running cleanup function ---")
		err := adapter.Close()
		if err != nil {
			tb.Logf("Error closing test DB adapter: %v", err)
			// Optionally panic or fail test if Close fails critically
			// tb.Fatalf("CRITICAL: Failed to close test DB adapter: %v", err)
		}
		// Remove the test database file
		removeErr := os.Remove(dbFile)
		if removeErr != nil && !os.IsNotExist(removeErr) {
			tb.Logf("Error removing test DB file %s: %v", dbFile, removeErr)
		}
		tb.Logf("--- setupTestDB: Cleanup function finished ---")
	}

	return adapter, cache, cleanup
}

// setupCacheTest creates test setup specifically for cache tests.
// It returns the Thing instance, the mock cache, the DB adapter, and a cleanup function.
func setupCacheTest[T any](tb testing.TB) (*thing.Thing[T], *mockCacheClient, thing.DBAdapter, func()) {
	db, cacheClient, cleanup := setupTestDB(tb)
	mockCache, ok := cacheClient.(*mockCacheClient)
	require.True(tb, ok, "Cache client is not a mockCacheClient")

	// Reset mock cache state
	mockCache.Reset()

	// Create Thing instance with the given adapter and cache
	thingInstance, err := thing.New[T](db, mockCache)
	require.NoError(tb, err, "Failed to create Thing instance")

	return thingInstance, mockCache, db, cleanup
}

// TestMain handles global test setup and teardown
func TestMain(m *testing.M) {
	log.Println("--- TestMain START ---")
	// Set up any global test state here
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println("--- TestMain: Running m.Run() ---")
	// Run tests
	exitCode := m.Run()
	log.Printf("--- TestMain: m.Run() finished with exitCode: %d ---", exitCode)

	// Perform any global cleanup here
	log.Println("--- TestMain: Performing global cleanup (if any) ---")

	// Exit with appropriate code
	log.Printf("--- TestMain: Exiting with code: %d ---", exitCode)
	os.Exit(exitCode)
}
