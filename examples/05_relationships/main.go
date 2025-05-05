package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/burugo/thing"
	"github.com/burugo/thing/drivers/db/sqlite"
)

// User model with HasMany Books and ManyToMany Roles
// Note: UserRole is the join table for ManyToMany

type User struct {
	thing.BaseModel
	Name  string
	Email string
}

type Book struct {
	thing.BaseModel
	Title  string
	UserID int64
}

type Role struct {
	thing.BaseModel
	Name string
}

type UserRole struct {
	thing.BaseModel
	UserID int64
	RoleID int64
}

func main() {
	ctx := context.Background()
	log.Println("--- Relationships Example ---")

	// --- Thing ORM Initialization Patterns ---
	// 1. Use default local (in-memory) cache (no Redis required):
	//    userThing, _ := thing.New[*User](db)
	// 2. Use external cache (e.g., Redis):
	//    userThing, _ := thing.New[*User](db, redisCache)
	// 3. Use multiple caches (future: load balancing/failover):
	//    userThing, _ := thing.New[*User](db, cache1, cache2)

	// --- DB Setup (SQLite in-memory) ---
	db, err := sqlite.NewSQLiteAdapter(":memory:")
	if err != nil {
		log.Fatalf("Failed to initialize DB: %v", err)
	}
	defer db.Close()

	// --- Create tables ---
	_, err = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, email TEXT, created_at DATETIME, updated_at DATETIME, deleted BOOLEAN DEFAULT FALSE);
	CREATE TABLE books (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT, user_id INTEGER, created_at DATETIME, updated_at DATETIME, deleted BOOLEAN DEFAULT FALSE);
	CREATE TABLE roles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, created_at DATETIME, updated_at DATETIME, deleted BOOLEAN DEFAULT FALSE);
	CREATE TABLE user_roles (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, role_id INTEGER, created_at DATETIME, updated_at DATETIME, deleted BOOLEAN DEFAULT FALSE);`)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	// --- ORM Setup (demonstrate in-memory cache fallback) ---
	userThing, _ := thing.New[*User](db, nil)
	bookThing, _ := thing.New[*Book](db, nil)
	roleThing, _ := thing.New[*Role](db, nil)
	userRoleThing, _ := thing.New[*UserRole](db, nil)

	// --- Insert sample data ---
	user := &User{Name: "Alice"}
	_ = userThing.Save(user)
	book1 := &Book{Title: "Go 101", UserID: user.ID}
	book2 := &Book{Title: "Advanced Go", UserID: user.ID}
	_ = bookThing.Save(book1)
	_ = bookThing.Save(book2)
	role1 := &Role{Name: "Admin"}
	role2 := &Role{Name: "Editor"}
	_ = roleThing.Save(role1)
	_ = roleThing.Save(role2)
	_ = userRoleThing.Save(&UserRole{UserID: user.ID, RoleID: role1.ID})
	_ = userRoleThing.Save(&UserRole{UserID: user.ID, RoleID: role2.ID})

	// --- Load HasMany (Books) ---
	books := bookThing.Query(thing.QueryParams{Where: "user_id = ?", Args: []interface{}{user.ID}})
	fetchedBooks, err := books.Fetch(0, 10)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("User's books: %+v\n", fetchedBooks)

	// --- Load ManyToMany (Roles via UserRole) ---
	userRoles := userRoleThing.Query(thing.QueryParams{Where: "user_id = ?", Args: []interface{}{user.ID}})
	fetchedUserRoles, err := userRoles.Fetch(0, 10)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("User's roles: %+v\n", fetchedUserRoles)

	roleIDs := make([]int64, 0, len(fetchedUserRoles))
	for _, ur := range fetchedUserRoles {
		roleIDs = append(roleIDs, ur.RoleID)
	}
	roles := roleThing.Query(thing.QueryParams{Where: "id IN (?)", Args: []interface{}{roleIDs}})
	fetchedRoles, err := roles.Fetch(0, 10)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Roles: %+v\n", fetchedRoles)

	// --- JSON Serialization Example ---
	b, _ := json.Marshal(user)
	log.Printf("User JSON: %s", string(b))

	log.Println("--- Relationships Example Finished ---")
}
