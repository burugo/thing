// Package thing is a Go ORM focused on fast CRUD, ID-based loading, list
// queries, and built-in cache invalidation. It uses generics for type-safe
// model access and supports SQLite, MySQL, and PostgreSQL as database backends,
// with pluggable cache drivers (local in-memory or Redis).
//
// Quick start:
//
//	db, _ := sqlite.Open("app.db")
//	thing.Configure(db)
//	th, _ := thing.Use[*User]()
//	_ = th.Save(&User{Name: "Alice"})
//	user, _ := th.ByID(1)
//
// See https://github.com/burugo/thing for full documentation.
package thing
