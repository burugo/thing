package thing_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	// Import the package under test (now at module root)
	"thing"
	// Import the internal package ONLY for the adapter implementation in the test setup
	// Production code should not import internal packages directly.
	// "thing/internal" // No longer needed
	// Import the SQLite driver specifically for test setup
	"thing/drivers/sqlite"
)

// --- Test Models ---

// User represents a user for testing.
type User struct {
	thing.BaseModel
	Name  string `db:"name"`
	Email string `db:"email"`
	// Books []Book `thing:"rel:has_many;fk:UserID"` // Relation handling might change
}

// Change TableName to pointer receiver to satisfy Model[T] interface directly
func (u *User) TableName() string {
	return "users"
}

// Book represents a book for testing.
type Book struct {
	thing.BaseModel
	Title  string `db:"title"`
	UserID int64  `db:"user_id"`
	// User   *User  `thing:"rel:belongs_to;fk:UserID"` // Relation handling might change
}

// Change TableName to pointer receiver
func (b *Book) TableName() string {
	return "books"
}

// --- Explicit Model interface satisfaction for *User --- Removed Wrappers
// func (u *User) GetID() int64 { return u.BaseModel.ID }
// func (u *User) SetID(id int64)     { u.BaseModel.SetID(id) }

// --- Test Setup ---

// Mock CacheClient for testing purposes
type mockCacheClient struct {
	// Use sync.Map for thread-safe storage of models, query IDs, and locks
	store sync.Map
}

// Reset clears the mock cache's internal store.
func (m *mockCacheClient) Reset() {
	// Range and delete seems the most straightforward way to clear sync.Map
	m.store.Range(func(key, value interface{}) bool {
		m.store.Delete(key)
		return true
	})
}

// Exists checks if a key is present in the mock cache store.
// Note: This doesn't check lock prefixes.
func (m *mockCacheClient) Exists(key string) bool {
	_, ok := m.store.Load(key)
	return ok
}

// GetValue retrieves the raw stored bytes for a key from the mock cache.
// Note: This doesn't check lock prefixes.
func (m *mockCacheClient) GetValue(key string) ([]byte, bool) {
	val, ok := m.store.Load(key)
	if !ok {
		return nil, false
	}
	storedBytes, ok := val.([]byte)
	if !ok {
		// Value exists but isn't bytes - internal error or maybe a lock key?
		// Return false as it's not a valid model/query cache entry.
		return nil, false
	}
	return storedBytes, true
}

// Helper to marshal data for storage
func marshalForMock(data interface{}) ([]byte, error) {
	return json.Marshal(data)
}

// Helper to unmarshal data from storage
func unmarshalFromMock(stored []byte, dest interface{}) error {
	return json.Unmarshal(stored, dest)
}

func (m *mockCacheClient) GetModel(ctx context.Context, key string, dest interface{}) error {
	val, ok := m.store.Load(key)
	if !ok {
		return thing.ErrNotFound // Not found
	}

	storedBytes, ok := val.([]byte)
	if !ok {
		// Should not happen if SetModel stores bytes
		return fmt.Errorf("mock cache internal error: value for key '%s' is not []byte", key)
	}

	if err := unmarshalFromMock(storedBytes, dest); err != nil {
		return fmt.Errorf("mock cache unmarshal error for key '%s': %w", key, err)
	}
	return nil // Found and successfully unmarshaled
}

func (m *mockCacheClient) SetModel(ctx context.Context, key string, model interface{}, expiration time.Duration) error {
	// Ignore expiration for mock, just store the marshaled data
	dataBytes, err := marshalForMock(model)
	if err != nil {
		return fmt.Errorf("mock cache marshal error for key '%s': %w", key, err)
	}
	m.store.Store(key, dataBytes)
	return nil
}

func (m *mockCacheClient) DeleteModel(ctx context.Context, key string) error {
	m.store.Delete(key)
	return nil
}

func (m *mockCacheClient) GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error) {
	val, ok := m.store.Load(queryKey)
	if !ok {
		return nil, thing.ErrNotFound
	}
	storedBytes, ok := val.([]byte)
	if !ok {
		return nil, fmt.Errorf("mock cache internal error: value for query key '%s' is not []byte", queryKey)
	}
	var ids []int64
	if err := unmarshalFromMock(storedBytes, &ids); err != nil {
		return nil, fmt.Errorf("mock cache unmarshal error for query key '%s': %w", queryKey, err)
	}
	return ids, nil
}

func (m *mockCacheClient) SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error {
	dataBytes, err := marshalForMock(ids)
	if err != nil {
		return fmt.Errorf("mock cache marshal error for query key '%s': %w", queryKey, err)
	}
	m.store.Store(queryKey, dataBytes)
	return nil
}

func (m *mockCacheClient) DeleteQueryIDs(ctx context.Context, queryKey string) error {
	m.store.Delete(queryKey)
	return nil
}

// Simple lock simulation using sync.Map (can share the same store)
func (m *mockCacheClient) AcquireLock(ctx context.Context, lockKey string, expiration time.Duration) (bool, error) {
	// Prefix lock keys to avoid clashes with model/query keys if necessary
	actualLockKey := "lock:" + lockKey
	_, loaded := m.store.LoadOrStore(actualLockKey, time.Now()) // Store something simple for locks
	return !loaded, nil                                         // Acquired if it wasn't already loaded
}

func (m *mockCacheClient) ReleaseLock(ctx context.Context, lockKey string) error {
	actualLockKey := "lock:" + lockKey
	m.store.Delete(actualLockKey)
	return nil
}

// Remove old global adapter/client vars if they existed
// var testDbAdapter thing.DBAdapter
// var testCacheClient thing.CacheClient

// Keep track of the DBAdapter and CacheClient for creating Thing instances
var (
	globalTestDbAdapter   thing.DBAdapter
	globalTestCacheClient thing.CacheClient
	setupOnce             sync.Once
	seededUserID          int64 = 1 // Store the ID of the initially seeded user
)

// setupTestDB initializes an in-memory SQLite DB and configures the thing package.
func setupTestDB(tb testing.TB) {
	setupOnce.Do(func() {
		dsn := "file::memory:?cache=shared" // Use shared cache for in-memory persistence across connections
		// Use the constructor from the sqlite package
		dbAdapter, err := sqlite.NewSQLiteAdapter(dsn)
		if err != nil {
			tb.Fatalf("Failed to initialize SQLite adapter: %v", err)
		}
		globalTestDbAdapter = dbAdapter

		// Create tables IF NOT EXISTS
		createUsersSQL := `CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, email TEXT, created_at DATETIME, updated_at DATETIME);`
		createBooksSQL := `CREATE TABLE IF NOT EXISTS books (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT, user_id INTEGER, created_at DATETIME, updated_at DATETIME);`
		ctx := context.Background()
		_, err = dbAdapter.Exec(ctx, createUsersSQL)
		if err != nil {
			tb.Fatalf("Failed to create users table: %v", err)
		}
		_, err = dbAdapter.Exec(ctx, createBooksSQL)
		if err != nil {
			tb.Fatalf("Failed to create books table: %v", err)
		}

		// Configure the thing package globally
		testCacheClient := &mockCacheClient{}
		globalTestCacheClient = testCacheClient // Store cache client
		err = thing.Configure(dbAdapter, testCacheClient)
		if err != nil {
			tb.Fatalf("Failed to configure Thing package: %v", err)
		}

		// --- Seed initial data using the new API (using Save for creation) ---
		seedCtx := context.Background()
		// Replace New with Use, handle potential error
		userThing, err := thing.Use[User]()
		if err != nil {
			tb.Fatalf("Failed to get User Thing via Use: %v", err)
		}
		bookThing, err := thing.Use[Book]()
		if err != nil {
			tb.Fatalf("Failed to get Book Thing via Use: %v", err)
		}

		user1 := User{Name: "Seed User", Email: "seed@example.com"}
		// Use Save to create user1
		// saveInternal now returns only error, adjust call accordingly
		err = userThing.WithContext(seedCtx).Save(&user1)
		if err != nil {
			tb.Fatalf("Failed to seed user1 via Save: %v", err)
		}
		if user1.ID == 0 { // Check the ID on the original struct now
			tb.Fatalf("Seeding user1 via Save did not populate ID")
		}
		seededUserID = user1.ID // Store the actual ID
		log.Printf("Seeded User ID: %d", seededUserID)

		book1 := Book{Title: "Seed Book 1", UserID: seededUserID} // Use saved ID
		book2 := Book{Title: "Seed Book 2", UserID: seededUserID}

		// Use Save to create books
		err = bookThing.WithContext(seedCtx).Save(&book1)
		if err != nil {
			tb.Fatalf("Failed to seed book1 via Save: %v", err)
		}
		err = bookThing.WithContext(seedCtx).Save(&book2)
		if err != nil {
			tb.Fatalf("Failed to seed book2 via Save: %v", err)
		}

		log.Println("Test DB and Thing configuration setup complete.")
	})

	// Clear tables before each test function (optional, but good practice)
	// clearTables(tb, globalTestDbAdapter) // You might want a function like this
}

// clearTables helper (optional, if needed between tests)
// func clearTables(tb testing.TB, db thing.DBAdapter) {
//     ctx := context.Background()
//     _, err := db.Exec(ctx, "DELETE FROM books")
//     if err != nil { tb.Logf("Failed to clear books table: %v", err) }
//     _, err = db.Exec(ctx, "DELETE FROM users")
//      if err != nil { tb.Logf("Failed to clear users table: %v", err) }
//     // Reset autoincrement (SQLite specific)
//     _, err = db.Exec(ctx, "DELETE FROM sqlite_sequence WHERE name='users' OR name='books'")
//      if err != nil { tb.Logf("Failed to clear sqlite_sequence: %v", err) }
//     log.Println("Cleared test tables.")
// }

// TestMain allows setup and teardown for the package.
func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()
	// Teardown: Close DB connection
	if globalTestDbAdapter != nil {
		if err := globalTestDbAdapter.Close(); err != nil {
			log.Printf("Error closing test DB adapter: %v", err)
		}
	}
	os.Exit(code)
}

// --- Test Functions (Updated) ---

func TestThing_ByID_Found(t *testing.T) {
	setupTestDB(t)
	ctx := context.Background()
	userThing, err := thing.Use[User]()
	if err != nil {
		t.Fatalf("Failed to get User Thing via Use: %v", err)
	}

	user, err := userThing.WithContext(ctx).ByID(seededUserID)

	if err != nil {
		t.Fatalf("ByID(%d) resulted in unexpected error: %v", seededUserID, err)
	}
	if user == nil {
		t.Fatalf("ByID(%d) returned nil user", seededUserID)
	}
	if user.ID != seededUserID {
		t.Errorf("Expected user ID %d, got %d", seededUserID, user.ID)
	}
	if user.Name != "Seed User" {
		t.Errorf("Expected user name 'Seed User', got '%s'", user.Name)
	}
	if user.IsNewRecord() {
		t.Error("Fetched user should not be marked as new")
	}
	log.Printf("TestByID_Found: OK %+v", user)
}

func TestThing_ByID_NotFound(t *testing.T) {
	setupTestDB(t)
	ctx := context.Background()
	userThing, err := thing.Use[User]()
	if err != nil {
		t.Fatalf("Failed to get User Thing via Use: %v", err)
	}

	nonExistentID := int64(99999)
	user, err := userThing.WithContext(ctx).ByID(nonExistentID)

	if !errors.Is(err, thing.ErrNotFound) {
		t.Fatalf("Expected ErrNotFound for ID %d, got %v", nonExistentID, err)
	}
	if user != nil {
		t.Fatalf("Expected nil user for non-existent ID, got %+v", user)
	}
	log.Printf("TestByID_NotFound: OK")
}

func TestThing_Save_Create(t *testing.T) {
	setupTestDB(t)
	ctx := context.Background()
	userThing, err := thing.Use[User]()
	if err != nil {
		t.Fatalf("Failed to get User Thing via Use: %v", err)
	}

	newUser := User{Name: "New Create", Email: "create@example.com"}

	// Save the new user
	err = userThing.WithContext(ctx).Save(&newUser)
	if err != nil {
		t.Fatalf("Save (create) failed: %v", err)
	}

	// Verify ID and timestamps are set on the original struct
	if newUser.ID == 0 {
		t.Errorf("Expected non-zero ID after Save (create), got 0")
	}
	if newUser.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set after Save (create)")
	}
	if newUser.UpdatedAt.IsZero() {
		t.Error("Expected UpdatedAt to be set after Save (create)")
	}
	if newUser.IsNewRecord() {
		t.Error("Expected IsNewRecord to be false after Save (create)")
	}

	// Verify persistence by fetching
	fetchedUser, err := userThing.WithContext(ctx).ByID(newUser.ID)
	if err != nil {
		t.Fatalf("Failed to fetch user created via Save: %v", err)
	}
	if fetchedUser == nil {
		t.Fatalf("Fetched user created via Save is nil")
	}
	if fetchedUser.Name != "New Create" {
		t.Errorf("Fetched name mismatch: expected 'New Create', got '%s'", fetchedUser.Name)
	}
	if fetchedUser.Email != "create@example.com" {
		t.Errorf("Fetched email mismatch: expected 'create@example.com', got '%s'", fetchedUser.Email)
	}
	log.Printf("TestSave_Create: OK %+v", newUser)
}

func TestThing_Save_Update(t *testing.T) {
	setupTestDB(t)
	ctx := context.Background()
	userThing, err := thing.Use[User]()
	if err != nil {
		t.Fatalf("Failed to get User Thing via Use: %v", err)
	}

	// 1. Fetch the seeded user to update
	userToUpdate, err := userThing.WithContext(ctx).ByID(seededUserID)
	if err != nil {
		t.Fatalf("Setup for Save (update) failed: cannot fetch user %d: %v", seededUserID, err)
	}
	originalUpdatedAt := userToUpdate.UpdatedAt

	// Ensure some time passes for UpdatedAt comparison
	time.Sleep(10 * time.Millisecond)

	// 2. Modify and Save
	updatedEmail := "seed.updated@example.com"
	userToUpdate.Email = updatedEmail
	err = userThing.WithContext(ctx).Save(userToUpdate) // Pass pointer
	if err != nil {
		t.Fatalf("Save (update) failed: %v", err)
	}

	// Verify UpdatedAt changed on the original pointer
	if userToUpdate.UpdatedAt.Equal(originalUpdatedAt) {
		t.Errorf("Expected UpdatedAt timestamp on original struct to change after update. Original: %s, New: %s", originalUpdatedAt, userToUpdate.UpdatedAt)
	}

	// 3. Verify persistence by fetching again
	fetchedUser, err := userThing.WithContext(ctx).ByID(seededUserID)
	if err != nil {
		t.Fatalf("Failed to fetch user after Save (update): %v", err)
	}
	if fetchedUser.Email != updatedEmail {
		t.Errorf("Expected updated email '%s', got '%s'", updatedEmail, fetchedUser.Email)
	}
	if fetchedUser.UpdatedAt.Equal(originalUpdatedAt) {
		t.Errorf("Expected fetched UpdatedAt timestamp to change after update. Original: %s, Fetched: %s", originalUpdatedAt, fetchedUser.UpdatedAt)
	}
	// Compare fetched time with the time on the updated struct
	if !fetchedUser.UpdatedAt.Equal(userToUpdate.UpdatedAt) {
		t.Errorf("Fetched UpdatedAt (%s) does not match UpdatedAt on saved struct (%s)", fetchedUser.UpdatedAt, userToUpdate.UpdatedAt)
	}

	log.Printf("TestSave_Update: OK %+v", fetchedUser)
}

func TestThing_Delete(t *testing.T) {
	setupTestDB(t)
	ctx := context.Background()
	userThing, err := thing.Use[User]()
	if err != nil {
		t.Fatalf("Failed to get User Thing via Use: %v", err)
	}

	// Create a user specifically for this test
	userToDelete := User{Name: "Delete Me", Email: "delete@example.com"}
	err = userThing.WithContext(ctx).Save(&userToDelete)
	if err != nil {
		t.Fatalf("Failed to create user for deletion test: %v", err)
	}
	deleteID := userToDelete.ID
	log.Printf("TestDelete: Created user %d to delete", deleteID)

	// 2. Delete the user
	err = userThing.WithContext(ctx).Delete(&userToDelete)
	if err != nil {
		t.Fatalf("Delete failed for user ID %d: %v", deleteID, err)
	}

	// 3. Verify deletion
	log.Printf("Verifying deletion of user ID %d...", deleteID)
	_, err = userThing.WithContext(ctx).ByID(deleteID)
	if !errors.Is(err, thing.ErrNotFound) {
		t.Errorf("Expected ErrNotFound after delete, got %v", err)
	}

	fmt.Printf("Successfully tested Delete for user ID %d\n", deleteID)
}

func TestThing_Query(t *testing.T) {
	setupTestDB(t)
	ctx := context.Background()
	bookThing, err := thing.Use[Book]()
	if err != nil {
		t.Fatalf("Failed to get Book Thing via Use: %v", err)
	}

	// Query for books belonging to the seeded user
	params := thing.QueryParams{
		Where: "user_id = ?",
		Args:  []interface{}{seededUserID},
		Order: "title ASC", // Ensure deterministic order
	}
	loadedBooks, err := bookThing.WithContext(ctx).Query(params)

	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	expectedBookCount := 2
	if len(loadedBooks) != expectedBookCount {
		t.Fatalf("Expected %d books, got %d", expectedBookCount, len(loadedBooks))
	}

	// Verify content (assuming seeded books are "Seed Book 1", "Seed Book 2")
	if loadedBooks[0].Title != "Seed Book 1" {
		t.Errorf("Expected first book 'Seed Book 1', got '%s'", loadedBooks[0].Title)
	}
	if loadedBooks[0].UserID != seededUserID {
		t.Errorf("Expected first book UserID %d, got %d", seededUserID, loadedBooks[0].UserID)
	}
	if loadedBooks[1].Title != "Seed Book 2" {
		t.Errorf("Expected second book 'Seed Book 2', got '%s'", loadedBooks[1].Title)
	}
	if loadedBooks[1].UserID != seededUserID {
		t.Errorf("Expected second book UserID %d, got %d", seededUserID, loadedBooks[1].UserID)
	}

	log.Printf("TestQuery: OK (%d books)", len(loadedBooks))
}

func TestThing_IDs(t *testing.T) {
	setupTestDB(t)
	ctx := context.Background()
	bookThing, err := thing.Use[Book]()
	if err != nil {
		t.Fatalf("Failed to get Book Thing via Use: %v", err)
	}

	// Query for book IDs belonging to the seeded user
	params := thing.QueryParams{
		Where: "user_id = ?",
		Args:  []interface{}{seededUserID},
		Order: "id ASC", // Consistent ordering is important
	}
	ids, err := bookThing.WithContext(ctx).IDs(params)

	if err != nil {
		t.Fatalf("IDs failed: %v", err)
	}

	expectedIDCount := 2
	if len(ids) != expectedIDCount {
		t.Fatalf("Expected %d IDs, got %d", expectedIDCount, len(ids))
	}

	// We don't know the exact IDs assigned by AUTOINCREMENT, but we expect 2.
	// A more robust test might fetch the books first, get their IDs, then compare.
	// For now, just checking the count is sufficient for this example.
	log.Printf("TestIDs: OK (%d IDs found)", len(ids))

	// Example of a more robust check (if needed):
	// books, _ := bookThing.WithContext(ctx).Query(params)
	// expectedIDs := []int64{}
	// for _, b := range books { expectedIDs = append(expectedIDs, b.ID) }
	// if !reflect.DeepEqual(ids, expectedIDs) {
	//     t.Errorf("Expected IDs %v, got %v", expectedIDs, ids)
	// }
}

// TestThing_Query_Cache verifies query cache interactions (using IDs method).
func TestThing_Query_Cache(t *testing.T) {
	setupTestDB(t)
	// Explicitly reset cache *for this test* to avoid state from setup seeding
	mockCache, ok := globalTestCacheClient.(*mockCacheClient)
	if !ok {
		t.Fatalf("Test setup error: globalTestCacheClient is not *mockCacheClient")
	}
	mockCache.Reset()

	ctx := context.Background()
	bookThing, err := thing.Use[Book]()
	if err != nil {
		t.Fatalf("Failed to get Book Thing via Use: %v", err)
	}

	// Define query params matching seeded data
	params := thing.QueryParams{
		Where: "user_id = ?",
		Args:  []interface{}{seededUserID},
		Order: "id ASC",
	}

	// Generate expected query cache key (needs access to internal helper ideally, replicate for test)
	// This replication is brittle; better if generateQueryCacheKey were exported or testable.
	paramsBytes, _ := json.Marshal(params)
	hasher := sha256.New()
	hasher.Write([]byte("books")) // Table name
	hasher.Write(paramsBytes)
	hash := hex.EncodeToString(hasher.Sum(nil))
	queryKey := fmt.Sprintf("query:books:%s", hash)

	// 1. Initial query - should be cache miss, then DB hit, then cache set
	t.Logf("Attempting first IDs query for key: %s...", queryKey)
	if mockCache.Exists(queryKey) {
		t.Errorf("Query cache key '%s' should NOT exist before first query", queryKey)
	}

	ids1, err := bookThing.WithContext(ctx).IDs(params)
	if err != nil {
		t.Fatalf("First IDs query failed: %v", err)
	}
	if len(ids1) != 2 { // Should match seeded data
		t.Fatalf("First IDs query returned %d IDs, expected 2", len(ids1))
	}

	if !mockCache.Exists(queryKey) {
		t.Errorf("Query cache key '%s' SHOULD exist after first query", queryKey)
	}
	// Verify cache content
	bytes, found := mockCache.GetValue(queryKey)
	if !found {
		t.Fatalf("Query cache key '%s' not found via GetValue after query", queryKey)
	}
	var cachedIDs []int64
	if err := json.Unmarshal(bytes, &cachedIDs); err != nil {
		t.Fatalf("Failed to unmarshal cached query IDs for key '%s': %v", queryKey, err)
	}
	if len(cachedIDs) != len(ids1) { // Simple comparison for this test
		t.Errorf("Cached query IDs length mismatch. Expected %d, Got %d", len(ids1), len(cachedIDs))
	}
	t.Logf("First query successful, query cache populated for key: %s", queryKey)

	// 2. Second query - should be query cache hit
	t.Logf("Attempting second IDs query for key: %s...", queryKey)
	// --- How to verify DB wasn't hit? Deferred for now. ---
	ids2, err := bookThing.WithContext(ctx).IDs(params)
	if err != nil {
		t.Fatalf("Second IDs query failed: %v", err)
	}
	if len(ids2) != len(ids1) {
		t.Fatalf("Second IDs query returned %d IDs, expected %d", len(ids2), len(ids1))
	}

	t.Logf("TestQuery_Cache: OK")
}

// Helper function to access BaseModel field if GetBaseModelField is not exported
// func getBaseModelFieldHelper(modelPtr interface{}) (*thing.BaseModel, error) {
//     // Use reflection similar to the internal helper if needed
// }
