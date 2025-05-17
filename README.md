# Thing ORM: High-Performance Go ORM with Built-in Caching

[![Go Report Card](https://goreportcard.com/badge/github.com/burugo/thing)](https://goreportcard.com/report/github.com/burugo/thing)
[![Build Status](https://github.com/burugo/thing/actions/workflows/go.yml/badge.svg)](https://github.com/burugo/thing/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/burugo/thing.svg)](https://pkg.go.dev/github.com/burugo/thing)
[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.18-blue.svg)](https://golang.org/doc/go1.18)

**Thing ORM** is a high-performance, open-source Object-Relational Mapper for Go, designed for modern application needs:

- **Unique Integrated Caching:** Thing ORM provides built-in support for either Redis or in-memory caching, making cache integration seamless and efficient. It automatically caches single-entity and list queries, manages cache invalidation, and provides cache hit/miss monitoring—out of the box. No third-party plugins or manual cache wiring required.
- **Type-Safe Generics-Based API:** Built on Go generics, Thing ORM provides compile-time type safety, better performance, and a more intuitive, IDE-friendly API—unlike most Go ORMs that rely on reflection and runtime type checks.
- **Multi-Database Support:** Effortlessly switch between MySQL, PostgreSQL, and SQLite with a unified API and automatic SQL dialect adaptation.
- **Simple, Efficient CRUD and List Queries:** Focused on the most common application patterns—thread-safe Create, Read, Update, Delete, and efficient list retrieval with filtering, ordering, and pagination.
- **Focused API:** Designed for fast CRUD and list operations. Complex SQL features like JOINs are out of the ORM's direct scope, but raw SQL execution is supported.
- **Elegant, Developer-Friendly API:** Clean, extensible, and idiomatic Go API, with flexible JSON serialization, relationship management, and hooks/events system.
- **Open Source and Community-Ready:** Well-documented, thoroughly tested, and designed for easy adoption and contribution by the Go community.

## Table of Contents

- [Installation](#installation)
- [Configuration](#configuration)
- [API Documentation](#api-documentation)
- [AI Assistant Integration (e.g., Cursor)](#ai-assistant-integration-eg-cursor)
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
- [Multi-Database Testing](#multi-database-testing)
- [FAQ](#faq)
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
	// "github.com/burugo/thing/drivers/db/mysql"
	// "github.com/burugo/thing/drivers/db/postgres"
	"github.com/burugo/thing/drivers/db/sqlite"
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
	"github.com/burugo/thing"
)

// Option A: Use Default In-Memory Cache
// Simply pass nil as the cache client when calling thing.New
var cacheClient thing.CacheClient = nil

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

## AI Assistant Integration (e.g., Cursor)

For developers using AI coding assistants like Cursor, a dedicated project rule file is available to help the AI understand and correctly utilize the Thing ORM within this project.

You can find the rule file here: [Thing ORM Cursor Rules](https://github.com/burugo/thing/blob/main/.cursor/rules/thing.mdc)

Referencing this rule (`@thing`) in your prompts can improve the AI's accuracy when generating or modifying code related to Thing ORM.

## Basic CRUD Example


Here is a minimal example demonstrating how to use Thing ORM for basic CRUD operations:

```go
package main

import (
	"fmt"
	"log"

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

	// Chainable Query API
	usersOver18, err := users.Where("age > ?", 18).Order("name ASC").Fetch(0, 100)
	if err != nil { /* handle error */ }
	fmt.Println(usersOver18)

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
	// import your models package e.g., "yourproject/models"
)

func main() {
	// Assume thing.Configure() and AutoMigrate(&models.User{}, &models.Book{}) are done
	// Assume user and books are created...

	userThing, _ := thing.Use[*models.User]()
	bookThing, _ := thing.Use[*models.Book]()

	// Example 1: Find a user and preload their books (HasMany)
	userParams := thing.QueryParams{
		Where:    "id = ?",
		Args:     []interface{}{1}, // Assuming user with ID 1 exists
		Preloads: []string{"Books"}, // Specify the relationship field name
	}
	userResult := userThing.Query(userParams)
	fetchedUsers, _ := userResult.Fetch(0, 1)
	if len(fetchedUsers) > 0 {
		fmt.Printf("User: %s, Number of Books: %d\n", fetchedUsers[0].Name, len(fetchedUsers[0].Books))
		// fetchedUsers[0].Books is now populated
	}

	// Example 2: Find a book and preload its user (BelongsTo)
	bookParams := thing.QueryParams{
		Where:    "id = ?",
		Args:     []interface{}{5}, // Assuming book with ID 5 exists
		Preloads: []string{"User"}, // Specify the relationship field name
	}
	bookResult := bookThing.Query(bookParams)
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

You can call `GetCacheStats(ctx)`

## ⚠️ Note: Caching Custom Struct Fields and gob.Register

If your model struct contains a custom struct field (such as Address) and you want it to be cached (i.e., the field is not tagged with db:"-"), **you must register that struct type with gob.Register in your main program or test code**. Otherwise, you will encounter a gob encoding error when caching:

```
gob: type not registered for interface: yourpkg.Address
```

For example:
```go
import (
    "encoding/gob"
)

func init() {
    gob.Register(Address{}) // Address is your struct type
}
```

- This registration must be done in the same package where the struct is defined (e.g., in tests/struct_json_test.go), and the type must match the actual package path exactly.
- If the field is a pointer type, you should also register gob.Register(&Address{}).
- It is also recommended to always register time.Time.

This ensures that the cache layer (whether in-memory or Redis) can correctly serialize and restore your struct fields.