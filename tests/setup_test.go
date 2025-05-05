package thing_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/burugo/thing"
	"github.com/burugo/thing/drivers/db/mysql"
	"github.com/burugo/thing/drivers/db/postgres"
	"github.com/burugo/thing/drivers/db/sqlite"
	"github.com/burugo/thing/internal/cache"
	"github.com/burugo/thing/internal/types"
)

// setupTestDB creates a new file-based SQLite database for testing.
// It returns the DBAdapter, a mock CacheClient, and a cleanup function.
func setupTestDB(tb testing.TB) (thing.DBAdapter, *mockCacheClient, func()) {
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

	// Initialize mock mockcache
	mockcache := &mockCacheClient{}

	// Wrap SQLiteAdapter in mockDBAdapter for counting
	mockDB := &mockDBAdapter{SQLiteAdapter: adapter}

	// Reset the global query cache index to prevent interference between tests
	cache.ResetGlobalCacheIndex()

	cleanup := func() {
		tb.Logf("--- setupTestDB: Running cleanup function ---")
		if mockDB != nil {
			err := mockDB.Close()
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

	return mockDB, mockcache, cleanup
}

// setupMySQLTestDB creates a MySQL database for testing.
func setupMySQLTestDB(tb testing.TB) (thing.DBAdapter, thing.CacheClient, func()) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		dsn = "root:password@tcp(127.0.0.1:3306)/test_db?parseTime=true"
	}
	adapter, err := mysql.NewMySQLAdapter(dsn)
	if err != nil {
		tb.Logf("MySQL not available, skipping test: %v", err)
		tb.Skip("MySQL not available")
	}

	// Drop tables first to ensure clean state
	_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS books;")
	_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS users;")
	_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS diff_users;") // Drop diff table specifically

	// Create test tables (same schema as SQLite)
	_, err = adapter.Exec(
		context.Background(),
		`CREATE TABLE IF NOT EXISTS users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			created_at DATETIME,
			updated_at DATETIME,
			deleted BOOLEAN DEFAULT FALSE,
			name VARCHAR(255),
			email VARCHAR(255)
		);`,
	)
	require.NoError(tb, err, "Failed to create users table (MySQL)")

	_, err = adapter.Exec(
		context.Background(),
		`CREATE TABLE IF NOT EXISTS books (
			id INT AUTO_INCREMENT PRIMARY KEY,
			created_at DATETIME,
			updated_at DATETIME,
			deleted BOOLEAN DEFAULT FALSE,
			title VARCHAR(255),
			user_id INT,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
	)
	require.NoError(tb, err, "Failed to create books table (MySQL)")

	mockcache := &mockCacheClient{}
	cleanup := func() {
		tb.Logf("--- setupMySQLTestDB: Running cleanup function ---")
		// Drop tables after test
		if adapter != nil {
			_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS books;")
			_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS users;")
			_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS diff_users;")
			_ = adapter.Close()
		}
		tb.Logf("--- setupMySQLTestDB: Cleanup finished ---")
	}
	return adapter, mockcache, cleanup
}

// setupPostgresTestDB creates a PostgreSQL database for testing.
func setupPostgresTestDB(tb testing.TB) (thing.DBAdapter, thing.CacheClient, func()) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:password@localhost:5432/test_db?sslmode=disable"
	}
	adapter, err := postgres.NewPostgreSQLAdapter(dsn)
	if err != nil {
		tb.Logf("PostgreSQL not available, skipping test: %v", err)
		tb.Skip("PostgreSQL not available")
	}

	// Drop tables first to ensure clean state
	_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS books;")
	_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS users;")
	_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS diff_users;") // Drop diff table specifically

	// Create test tables (same schema as MySQL/SQLite)
	_, err = adapter.Exec(
		context.Background(),
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			created_at TIMESTAMP,
			updated_at TIMESTAMP,
			deleted BOOLEAN DEFAULT FALSE,
			name VARCHAR(255),
			email VARCHAR(255)
		);`,
	)
	require.NoError(tb, err, "Failed to create users table (PostgreSQL)")

	_, err = adapter.Exec(
		context.Background(),
		`CREATE TABLE IF NOT EXISTS books (
			id SERIAL PRIMARY KEY,
			created_at TIMESTAMP,
			updated_at TIMESTAMP,
			deleted BOOLEAN DEFAULT FALSE,
			title VARCHAR(255),
			user_id INT,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
	)
	require.NoError(tb, err, "Failed to create books table (PostgreSQL)")

	mockcache := &mockCacheClient{}
	cleanup := func() {
		tb.Logf("--- setupPostgresTestDB: Running cleanup function ---")
		// Drop tables after test
		if adapter != nil {
			_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS books;")
			_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS users;")
			_, _ = adapter.Exec(context.Background(), "DROP TABLE IF EXISTS diff_users;")
			_ = adapter.Close()
		}
		tb.Logf("--- setupPostgresTestDB: Cleanup finished ---")
	}
	return adapter, mockcache, cleanup
}

// setupCacheTest creates test setup specifically for cache tests.
// It returns the Thing instance, the mock cache, the DB adapter, and a cleanup function.
func setupCacheTest[T thing.Model](tb testing.TB) (*thing.Thing[T], *mockCacheClient, thing.DBAdapter, func()) {
	db, mockCache, cleanupDB := setupTestDB(tb)

	// Reset mock cache state
	mockCache.Reset()

	// Reset the global query cache index to prevent interference between tests
	cache.ResetGlobalCacheIndex()

	// Create Thing instance with the given adapter and cache using New
	// This ensures the instance uses the specific DB/cache we created.
	thingInstance, err := thing.New[T](db, mockCache)
	require.NoError(tb, err, "Failed to create Thing instance with New()")

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
func SetupTestThing[T thing.Model](t *testing.T) (*thing.Thing[T], func()) {
	dbAdapter, cacheClient, cleanupDB := setupTestDB(t)
	// Ensure cacheClient is the mock type for configuration
	mockCache := cacheClient

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

// toInternalQueryParams converts thing.QueryParams to types.QueryParams for use with internal/test APIs.
func toInternalQueryParams(p thing.QueryParams) types.QueryParams {
	return types.QueryParams{
		Where:          p.Where,
		Args:           p.Args,
		Order:          p.Order,
		Preloads:       p.Preloads,
		IncludeDeleted: p.IncludeDeleted,
	}
}
