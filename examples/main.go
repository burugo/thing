package main

import (
	"context"
	"fmt"
	"log"

	"thing"
	// Driver import removed from here, Wire handles it
	// "thing/drivers"
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

// --- Mock Cache Removed ---

// --- Application Struct ---
// This struct holds the dependencies we want Wire to inject.

type Application struct {
	UserRepo *thing.Thing[User]
	BookRepo *thing.Thing[Book]
	// Add DBAdapter to the struct so Wire injects it
	DB thing.DBAdapter
	// Add other dependencies here (e.g., services, http handlers)
}

func main() {
	log.Println("--- Thing Example with Wire --- S")

	// 1. Initialization using Wire
	dsn := ":memory:" // Use in-memory SQLite for example
	app, cleanup, err := initializeApplication(dsn)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer cleanup()

	// Create necessary tables using the injected DB adapter
	ctx := context.Background()
	_, err = app.DB.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, email TEXT, created_at DATETIME, updated_at DATETIME);`)
	if err != nil {
		log.Fatalf("Failed to create users table: %v", err)
	}
	_, err = app.DB.Exec(ctx, `CREATE TABLE books (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT, user_id INTEGER, created_at DATETIME, updated_at DATETIME);`)
	if err != nil {
		log.Fatalf("Failed to create books table: %v", err)
	}

	// 2. Use the injected repositories from the Application struct
	userRepo := app.UserRepo
	bookRepo := app.BookRepo

	// 3. Use the Repositories (same logic as before)
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

	// Fetch the results from CachedResult
	fetchedBooks, fetchErr := userBooks.Fetch(0, 10) // Fetch first 10 (adjust limit as needed)
	if fetchErr != nil {
		log.Fatalf("Failed to fetch books from CachedResult: %v", fetchErr)
	}

	fmt.Printf("Found %d books for user %d:\n", len(fetchedBooks), newUser.ID)
	for _, book := range fetchedBooks {
		fmt.Printf("  - ID: %d, Title: %s\n", book.ID, book.Title)
	}

	log.Println("--- Thing Example with Wire End ---")
}
