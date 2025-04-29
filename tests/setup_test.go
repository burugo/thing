package thing_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"thing"
	"thing/drivers/sqlite"
)

// setupTestDB creates a new file-based SQLite database for testing.
// It returns the DBAdapter, a mock CacheClient, and a cleanup function.
func setupTestDB(tb testing.TB) (thing.DBAdapter, thing.CacheClient, func()) {
	// Use a file-based DB for testing to rule out :memory: issues
	dbFile := "test_thing.db"
	_ = os.Remove(dbFile) // Remove any previous test db file
	dsn := dbFile

	adapter, err := sqlite.NewSQLiteAdapter(dsn)
	require.NoError(tb, err, "Failed to create SQLite adapter")

	// Create test tables using the adapter (so it's the same connection)
	// Updated schema to include base model fields
	_, err = adapter.Exec(
		context.Background(),
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted BOOLEAN DEFAULT FALSE,
			name TEXT,
			email TEXT
		);`,
	)
	require.NoError(tb, err, "Failed to create users table")

	_, err = adapter.Exec(
		context.Background(),
		`CREATE TABLE IF NOT EXISTS books (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted BOOLEAN DEFAULT FALSE,
			title TEXT,
			user_id INTEGER,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
	)
	require.NoError(tb, err, "Failed to create books table")

	// Initialize mock cache
	cache := &mockCacheClient{}

	cleanup := func() {
		tb.Logf("--- setupTestDB: Running cleanup function ---")
		if adapter != nil {
			err := adapter.Close()
			if err != nil {
				tb.Logf("Error closing test DB adapter: %v", err)
			}
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
	db, cacheClient, cleanupDB := setupTestDB(tb)
	mockCache, ok := cacheClient.(*mockCacheClient)
	require.True(tb, ok, "Cache client is not a mockCacheClient")

	// Reset mock cache state
	mockCache.Reset()

	// Create Thing instance with the given adapter and cache using New
	// This ensures the instance uses the specific DB/cache we created.
	thingInstance, err := thing.New[T](db, mockCache)
	require.NoError(tb, err, "Failed to create Thing instance with New()")

	// Configure Thing globally ONLY IF tests specifically need to call thing.Use()
	// Generally prefer passing the created thingInstance directly.
	// _ = thing.Configure(db, mockCache, 5*time.Minute)

	cleanup := func() {
		tb.Logf("--- setupCacheTest: Running cleanup function ---")
		cleanupDB() // Call the DB cleanup first
		tb.Logf("--- setupCacheTest: Cleanup function finished ---")
	}

	return thingInstance, mockCache, db, cleanup
}

// SetupTestThing initializes a file-based SQLite database, configures Thing globally,
// and returns a configured Thing instance obtained via thing.Use().
// Use this helper primarily when testing code paths that rely on the global configuration
// and the thing.Use() function. For most tests, setupCacheTest is preferred as it
// provides the specific *thing.Thing instance directly.
func SetupTestThing[T any](t *testing.T) (*thing.Thing[T], func()) {
	dbAdapter, cacheClient, cleanupDB := setupTestDB(t)
	// Ensure cacheClient is the mock type for configuration
	mockCache, ok := cacheClient.(*mockCacheClient)
	require.True(t, ok, "Cache client is not a mockCacheClient in SetupTestThing")

	// Configure Thing globally for the test
	err := thing.Configure(dbAdapter, mockCache, 5*time.Minute)
	require.NoError(t, err, "Failed to configure Thing globally")

	// Get a Thing instance for the specific type T using Use (relies on global config)
	thingInstance, err := thing.Use[T]()
	require.NoError(t, err, "Failed to get Thing instance for type %T via Use()", *new(T))

	// Return the Thing instance and the DB cleanup function
	return thingInstance, cleanupDB
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
