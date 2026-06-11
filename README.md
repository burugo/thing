# Thing ORM

[![Go Report Card](https://goreportcard.com/badge/github.com/burugo/thing)](https://goreportcard.com/report/github.com/burugo/thing)
[![Build Status](https://github.com/burugo/thing/actions/workflows/go.yml/badge.svg)](https://github.com/burugo/thing/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/burugo/thing.svg)](https://pkg.go.dev/github.com/burugo/thing)
[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.18-blue.svg)](https://golang.org/doc/go1.18)

Thing is a Go ORM focused on fast CRUD, ID-based loading, list queries, and built-in cache invalidation. It uses generics for type-safe model access and supports SQLite, MySQL, and PostgreSQL.

For detailed usage, model tags, relationships, cache-friendly query patterns, and raw SQL guidance, see the usage guides:

- [English](docs/usage-guide-EN.md)
- [中文](docs/usage-guide-CN.md)

## Install

```bash
go get github.com/burugo/thing
```

## Quick Start

```go
package main

import (
	"log"

	"github.com/burugo/thing"
	"github.com/burugo/thing/drivers/db/sqlite"
)

type User struct {
	thing.BaseModel
	Name  string `db:"name,index"`
	Email string `db:"email,unique"`
}

func main() {
	db, err := sqlite.NewSQLiteAdapter("app.db")
	if err != nil {
		log.Fatal(err)
	}

	if err := thing.Configure(db, nil); err != nil { // nil uses the built-in memory cache
		log.Fatal(err)
	}

	if err := thing.AutoMigrate(&User{}); err != nil {
		log.Fatal(err)
	}

	users, err := thing.Use[*User]()
	if err != nil {
		log.Fatal(err)
	}

	alice := &User{Name: "Alice", Email: "alice@example.com"}
	if err := users.Save(alice); err != nil {
		log.Fatal(err)
	}

	cachedUser, err := users.ByID(alice.ID)
	if err != nil {
		log.Fatal(err)
	}
	_ = cachedUser

	activeUsers, err := users.
		Where("name LIKE ?", "A%").
		Order("id DESC").
		Fetch(0, 20)
	if err != nil {
		log.Fatal(err)
	}
	_ = activeUsers
}
```

## What Thing Optimizes For

- `Save`, `ByID`, `ByIDs`, `Delete`, and `SoftDelete`
- Paginated list queries through `Query(...).Fetch(offset, limit)`
- Count caching through `Query(...).Count()`
- Model cache reuse when list results are hydrated
- Cache invalidation for common `WHERE` and `ORDER BY` patterns
- Relationship preloading without hand-written joins

Thing intentionally keeps the ORM surface small. For reporting queries, database-specific SQL, or one-off joins, use the raw `DBAdapter` / `*sql.DB` accessors and treat those reads as outside the ORM cache path.

## Cache-Friendly Pattern

Prefer this shape:

```go
ids := []int64{1, 2, 3}
usersByID, err := users.ByIDs(ids)
```

or:

```go
page, err := users.Where("id IN (?)", ids).Fetch(0, len(ids))
```

Avoid replacing repeated application reads with `SELECT * ... JOIN ...` when the same data can be loaded as an ID list and hydrated through `ByID` / `ByIDs`. That is the path where Thing can reuse model cache and maintain query cache invalidation.

More examples are in [Cache Utilization](docs/usage-guide-EN.md#improving-cache-utilization) / [提高缓存利用率](docs/usage-guide-CN.md#提高缓存利用率).

## Documentation

- [Usage Guide (English)](docs/usage-guide-EN.md)
- [使用指南（中文）](docs/usage-guide-CN.md)
- [API Reference](https://pkg.go.dev/github.com/burugo/thing)
- [Partial Indexes Spec](docs/partial-indexes-spec.md)

## Development

Run the test suite:

```bash
go test ./...
```

## License

MIT
