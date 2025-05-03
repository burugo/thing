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

> **Thing ORM** is ideal for projects that need fast, reliable, and maintainable data access without the overhead of a full-featured SQL builder. It empowers developers to build scalable applications with minimal boilerplate and maximum control over caching and serialization.

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



