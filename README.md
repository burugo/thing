[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.24-blue.svg)](https://golang.org/doc/go1.24) \
[![Go Report Card](https://goreportcard.com/badge/github.com/burugo/thing)](https://goreportcard.com/report/github.com/burugo/thing) \
[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

# Thing ORM: High-Performance Go ORM with Built-in Caching

**Thing ORM** is a high-performance, open-source Object-Relational Mapper for Go, designed for modern application needs:

- **Unique Integrated Caching:** Thing ORM is the only Go ORM with first-class, built-in support for both Redis and in-memory caching. It automatically caches single-entity and list queries, manages cache invalidation, and provides cache hit/miss monitoring—out of the box. No third-party plugins or manual cache wiring required.
- **Multi-Database Support:** Effortlessly switch between MySQL, PostgreSQL, and SQLite with a unified API and automatic SQL dialect adaptation.
- **Simple, Efficient CRUD and List Queries:** Focused on the most common application patterns—thread-safe Create, Read, Update, Delete, and efficient list retrieval with filtering, ordering, and pagination.
- **Focused API:** Designed for fast CRUD and list operations. Complex SQL features like JOINs are out of the ORM's direct scope, but raw SQL execution is supported.
- **Elegant, Developer-Friendly API:** Clean, extensible, and idiomatic Go API, with flexible JSON serialization, relationship management, and hooks/events system.
- **Open Source and Community-Ready:** Well-documented, thoroughly tested, and designed for easy adoption and contribution by the Go community.

> **Why Thing ORM? (Unique Caching)**
>
> Unlike other Go ORMs, Thing ORM natively integrates caching at its core. All queries and entity fetches are automatically cached and invalidated as needed, with zero configuration. This delivers significant performance gains and reduces database load—no other Go ORM offers this level of built-in, transparent caching.

### Transparent Caching in Action

Fetching the same entity multiple times automatically utilizes the cache after the first database hit.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/burugo/thing"
)

// Assuming User model and thing.Configure() are already set up
// like in the Basic CRUD Example

func demonstrateCaching() {
	users, err := thing.Use[*User]()
	if err != nil {
		log.Fatal("Failed to get User ORM:", err)
	}

	// 1. Create a user (or ensure one exists with ID 1)
	u := &User{Name: "Cached User", Age: 50}
	u.ID = 1 // Assuming ID 1 for simplicity, ensure it exists
	if err := users.Save(u); err != nil {
		// Handle potential error if saving fails (e.g., unique constraints)
		log.Println("Note: Failed to save user ID 1, assuming it exists:", err)
	}

	// 2. First fetch - potentially hits the database
	fmt.Println("\nFetching user ID 1 for the first time...")
	user1, err := users.ByID(1)
	if err != nil {
		log.Fatal("Failed to fetch user ID 1 (first time):", err)
	}
	fmt.Println("First fetch result:", user1.Name)
	// You can check cache stats here to see a potential miss
	cacheClient := users.Cache() // Get the cache client
	stats1 := cacheClient.GetCacheStats(context.Background()) // Use appropriate context
	fmt.Printf("Stats after first fetch: GetModel=%d, GetModelMiss=%d\n",
		stats1.Counters["GetModel"], stats1.Counters["GetModelMiss"])

	// 3. Second fetch - should hit the cache (if caching is enabled)
	fmt.Println("\nFetching user ID 1 for the second time...")
	user2, err := users.ByID(1)
	if err != nil {
		log.Fatal("Failed to fetch user ID 1 (second time):", err)
	}
	fmt.Println("Second fetch result:", user2.Name)
	// Cache stats should show a hit here (e.g., GetModel counter increased,
	// GetModelMiss counter remains the same as after the first fetch).
	stats2 := cacheClient.GetCacheStats(context.Background())
	fmt.Printf("Stats after second fetch: GetModel=%d, GetModelMiss=%d\n",
		stats2.Counters["GetModel"], stats2.Counters["GetModelMiss"])

	// Subsequent fetches for ID 1 will also hit the cache until invalidated.
}

// In main(), call demonstrateCaching() after AutoMigrate

> **Thing ORM** is ideal for projects that need fast, reliable, and maintainable data access without the overhead of a full-featured SQL builder. It empowers developers to build scalable applications with minimal boilerplate and maximum control over caching and serialization.

## Installation

```bash
go get github.com/burugo/thing
```

## Basic CRUD Example

Here is a minimal example demonstrating how to use Thing ORM for basic CRUD operations:

```go
package main

import (
	"fmt"
	"log"

	"github.com/burugo/thing/internal/types"

	"github.com/burugo/thing"
)

// User model definition
// Only basic fields for demonstration
// No relationships

type User struct {
	thing.BaseModel
	Name string `db:"name"`
	Age  int    `db:"age"`
}

func main() {

	// Configure Thing ORM (in-memory DB and cache for demo)
	thing.Configure()

	// Auto-migrate User table
	if err := thing.AutoMigrate(&User{}); err != nil {
		log.Fatal(err)
	}

	// Get the User ORM object
	users, err := thing.Use[*User]()

	// Create
	u := &User{Name: "Alice", Age: 30}
	if err := users.Save(u); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Created:", u)

	// ByID
	found, err := users.ByID(u.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("ByID:", found)

	// Update
	found.Age = 31
	if err := users.Save(found); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Updated:", found)

	// Query all
	result, err := users.Query(types.QueryParams{})
	if err != nil {
		log.Fatal(err)
	}
	all, err := result.All()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("All users:", all)

	// Delete
	if err := users.Delete(u); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Deleted user.")
}
```

## Flexible JSON Serialization (Key Feature)

- **Supports Go struct tag**  
  Use standard `json:"name,omitempty"`, `json:"-"` tags for default serialization rules, fully compatible with `encoding/json`.
- **Dynamic field control (DSL/WithFields)**  
  Specify included/excluded fields, nested relationships, and output order at runtime using a simple DSL string or `WithFields` API.
- **Clear priority**  
  **Struct tag rules (e.g., `json:"-"`, `json:"name"`) always take precedence over DSL/WithFields.** If a struct tag excludes a field (e.g., `json:"-"`), it will never be output, even if the DSL includes it. DSL/WithFields dynamic rules control output order and inclusion/exclusion for all fields allowed by struct tags. Struct tags provide the ultimate allow/deny list; DSL/WithFields provides dynamic, ordered, and nested control within those constraints.
- **Order guaranteed**  
  Output JSON field order strictly follows the DSL, meeting frontend/API spec requirements.
- **Recursive nested support**  
  Control nested objects/relationship fields recursively, including their output order and content.
- **No struct tag required**  
  Even without any `json` struct tags, you can serialize to JSON with full field control and ordering. Struct tags are optional and only needed for default rules or special cases.

### Example

// Model definition (with struct tags, optional)
type User struct {
    ID   int    `json:"id"`
    Name string `json:"name,omitempty"`
    // ...
}

// Model definition (no struct tags)
type SimpleUser struct {
    ID   int
    Name string
    // ...
}

// Default serialization follows struct tag (if present)
json.Marshal(user)

// Flexible, ordered, nested output (works with or without struct tags)
// Note: ToJSON is called on a Thing[*User] or Thing[*SimpleUser] instance (e.g., userThing)
userThing.ToJSON(WithFieldsDSL("name,profile{avatar},-id"))
simpleUserThing.ToJSON(WithFieldsDSL("name,-id"))

## Relationship Management (HasMany, BelongsTo)

Thing ORM supports basic relationship management, including preloading related models.

### 1. Define Relationships in Models

Use `thing` struct tags to define relationships. The `db:"-"` tag prevents the ORM from treating the relationship field as a database column.

```go
package models // assuming models are in a separate package

import "github.com/burugo/thing"

// User has many Books
type User struct {
	thing.BaseModel
	Name  string `db:"name"`
	Email string `db:"email"`
	// Define HasMany relationship:
	// - fk: Foreign key in the 'Book' table (user_id)
	// - model: Name of the related model struct (Book)
	Books []*Book `thing:"hasMany;fk:user_id;model:Book" db:"-"`
}

func (u *User) TableName() string { return "users" }

// Book belongs to a User
type Book struct {
	thing.BaseModel
	Title  string `db:"title"`
	UserID int64  `db:"user_id"` // Foreign key column
	// Define BelongsTo relationship:
	// - fk: Foreign key in the 'Book' table itself (user_id)
	User *User `thing:"belongsTo;fk:user_id" db:"-"`
}

func (b *Book) TableName() string { return "books" }
```

### 2. Preload Relationships in Queries

Use the `Preloads` field in `QueryParams` to specify relationships to eager-load.

```go
package main

import (
	"fmt"
	"log"

	"github.com/burugo/thing"
	"github.com/burugo/thing/internal/types"
	// import your models package e.g., "yourproject/models"
)

func main() {
	// Assume thing.Configure() and AutoMigrate(&models.User{}, &models.Book{}) are done
	// Assume user and books are created...

	userThing, _ := thing.Use[*models.User]()
	bookThing, _ := thing.Use[*models.Book]()

	// Example 1: Find a user and preload their books (HasMany)
	userParams := types.QueryParams{
		Where:    "id = ?",
		Args:     []interface{}{1}, // Assuming user with ID 1 exists
		Preloads: []string{"Books"}, // Specify the relationship field name
	}
	userResult, _ := userThing.Query(userParams)
	fetchedUsers, _ := userResult.Fetch(0, 1)
	if len(fetchedUsers) > 0 {
		fmt.Printf("User: %s, Number of Books: %d\n", fetchedUsers[0].Name, len(fetchedUsers[0].Books))
		// fetchedUsers[0].Books is now populated
	}

	// Example 2: Find a book and preload its user (BelongsTo)
	bookParams := types.QueryParams{
		Where:    "id = ?",
		Args:     []interface{}{5}, // Assuming book with ID 5 exists
		Preloads: []string{"User"}, // Specify the relationship field name
	}
	bookResult, _ := bookThing.Query(bookParams)
	fetchedBooks, _ := bookResult.Fetch(0, 1)
	if len(fetchedBooks) > 0 && fetchedBooks[0].User != nil {
		fmt.Printf("Book: %s, Owner: %s\n", fetchedBooks[0].Title, fetchedBooks[0].User.Name)
		// fetchedBooks[0].User is now populated
	}
}

```

Thing ORM automatically fetches the related models in an optimized way, utilizing the cache where possible.

## Raw SQL Execution

While Thing ORM focuses on common CRUD and list patterns, you can always drop down to raw SQL for complex queries or operations not directly supported by the ORM API. You can access the underlying `DBAdapter` or even the standard `*sql.DB` connection.

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/burugo/thing"
	// import your models package
)

func rawSQLExample() {
	// Assume thing.Configure() is done and you have a User model
	userThing, _ := thing.Use[*models.User]()

	// Get the underlying DBAdapter
	dbAdapter := userThing.DBAdapter()
	ctx := context.Background() // Or your request context

	// 1. Execute a raw SQL command (e.g., UPDATE)
	updateQuery := "UPDATE users SET email = ? WHERE id = ?"
	result, err := dbAdapter.Exec(ctx, updateQuery, "new.raw.email@example.com", 1)
	if err != nil {
		log.Fatalf("Raw Exec failed: %v", err)
	}
	rowsAffected, _ := result.RowsAffected()
	fmt.Printf("Raw Exec: Rows affected: %d\n", rowsAffected)

	// 2. Query a single row and scan into a struct (using Adapter.Get)
	var user models.User
	selectQuery := "SELECT id, name, email FROM users WHERE id = ?"
	err = dbAdapter.Get(ctx, &user, selectQuery, 1)
	if err != nil {
		if err == sql.ErrNoRows {
			fmt.Println("Raw Get: User not found")
		} else {
			log.Fatalf("Raw Get failed: %v", err)
		}
	} else {
		fmt.Printf("Raw Get: User found: ID=%d, Name=%s\n", user.ID, user.Name)
	}

	// 3. Query multiple rows (using Adapter.Select)
	var names []string
	selectMultiple := "SELECT name FROM users WHERE email LIKE ? ORDER BY name"
	err = dbAdapter.Select(ctx, &names, selectMultiple, "%@example.com")
	if err != nil {
		log.Fatalf("Raw Select failed: %v", err)
	}
	fmt.Printf("Raw Select: Found %d names ending in @example.com: %v\n", len(names), names)

	// 4. Access the standard *sql.DB (for things not covered by adapter)
	// sqlDB := dbAdapter.DB()
	// // Use sqlDB for standard database/sql operations if needed
}

// In main(), call rawSQLExample()

## Method-based Virtual Properties (Advanced JSON Serialization)

You can define computed (virtual) fields on your model by adding exported, zero-argument, single-return-value methods. These methods will only be included in the JSON output if you explicitly reference their corresponding field name in the DSL string passed to `ToJSON`.

- **Method Naming:** Use Go's exported method naming (e.g., `FullName`). The field name in the DSL should be the snake_case version (e.g., `full_name`).
- **How it works:**
    - If the DSL includes a field name that matches a method (converted to snake_case), the method will be called and its return value included in the output.
    - If the DSL does not mention the virtual field, it will not be output.

**Example:**

```go
// Model definition
 type User struct {
     FirstName string
     LastName  string
 }

 // Virtual property method
 func (u *User) FullName() string {
     return u.FirstName + " " + u.LastName
 }

 user := &User{FirstName: "Alice", LastName: "Smith"}
 jsonBytes, _ := thing.ToJSON(user, thing.WithFields("first_name,full_name"))
 fmt.Println(string(jsonBytes))
 // Output: {"first_name":"Alice","full_name":"Alice Smith"}
```

- If you omit `full_name` from the DSL, the `FullName()` method will not be called or included in the output.

This approach gives you full control over which computed fields are exposed, and ensures only explicitly requested virtuals are included in the JSON output.

## Simple Field Inclusion: `Include(fields ...string)`

For most use cases, you can use the `Include` function to specify exactly which fields to output, in order, using plain Go string arguments:

```go
thing.ToJSON(user, thing.Include("id", "name", "full_name"))
```

- This is equivalent to `WithFields("id,name,full_name")` but is more Go-idiomatic and avoids DSL syntax for simple cases.
- You can use this to output method-based virtuals as well:

```go
thing.ToJSON(user, thing.Include("first_name", "full_name"))
```

- If you need nested, exclude, or advanced DSL features, use `WithFields`:

```go
thing.ToJSON(user, thing.WithFields("name,profile{avatar},-id"))
```

- Both `Include` and `WithFields` are fully compatible with struct tags and method-based virtuals as described above.

## Cache Monitoring & Hit/Miss Statistics

Thing ORM provides built-in cache operation monitoring for all cache clients (including Redis and the mock client used in tests). Remember, this monitoring capability is a core, integrated feature, not an add-on.

### How to Use

You can call `GetCacheStats(ctx)` on any `CacheClient` instance to retrieve a snapshot of cache operation counters. The returned `CacheStats.Counters` map includes keys such as:

- `Get`: total number of Get calls
- `GetMiss`: number of Get calls that missed (not found)
- `GetModel`, `GetModelMiss`: same for model cache
- `GetQueryIDs`, `GetQueryIDsMiss`: same for query ID list cache

**To compute hit count and hit rate:**

- `hit = total - miss`
- `hit rate = hit / total`

### Example

```go
stats := cacheClient.GetCacheStats(ctx)
getTotal := stats.Counters["Get"]
getMiss := stats.Counters["GetMiss"]
getHit := getTotal - getMiss
hitRate := float64(getHit) / float64(getTotal)
fmt.Printf("Get hit rate: %.2f%%\n", hitRate*100)
```

This mechanism allows you to monitor cache effectiveness, debug performance issues, and tune your caching strategy.

**Note:** All counters are thread-safe and represent the state since the cache client was created or last reset. This built-in monitoring applies automatically to any cache backend you use (Redis, in-memory, etc.).

## Schema/Migration Tools

### Usage Overview

- Use `thing.AutoMigrate` to automatically generate and execute CREATE TABLE SQL, adapting to the current database dialect (MySQL/PostgreSQL/SQLite).
- Supports declaring normal and unique indexes via struct tags.

### Index Declaration

- Normal index: add tag `thing:"index"` to the struct field
- Unique index: add tag `thing:"unique"` to the struct field

```go
// Example
 type User struct {
     ID    int64  `db:"id,pk"`
     Name  string `db:"name" thing:"index"`
     Email string `db:"email" thing:"unique"`
 }
```

### Auto Migration Example

```go
import "github.com/burugo/thing"

// Configure the database adapter
thing.Configure(dbAdapter, cacheClient)

// Auto create tables (including indexes)
err := thing.AutoMigrate(&User{})
if err != nil {
    panic(err)
}
```

- During migration, CREATE TABLE and CREATE INDEX/UNIQUE INDEX statements are automatically generated.
- Supports batch migration of multiple models: `thing.AutoMigrate(&User{}, &Book{})`

## Quick Start: Initialization and Cache Selection

You can initialize a Thing ORM instance with flexible cache options:

### 1. Use Default Local (In-Memory) Cache (No Redis Required)
```go
userThing, err := thing.New[*User](db,nil) // Uses built-in localCache automatically
```
- Suitable for development, testing, or single-node deployments.
- No external cache dependency.

### 2. Use External Cache (e.g., Redis)
```go
userThing, err := thing.New[*User](db, redisCache)
```
- Pass any implementation of `thing.CacheClient` (e.g., Redis, Memcached).
- Recommended for production/distributed deployments.

> **Tip:** You can always pass `nil` as the cache argument to force fallback to localCache:
> ```go
> userThing, err := thing.New[*User](db, nil) // Same as omitting cache
> ```



