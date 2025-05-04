# Thing ORM: High-Performance Go ORM with Built-in Caching

[![Go Report Card](https://goreportcard.com/badge/github.com/burugo/thing)](https://goreportcard.com/report/github.com/burugo/thing)
[![Build Status](https://github.com/burugo/thing/actions/workflows/go.yml/badge.svg)](https://github.com/burugo/thing/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/burugo/thing.svg)](https://pkg.go.dev/github.com/burugo/thing)
[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.18-blue.svg)](https://golang.org/doc/go1.18)

**Thing ORM** is a high-performance, open-source Object-Relational Mapper for Go, designed for modern application needs:

- **Unique Integrated Caching:** Thing ORM is the only Go ORM with first-class, built-in support for both Redis and in-memory caching. It automatically caches single-entity and list queries, manages cache invalidation, and provides cache hit/miss monitoring—out of the box. No third-party plugins or manual cache wiring required.
- **Type-Safe Generics-Based API:** Built on Go generics, Thing ORM provides compile-time type safety, better performance, and a more intuitive, IDE-friendly API—unlike most Go ORMs that rely on reflection and runtime type checks.
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
```

*See [Caching & Monitoring](#caching--monitoring) for advanced cache statistics and monitoring.*

## Table of Contents

- [Installation](#installation)
- [Configuration](#configuration)
- [API Documentation](#api-documentation)
- [Basic CRUD Example](#basic-crud-example)
- [Flexible JSON Serialization](#flexible-json-serialization)
  - [Examples](#examples)
  - [Method-based Virtual Properties](#method-based-virtual-properties)
- [Relationship Management](#relationship-management)
  - [Defining Relationships](#defining-relationships)
  - [Preloading Related Models](#preloading-related-models)
- [Hooks & Events](#hooks--events)
  - [Available Events](#available-events)
  - [Registering Listeners](#registering-listeners)
  - [Example](#example)
- [Caching & Monitoring](#caching--monitoring)
  - [Cache Monitoring & Hit/Miss Statistics](#cache-monitoring--hitmiss-statistics)
- [Advanced Usage](#advanced-usage)
  - [Raw SQL Execution](#raw-sql-execution)
- [Schema/Migration Tools](#schemamigration-tools)
  - [Usage Overview](#usage-overview)
  - [Index Declaration](#index-declaration)
  - [Auto Migration Example](#auto-migration-example-1)
- [Contributing](#contributing)
- [Performance](#performance)
- [License](#license)

## Installation

```bash
go get github.com/burugo/thing
```

## Configuration

Thing ORM is configured by providing a database adapter (`DBAdapter`) and an optional cache client (`CacheClient`) when creating a `Thing` instance using `thing.New`. This allows for flexible setup tailored to your application's needs.

### 1. Choose a Database Adapter

First, create an instance of a database adapter for your chosen database (MySQL, PostgreSQL, or SQLite).

```go
import (
	"github.com/burugo/thing/drivers/db/mysql"
	"github.com/burugo/thing/drivers/db/postgres"
	"github.com/burugo/thing/drivers/db/sqlite"
	"github.com/burugo/thing/interfaces"
)

// Example: SQLite (replace ":memory:" with your file path)
dbAdapter, err := sqlite.NewSQLiteAdapter(":memory:")
if err != nil {
	log.Fatal("Failed to create SQLite adapter:", err)
}

// Example: MySQL (replace with your actual DSN)
// mysqlDSN := "user:password@tcp(127.0.0.1:3306)/database?parseTime=true"
// dbAdapter, err := mysql.NewMySQLAdapter(mysqlDSN)
// if err != nil {
// 	log.Fatal("Failed to create MySQL adapter:", err)
// }

// Example: PostgreSQL (replace with your actual DSN)
// pgDSN := "host=localhost user=user password=password dbname=database port=5432 sslmode=disable TimeZone=Asia/Shanghai"
// dbAdapter, err := postgres.NewPostgreSQLAdapter(pgDSN)
// if err != nil {
// 	log.Fatal("Failed to create PostgreSQL adapter:", err)
// }
```

### 2. Choose a Cache Client (Optional)

Thing ORM includes a built-in in-memory cache, which is used by default if no cache client is provided. For production or distributed systems, using Redis is recommended.

```go
import (
	"github.com/redis/go-redis/v9"
	redisCache "github.com/burugo/thing/drivers/cache/redis"
	"github.com/burugo/thing/interfaces"
)

// Option A: Use Default In-Memory Cache
// Simply pass nil as the cache client when calling thing.New
var cacheClient interfaces.CacheClient = nil

// Option B: Use Redis
// redisAddr := "localhost:6379"
// redisPassword := ""
// redisDB := 0
// rdb := redis.NewClient(&redis.Options{
// 	Addr:     redisAddr,
// 	Password: redisPassword,
// 	DB:       redisDB,
// })
// cacheClient = redisCache.NewClient(rdb) // Create Thing's Redis client wrapper

```

### 3. Create Thing Instance

Use `thing.New` to create an ORM instance for your specific model type, passing the chosen database adapter and cache client.

```go
import (
	"github.com/burugo/thing"
	// import your models package
)

// Create a Thing instance for the User model
userThing, err := thing.New[*models.User](dbAdapter, cacheClient)
if err != nil {
	log.Fatal("Failed to create Thing instance for User:", err)
}

// Now you can use userThing for CRUD, queries, etc.
// userThing.Save(...)
// userThing.ByID(...)
// userThing.Query(...)
```

### Global Configuration (Alternative)

For simpler applications or global setup, you can use `thing.Configure` once at startup and then `thing.Use` to get model-specific instances. **Note:** This uses global variables and is less flexible for managing multiple database/cache connections.

```go
// At application startup:
// err := thing.Configure(dbAdapter, cacheClient)
// if err != nil { ... }

// Later, in your code:
// userThing, err := thing.Use[*models.User]()
// if err != nil { ... }
```

## API Documentation

Full API documentation is available on [pkg.go.dev](https://pkg.go.dev/github.com/burugo/thing).

*(Note: The documentation link will become active after the first official release/tag of the package.)*

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

## Flexible JSON Serialization

Thing ORM provides multiple ways to control JSON output fields, order, and nesting:

- **Include(fields ...string):** Specify exactly which top-level fields to output, in order. Best for simple, flat cases.
- **Exclude(fields ...string):** Specify top-level fields to exclude from output. Can be combined with Include or used alone.
- **WithFields(dsl string):** Use a powerful DSL string to control inclusion, exclusion, order, and nested fields (e.g. `"name,profile{avatar},-id"`).

> **Note:**
> - `Include` and `Exclude` only support flat (top-level) fields.
> - For nested field control, use `WithFields` DSL.
> - You can combine `Include`, `Exclude`, and `WithFields`, but only `WithFields` supports nested and ordered field selection.

### Examples

```go
// Only include id, name, and full_name (method-based virtual)
thing.ToJSON(user, thing.Include("id", "name", "full_name"))

// Exclude sensitive fields
thing.ToJSON(user, thing.Exclude("password", "email"))

// Combine Include and Exclude (still only affects top-level fields)
thing.ToJSON(user, thing.Include("id", "name", "email"), thing.Exclude("email"))

// Use WithFields DSL for advanced/nested control
thing.ToJSON(user, thing.WithFields("name,profile{avatar},-id"))
```

- `WithFields` supports nested fields, exclusion (with `-field`), and output order.
- `Include`/`Exclude` are Go-idiomatic and best for simple, flat cases.
- Struct tags (e.g. `json:"-"`) always take precedence.

### Method-based Virtual Properties

You can define computed (virtual) fields on your model by adding exported, zero-argument, single-return-value methods. These methods will only be included in the JSON output if you explicitly reference their corresponding field name in the DSL string or Include option.

- **Method Naming:** Use Go's exported method naming (e.g., `FullName`). The field name in the DSL should be the snake_case version (e.g., `full_name`).
- **How it works:**
    - If the DSL or Include includes a field name that matches a method (converted to snake_case), the method will be called and its return value included in the output.
    - If the DSL/Include does not mention the virtual field, it will not be output.

**Example:**

```go
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

- If you omit `full_name` from the DSL or Include, the `FullName()` method will not be called or included in the output.

This approach gives you full control over which computed fields are exposed, and ensures only explicitly requested virtuals are included in the JSON output.

## Relationship Management

### Defining Relationships

Thing ORM supports basic relationship management, including preloading related models.

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

### Preloading Related Models

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

## Hooks & Events

Thing ORM provides a hook system that allows you to register functions (listeners) to be executed before or after specific database operations. This is useful for tasks like validation, logging, data modification, or triggering side effects.

### Available Events

- `EventTypeBeforeSave`: Before creating or updating a record.
- `EventTypeAfterSave`: After successfully creating or updating a record.
- `EventTypeBeforeCreate`: Before creating a new record (subset of BeforeSave).
- `EventTypeAfterCreate`: After successfully creating a new record.
- `EventTypeBeforeDelete`: Before hard deleting a record.
- `EventTypeAfterDelete`: After successfully hard deleting a record.
- `EventTypeBeforeSoftDelete`: Before soft deleting a record.
- `EventTypeAfterSoftDelete`: After successfully soft deleting a record.

### Registering Listeners

Use `thing.RegisterListener` to attach your hook function to an event type. The listener function receives the context, event type, the model instance, and optional event-specific data.

**Listener Signature:**

```go
func(ctx context.Context, eventType thing.EventType, model interface{}, eventData interface{}) error
```

- **Returning an error** from a `Before*` hook will abort the database operation.
- `eventData` for `EventTypeAfterSave` contains a `map[string]interface{}` of changed fields.

### Example

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/burugo/thing"
	// Assume User model is defined
)

// Example Hook: Validate email before saving
func validateEmailHook(ctx context.Context, eventType thing.EventType, model interface{}, eventData interface{}) error {
	if user, ok := model.(*User); ok { // Type assert to your model
		log.Printf("[HOOK %s] Checking user: %s, Email: %s", eventType, user.Name, user.Email)
		if user.Email == "invalid@example.com" {
			return errors.New("invalid email provided")
		}
	}
	return nil
}

// Example Hook: Log after creation
func logAfterCreateHook(ctx context.Context, eventType thing.EventType, model interface{}, eventData interface{}) error {
	if user, ok := model.(*User); ok {
		log.Printf("[HOOK %s] User created! ID: %d, Name: %s", eventType, user.ID, user.Name)
	}
	return nil
}

func main() {
	// Assume thing.Configure() and thing.AutoMigrate(&User{}) are done

	// Register hooks
	thing.RegisterListener(thing.EventTypeBeforeSave, validateEmailHook)
	thing.RegisterListener(thing.EventTypeAfterCreate, logAfterCreateHook)

	// Get ORM instance
	users, _ := thing.Use[*User]()

	// 1. Attempt to save user with invalid email (will be aborted by hook)
	invalidUser := &User{Name: "Invalid", Email: "invalid@example.com"}
	err := users.Save(invalidUser)
	if err != nil {
		fmt.Printf("Failed to save invalid user (as expected): %v\n", err)
	}

	// 2. Save a valid user (triggers BeforeSave and AfterCreate hooks)
	validUser := &User{Name: "Valid Hook User", Email: "valid@example.com"}
	err = users.Save(validUser)
	if err != nil {
		log.Fatalf("Failed to save valid user: %v", err)
	} else {
		fmt.Printf("Successfully saved valid user ID: %d\n", validUser.ID)
	}

	// Unregistering listeners is also possible with thing.UnregisterListener
}

```

## Caching & Monitoring

### Cache Monitoring & Hit/Miss Statistics

Thing ORM provides built-in cache operation monitoring for all cache clients (including Redis and the mock client used in tests). This monitoring capability is a core, integrated feature, not an add-on.

You can call `GetCacheStats(ctx)` on any `CacheClient` instance to retrieve a snapshot of cache operation counters. The returned `CacheStats.Counters` map includes keys such as:

- `Get`: total number of Get calls
- `GetMiss`: number of Get calls that missed (not found)
- `GetModel`, `GetModelMiss`: same for model cache
- `GetQueryIDs`, `GetQueryIDsMiss`: same for query ID list cache

**To compute hit count and hit rate:**

- `hit = total - miss`
- `hit rate = hit / total`

**Example:**

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

## Advanced Usage

### Raw SQL Execution

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

## License

Thing ORM is released under the [MIT License](LICENSE).

## Multi-Database Testing

Thing ORM supports multiple database systems, including MySQL, PostgreSQL, and SQLite. This section provides guidelines and considerations for testing across different environments.

### Considerations

- **Database Compatibility:** Thing ORM is designed to work with the SQL dialects of MySQL, PostgreSQL, and SQLite. However, there might be differences in SQL syntax or features between these databases.
- **Migration Tools:** Thing ORM's migration tools are database-agnostic and should work across all supported databases. However, you might need to adjust SQL statements or query results based on the specific database you're using.
- **Testing Strategy:**
  - **Unit Tests:** Ensure that your tests cover different database scenarios.
  - **Integration Tests:** Test the ORM's functionality with a variety of databases.
  - **Manual Testing:** Test the ORM in different environments to ensure compatibility.

### FAQ

- **Driver Customization:** If you need to customize the behavior of a database driver, you can do so by implementing the `DBAdapter` interface.
- **Test Environment Configuration:** Ensure that your test environment is set up correctly for database testing.
- **Interface Location:** All database-related interfaces are located in the `drivers/db` package.

## Contributing

We welcome contributions from the community! If you're interested in contributing to Thing ORM, please follow these steps:

1. **Fork the Repository:**
   - Go to the [Thing ORM GitHub repository](https://github.com/burugo/thing).
   - Click the "Fork" button to create your own copy of the repository.

2. **Clone the Repository:**
   - Clone your forked repository to your local machine.
   ```bash
   git clone https://github.com/your-username/thing.git
   ```

3. **Create a New Branch:**
   - Create a new branch for your changes.
   ```bash
   git checkout -b feature-new-feature
   ```

4. **Make Your Changes:**
   - Implement your changes in the code.

5. **Commit Your Changes:**
   - Commit your changes with a meaningful commit message.
   ```bash
   git commit -m "Added new feature"
   ```

6. **Push Your Changes:**
   - Push your changes to your forked repository.
   ```bash
   git push origin feature-new-feature
   ```

7. **Create a Pull Request:**
   - Go to your forked repository on GitHub.
   - Click the "Pull Request" button to create a pull request.

We'll review your pull request and merge it if it meets our standards.

## Performance

Thing ORM is designed to be performant and efficient. The following sections provide information about its performance characteristics and how to optimize its usage.

### Performance Considerations

- **Memory Usage:** Thing ORM uses in-memory caching for performance gains. Ensure that your system has enough memory to handle the cache.
- **Database Load:** Thing ORM reduces database load by caching queries and entities. However, excessive caching can lead to memory usage issues.
- **Query Execution:** Thing ORM provides efficient query execution. Complex queries might still require manual optimization.

### Optimizing Thing ORM

- **Caching Strategy:**
  - **Cache Invalidation:** Thing ORM automatically invalidates cache entries when data changes.
  - **Cache Size:** Thing ORM uses in-memory caching. Monitor memory usage and adjust cache size as needed.
- **Query Execution:**
  - **Preloading:** Thing ORM provides efficient query execution with preloading.
  - **Raw SQL:** Thing ORM supports raw SQL execution. Use it for complex queries or when Thing ORM's API doesn't meet your needs.



