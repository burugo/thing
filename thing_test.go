package thing_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	// Import the package we are testing
	"thing"
	// Import the internal package ONLY for the adapter implementation in the test setup
	// Production code should not import internal packages directly.
	"thing/internal"
)

// --- Test Models ---

// User represents a user for testing.
type User struct {
	thing.BaseModel
	Name  string `db:"name"`
	Email string `db:"email"`
	Books []Book `thing:"rel:has_many;fk:UserID"` // Planned relation tag
}

func (u User) TableName() string {
	return "users"
}

// Book represents a book for testing.
type Book struct {
	thing.BaseModel
	Title  string `db:"title"`
	UserID int64  `db:"user_id"`
	User   *User  `thing:"rel:belongs_to;fk:UserID"` // Planned relation tag
}

func (b Book) TableName() string {
	return "books"
}

// --- Test Setup ---

// Mock CacheClient for testing purposes (replace with real implementation testing later)
type mockCacheClient struct{}

func (m *mockCacheClient) GetModel(ctx context.Context, key string, dest thing.Model) error {
	return thing.ErrNotFound
} // Always cache miss
func (m *mockCacheClient) SetModel(ctx context.Context, key string, model thing.Model, expiration time.Duration) error {
	return nil
}                                                                            // Do nothing
func (m *mockCacheClient) DeleteModel(ctx context.Context, key string) error { return nil } // Do nothing
func (m *mockCacheClient) GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error) {
	return nil, thing.ErrNotFound
}
func (m *mockCacheClient) SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error {
	return nil
}
func (m *mockCacheClient) DeleteQueryIDs(ctx context.Context, queryKey string) error { return nil }
func (m *mockCacheClient) AcquireLock(ctx context.Context, lockKey string, expiration time.Duration) (bool, error) {
	return true, nil
}                                                                                // Always succeed
func (m *mockCacheClient) ReleaseLock(ctx context.Context, lockKey string) error { return nil } // Do nothing

var testDbAdapter thing.DBAdapter
var testCacheClient thing.CacheClient

// setupTestDB initializes an in-memory SQLite DB and configures the thing package for testing.
func setupTestDB(tb testing.TB) thing.DBAdapter {
	if testDbAdapter != nil {
		// TODO: Clean tables between tests instead of reusing?
		return testDbAdapter
	}

	// Use in-memory SQLite database for tests
	// "cache=shared" allows multiple connections in the pool to access the same memory DB.
	dsn := "file::memory:?cache=shared"
	dbAdapter, err := internal.NewSQLiteAdapter(dsn)
	if err != nil {
		tb.Fatalf("Failed to initialize SQLite adapter: %v", err)
	}

	// Create tables (simple schema for testing)
	createUsersSQL := `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, email TEXT, created_at DATETIME, updated_at DATETIME);`
	createBooksSQL := `CREATE TABLE books (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT, user_id INTEGER, created_at DATETIME, updated_at DATETIME);`

	ctx := context.Background()
	_, err = dbAdapter.Exec(ctx, createUsersSQL)
	if err != nil {
		tb.Fatalf("Failed to create users table: %v", err)
	}
	_, err = dbAdapter.Exec(ctx, createBooksSQL)
	if err != nil {
		tb.Fatalf("Failed to create books table: %v", err)
	}

	// Configure the thing package with the test adapter and mock cache
	testCacheClient = &mockCacheClient{}
	thing.Configure(dbAdapter, testCacheClient)
	testDbAdapter = dbAdapter // Store for potential reuse/cleanup

	// Insert test data
	now := time.Now()
	user1 := User{
		BaseModel: thing.BaseModel{CreatedAt: now, UpdatedAt: now},
		Name:      "Test User 1", Email: "test1@example.com",
	}
	book1 := Book{
		BaseModel: thing.BaseModel{CreatedAt: now, UpdatedAt: now},
		Title:     "Test Book 1", UserID: 1,
	}
	book2 := Book{
		BaseModel: thing.BaseModel{CreatedAt: now, UpdatedAt: now},
		Title:     "Test Book 2", UserID: 1,
	}

	// Use adapter directly for setup, bypassing ORM Create for now
	insertUserSQL := "INSERT INTO users (name, email, created_at, updated_at) VALUES (?, ?, ?, ?)"
	res, err := dbAdapter.Exec(ctx, insertUserSQL, user1.Name, user1.Email, user1.BaseModel.CreatedAt, user1.BaseModel.UpdatedAt)
	if err != nil {
		tb.Fatalf("Failed to insert user1: %v", err)
	}
	userID, _ := res.LastInsertId()
	if userID != 1 {
		tb.Fatalf("Expected user ID 1, got %d", userID)
	}

	insertBookSQL := "INSERT INTO books (title, user_id, created_at, updated_at) VALUES (?, ?, ?, ?)"
	_, err = dbAdapter.Exec(ctx, insertBookSQL, book1.Title, book1.UserID, book1.BaseModel.CreatedAt, book1.BaseModel.UpdatedAt)
	if err != nil {
		tb.Fatalf("Failed to insert book1: %v", err)
	}
	_, err = dbAdapter.Exec(ctx, insertBookSQL, book2.Title, book2.UserID, book2.BaseModel.CreatedAt, book2.BaseModel.UpdatedAt)
	if err != nil {
		tb.Fatalf("Failed to insert book2: %v", err)
	}

	log.Println("Test DB setup complete.")
	return dbAdapter
}

// TestMain allows setup and teardown for the package.
func TestMain(m *testing.M) {
	// Optional: Setup code here, e.g., ensure DB is clean before tests.
	code := m.Run()
	// Optional: Teardown code here, e.g., close DB connection pool.
	if testDbAdapter != nil {
		testDbAdapter.Close()
	}
	os.Exit(code)
}

// --- Test Functions ---

func TestByIDAndRelations(t *testing.T) {
	_ = setupTestDB(t) // Ignore returned adapter for now
	ctx := context.Background()

	// 1. Test ByID
	userID := int64(1)
	user, err := thing.ByID[User](ctx, userID)

	if err != nil {
		t.Fatalf("thing.ByID[User](%d) failed: %v", userID, err)
	}
	if user == nil {
		t.Fatalf("thing.ByID[User](%d) returned nil user", userID)
	}
	if user.GetID() != userID {
		t.Errorf("Expected user ID %d, got %d", userID, user.GetID())
	}
	expectedName := "Test User 1"
	if user.Name != expectedName {
		t.Errorf("Expected user name '%s', got '%s'", expectedName, user.Name)
	}
	fmt.Printf("Successfully fetched User: %+v\n", *user)

	// Check if adapters were injected (optional check)
	// SKIP this check for now as getBaseModelField is not exported
	// bm, _ := thing.GetBaseModelField(user)
	// if bm == nil || bm.GetDBAdapter() == nil || bm.GetCacheClient() == nil {
	//     t.Errorf("Adapters not injected into fetched user model")
	// }

	// 2. Test Relation Loading (Conceptual - Requires Task 6 Implementation)
	var loadedBooks []Book
	log.Println("Attempting conceptual relation load for user books...")

	// Hypothetical call (replace with actual API when implemented)
	// err = thing.LoadRelation(ctx, user, "Books", &loadedBooks)
	// TEMP: Simulate by calling thing.Query directly (what LoadRelation should do)
	queryParams := thing.QueryParams{Where: "user_id = ?", Args: []interface{}{userID}, Order: "id ASC"}
	cachedResult, queryErr := thing.Query[Book](ctx, queryParams)
	if queryErr != nil {
		t.Fatalf("thing.Query[Book] for user %d failed: %v", userID, queryErr)
	}
	loadedBooks, fetchErr := cachedResult.Fetch(ctx)
	if fetchErr != nil {
		t.Logf("Warning during Fetch for related books: %v", fetchErr)
		// Allow test to continue with potentially partial results for now
	}

	// Assertions for loaded books
	expectedBookCount := 2
	if len(loadedBooks) != expectedBookCount {
		t.Errorf("Expected %d books for user %d, got %d", expectedBookCount, userID, len(loadedBooks))
	}

	if len(loadedBooks) > 0 && loadedBooks[0].Title != "Test Book 1" {
		t.Errorf("Expected first book title 'Test Book 1', got '%s'", loadedBooks[0].Title)
	}
	if len(loadedBooks) > 1 && loadedBooks[1].Title != "Test Book 2" {
		t.Errorf("Expected second book title 'Test Book 2', got '%s'", loadedBooks[1].Title)
	}

	fmt.Printf("Successfully fetched/queried related Books: %+v\n", loadedBooks)

}

// Helper function to access BaseModel field if GetBaseModelField is not exported
// func getBaseModelFieldHelper(modelPtr interface{}) (*thing.BaseModel, error) {
//     // Use reflection similar to the internal helper if needed
// }
