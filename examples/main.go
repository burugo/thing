package main

import (
	"context"
	"fmt"
	"log"
	"time"

	// Import core package (now at module root)
	"thing"
	// Import drivers package
	"thing/drivers"
)

// --- Example Models (could be in a separate 'models' package) ---

type User struct {
	thing.BaseModel
	Name  string `db:"name"`
	Email string `db:"email"`
}

func (u *User) TableName() string {
	return "users"
}

type Book struct {
	thing.BaseModel
	Title  string `db:"title"`
	UserID int64  `db:"user_id"`
}

func (b *Book) TableName() string {
	return "books"
}

// --- Mock Cache (for example simplicity) ---

type mockCache struct{}

func (m *mockCache) GetModel(ctx context.Context, key string, dest interface{}) error {
	return thing.ErrNotFound
}
func (m *mockCache) SetModel(ctx context.Context, key string, model interface{}, expiration time.Duration) error {
	return nil
}
func (m *mockCache) DeleteModel(ctx context.Context, key string) error { return nil }
func (m *mockCache) GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error) {
	return nil, thing.ErrNotFound
}
func (m *mockCache) SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error {
	return nil
}
func (m *mockCache) DeleteQueryIDs(ctx context.Context, queryKey string) error { return nil }
func (m *mockCache) AcquireLock(ctx context.Context, lockKey string, expiration time.Duration) (bool, error) {
	return true, nil
}
func (m *mockCache) ReleaseLock(ctx context.Context, lockKey string) error { return nil }

func main() {
	log.Println("--- Thing Example --- S")

	// 1. Initialization (typically done once at startup)
	// Use SQLite adapter from the drivers package
	db, err := drivers.NewSQLiteAdapter(":memory:") // Use in-memory SQLite for example
	if err != nil {
		log.Fatalf("Failed to create DB adapter: %v", err)
	}
	defer db.Close()

	cache := &mockCache{}

	// Create necessary tables (in a real app, use migrations)
	ctx := context.Background()
	_, err = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, email TEXT, created_at DATETIME, updated_at DATETIME);`)
	if err != nil {
		log.Fatalf("Failed to create users table: %v", err)
	}
	_, err = db.Exec(ctx, `CREATE TABLE books (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT, user_id INTEGER, created_at DATETIME, updated_at DATETIME);`)
	if err != nil {
		log.Fatalf("Failed to create books table: %v", err)
	}

	// Configure the thing package globally
	if err := thing.Configure(db, cache); err != nil {
		log.Fatalf("Failed to configure Thing: %v", err)
	}

	// 2. Get Thing instances using Use[T]()
	// No need to pass db, cache here!
	userRepo, err := thing.Use[User]()
	if err != nil {
		log.Fatalf("Failed to get User repo: %v", err)
	}

	bookRepo, err := thing.Use[Book]()
	if err != nil {
		log.Fatalf("Failed to get Book repo: %v", err)
	}

	// 3. Use the Repositories
	log.Println("Creating user...")
	newUser := User{Name: "Example User", Email: "user@example.com"}
	if err := userRepo.Save(&newUser); err != nil {
		log.Fatalf("Failed to save new user: %v", err)
	}
	log.Printf("User created with ID: %d", newUser.ID)

	log.Println("Creating books...")
	book1 := Book{Title: "Go Programming", UserID: newUser.ID}
	book2 := Book{Title: "Learning Concurrency", UserID: newUser.ID}
	if err := bookRepo.Save(&book1); err != nil {
		log.Fatalf("Failed to save book1: %v", err)
	}
	if err := bookRepo.Save(&book2); err != nil {
		log.Fatalf("Failed to save book2: %v", err)
	}
	log.Printf("Books created with IDs: %d, %d", book1.ID, book2.ID)

	log.Println("Querying books for user...")
	userBooks, err := bookRepo.Query(thing.QueryParams{
		Where: "user_id = ?",
		Args:  []interface{}{newUser.ID},
	})
	if err != nil {
		log.Fatalf("Failed to query books: %v", err)
	}

	fmt.Printf("Found %d books for user %d:\n", len(userBooks), newUser.ID)
	for _, book := range userBooks {
		fmt.Printf("  - ID: %d, Title: %s\n", book.ID, book.Title)
	}

	log.Println("--- Thing Example End ---")
}
