# Thing ORM Usage Guide

This guide is the main day-to-day reference for using Thing ORM. The README stays short; model tags, queries, cache utilization, relationships, JSON output, hooks, and raw SQL guidance live here.

## Core Positioning

Thing is designed for high-frequency CRUD, ID-based reads, paginated list queries, lightweight relationship preloading, and automatic cache invalidation.

It does not try to cover every SQL feature. Complex reporting queries, ad-hoc analytics, database-specific SQL, and heavy JOIN queries can use raw SQL, but those reads should be treated as outside Thing's ORM cache path.

## Installation

```bash
go get github.com/burugo/thing
```

## Configuration

### Create A Database Adapter

```go
import "github.com/burugo/thing/drivers/db/sqlite"

db, err := sqlite.NewSQLiteAdapter("app.db")
if err != nil {
	log.Fatal(err)
}
```

MySQL and PostgreSQL use their corresponding drivers:

```go
// "github.com/burugo/thing/drivers/db/mysql"
// "github.com/burugo/thing/drivers/db/postgres"
```

### Configure Cache

When the second argument to `thing.New` or `thing.Configure` is `nil`, Thing uses the built-in in-memory cache. For production or multi-instance deployments, Redis is usually the better choice.

```go
import (
	"github.com/redis/go-redis/v9"
	redisCache "github.com/burugo/thing/drivers/cache/redis"
)

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
cacheClient := redisCache.NewClient(rdb)
```

### Create A Model Entry

For most applications, create one `Thing` instance per model:

```go
users, err := thing.New[*User](db, cacheClient)
if err != nil {
	log.Fatal(err)
}
```

For smaller applications, package-level configuration is also available:

```go
if err := thing.Configure(db, cacheClient); err != nil {
	log.Fatal(err)
}

users, err := thing.Use[*User]()
```

## Model Definition

Models usually embed `thing.BaseModel` and use `db` tags for column names, indexes, and unique constraints.

```go
type User struct {
	thing.BaseModel
	Name  string `db:"name,index"`
	Email string `db:"email,unique"`
	Age   int    `db:"age,default:18"`
}

func (u *User) TableName() string { return "users" }
```

Rules:

- `thing.BaseModel` provides `ID`, `CreatedAt`, `UpdatedAt`, and `Deleted`.
- `db:"name"` explicitly maps a field to a column. If omitted, Thing converts the Go field name to snake_case.
- `db:"-"` marks a field as not belonging to the table. Relationship fields must use it.
- Indexes are declared in the `db` tag: `index`, `unique`, `index:idx_name`, `unique:uq_name`.
- The `thing` tag is only for relationship declarations, not indexes.

Composite index example:

```go
type Account struct {
	thing.BaseModel
	TenantID int64  `db:"tenant_id,index:idx_tenant_email"`
	Email    string `db:"email,index:idx_tenant_email"`
	KeyA     string `db:"key_a,unique:uq_pair"`
	KeyB     string `db:"key_b,unique:uq_pair"`
}
```

## Migration

`AutoMigrate` uses the package-level DB configuration, so call `thing.Configure` first:

```go
if err := thing.Configure(db, cacheClient); err != nil {
	log.Fatal(err)
}

if err := thing.AutoMigrate(&User{}); err != nil {
	log.Fatal(err)
}
```

Multiple models can be migrated together:

```go
if err := thing.AutoMigrate(&User{}, &Post{}); err != nil {
	log.Fatal(err)
}
```

## Basic CRUD

```go
user := &User{Name: "Alice", Email: "alice@example.com"}

// Create or update
if err := users.Save(user); err != nil {
	log.Fatal(err)
}

// Create or update multiple records in one transaction
batch := []*User{
	{Name: "Bob", Email: "bob@example.com"},
	{Name: "Carol", Email: "carol@example.com"},
}
err := users.SaveMany(batch)

// Cached single read
found, err := users.ByID(user.ID)

// Cached batch read
userMap, err := users.ByIDs([]int64{user.ID})

// Soft delete: set deleted=true and update updated_at
err = users.SoftDelete(user)

// Hard delete
err = users.Delete(user)

// Hard delete multiple records in one transaction
err = users.DeleteMany(batch)
```

Prefer `ByID` and `ByIDs` for entity reads. They are the main model-cache path and are easier to reuse across list pages than scattered `SELECT *` calls.

`SaveMany` and `DeleteMany` keep the normal Thing lifecycle behavior, but group the database writes into one transaction. Model and query caches are updated only after the transaction commits.

## Queries

Thing queries return `*CachedResult[T]`. Execution is delayed until `Fetch`, `First`, `All`, or `Count`.

```go
result := users.Query(thing.QueryParams{
	Where: "age > ? AND status = ?",
	Args:  []interface{}{18, "active"},
	Order: "id DESC",
})

page, err := result.Fetch(0, 20)
count, err := result.Count()
first, err := result.First()
all, err := result.All()
```

Chainable form:

```go
page, err := users.
	Where("age > ?", 18).
	Order("id DESC").
	Fetch(0, 20)
```

Queries exclude soft-deleted records by default. To include them:

```go
page, err := users.
	Where("email = ?", email).
	WithDeleted().
	Fetch(0, 10)
```

## Relationships And Preloading

Relationship fields use the `thing` tag and must also use `db:"-"`.

```go
type User struct {
	thing.BaseModel
	Name  string  `db:"name"`
	Books []*Book `thing:"hasMany;fk:user_id;model:Book" db:"-"`
}

type Book struct {
	thing.BaseModel
	UserID int64 `db:"user_id,index"`
	Title  string `db:"title"`
	User   *User  `thing:"belongsTo;fk:user_id;model:User" db:"-"`
}
```

Preload related data with `Preload`:

```go
usersWithBooks, err := users.
	Where("id IN (?)", []int64{1, 2, 3}).
	Preload("Books").
	Fetch(0, 3)
```

Many-to-many relationships use a join table:

```go
type User struct {
	thing.BaseModel
	Roles []*Role `thing:"manyToMany;model:Role;joinTable:user_roles;joinLocalKey:user_id;joinRelatedKey:role_id" db:"-"`
}
```

## Improving Cache Utilization

This is the most important performance section when using Thing.

### Understand The Cache Path

Thing mainly caches two kinds of data:

- Model cache: `ByID` and `ByIDs` cache individual models by primary key.
- Query cache: `Fetch` caches list IDs, and `Count` caches counts.

After a list query hits cache, Thing hydrates the cached IDs through the model read path. This means the same model can be reused across multiple lists.

High cache hit rate comes from returning application reads to this shape:

```text
query list IDs -> ByIDs/model cache -> assemble result
```

### Query ID Lists First, Then Use ByIDs

If the business flow already has IDs, use them directly:

```go
ids := []int64{1, 2, 3}
userMap, err := users.ByIDs(ids)
```

If the IDs come from another table, query that table as a model first, then return to the main model:

```go
labels, err := threadLabels.
	Where("user_id = ? AND label = ?", userID, label).
	Fetch(0, 100)
if err != nil {
	return err
}

threadIDs := make([]int64, 0, len(labels))
for _, item := range labels {
	threadIDs = append(threadIDs, item.ThreadID)
}

threadsPage, err := threads.
	Where("id IN (?)", threadIDs).
	Order("updated_at DESC").
	Fetch(0, len(threadIDs))
```

Both queries remain model queries that Thing can understand, so they can reuse list cache, count cache, and model cache.

### Prefer Thing Reads Over SELECT *

Avoid frequent business-path reads like this:

```go
var rows []*Thread
err := db.Select(ctx, &rows, `
	SELECT *
	FROM threads
	WHERE user_id = ?
	ORDER BY updated_at DESC
`, userID)
```

Prefer:

```go
rows, err := threads.
	Where("user_id = ?", userID).
	Order("updated_at DESC").
	Fetch(0, 50)
```

Raw `SELECT *` bypasses model cache and query cache. Even if the SQL is fast, the application loses reuse across requests and list pages.

### Avoid JOINs In The Cache Path

Thing invalidates query cache around changed fields on a model table. `JOIN`, `EXISTS`, and subqueries often depend on multiple tables, which makes invalidation boundaries harder and model-cache reuse weaker.

Avoid this shape in high-frequency cached reads:

```go
threads.Where(`
	EXISTS (
		SELECT 1
		FROM thread_labels
		WHERE thread_labels.thread_id = threads.id
		  AND thread_labels.label = ?
	)
`, label).Fetch(0, 50)
```

Prefer two model queries:

```go
labels, err := threadLabels.Where("label = ?", label).Fetch(0, 100)
// collect threadIDs...
threads, err := threadThing.Where("id IN (?)", threadIDs).Fetch(0, len(threadIDs))
```

For reporting, aggregation, and one-off background jobs, raw SQL is fine. Treat it as outside the Thing cache path.

### Prefer `IN (?)` With A Slice

Recommended:

```go
threads.Where("id IN (?)", []int64{1, 2, 3}).Fetch(0, 3)
```

Thing expands the slice into the correct database placeholders while building SQL. An empty slice becomes `IN (NULL)`, which safely matches nothing.

Although the matcher can parse some manually expanded forms, prefer not to write this in application code:

```go
threads.Where("id IN (?, ?, ?)", 1, 2, 3)
```

`IN (?)` plus a slice is more stable and avoids placeholder/argument mismatch bugs when building dynamic SQL.

### Keep WHERE Clauses Parseable

Cache invalidation parses cached query `WHERE` clauses. Prefer simple conditions on model fields:

- `=`, `!=`, `<>`
- `>`, `<`, `>=`, `<=`
- `LIKE`, `NOT LIKE`
- `IN`, `NOT IN`
- `AND`, `OR`, parentheses
- `true`, `false`, and `NULL` literals

Avoid putting functions, expressions, and subqueries into cached queries:

```go
// Less cache-friendly
users.Where("LOWER(email) = ?", normalizedEmail)
```

Prefer storing queryable values in normal columns:

```go
users.Where("email_normalized = ?", normalizedEmail)
```

If a condition cannot be parsed, Thing favors safety: related writes delete the query cache instead of returning stale data. The tradeoff is lower cache hit rate.

### Keep Order Fields Stable

List cache depends on both `WHERE` and `ORDER BY`.

```go
threads.
	Where("user_id = ?", userID).
	Order("updated_at DESC, id DESC").
	Fetch(0, 50)
```

When `updated_at` or `id` affects ordering, relevant updates invalidate the list cache. More complex order expressions tend to fall back to more conservative invalidation.

### Use KeepItem For Complex Visibility

Some business visibility rules should not be forced into SQL, such as hidden posts, complex permissions, or final visibility checks. Put that logic in the model's `KeepItem()` method.

Thing list loading can fetch extra IDs and then filter with `KeepItem()`, keeping final business visibility in the model layer.

Good candidates for `KeepItem()`:

- Soft-delete or hidden-state checks
- Visibility rules that would make SQL too complex
- Final filtering that must stay consistent with model methods

If a condition is simple, stable, and frequently used as a list entry point, prefer storing it as a normal column and using it in `WHERE`. That keeps cache candidate sets smaller.

#### Declare KeepItemFields When You Override KeepItem

Thing's automatic cache invalidation locates affected list/count caches by the
**columns used in each query's `WHERE`/`ORDER`**. The framework already
special-cases `deleted`. A *custom* KeepItem field such as `hidden` is otherwise
treated as an ordinary column: if it does not appear in a query's `WHERE`,
changing it would not invalidate that query's cache, leaving it stale.

To close this gap, a model that overrides `KeepItem()` **must** also override
`KeepItemFields()` to declare the DB columns its visibility logic depends on
(besides `deleted`):

```go
func (p *Post) KeepItem() bool          { return !p.Deleted && !p.Hidden }
func (p *Post) KeepItemFields() []string { return []string{"hidden"} }
```

Now changing `hidden` via `Save()` automatically invalidates the affected
list/count caches — in both directions (showing or hiding a row) — with no
manual call:

```go
post.Hidden = false
posts.Save(post) // list/count caches that include this row are refreshed
```

If your `KeepItem()` depends on no mutable column (for example it always returns
`true`), declare it explicitly as empty:

```go
func (u *User) KeepItem() bool          { return true }
func (u *User) KeepItemFields() []string { return nil }
```

Enforcement is at construction time: `thing.New[T]` / `thing.Use[T]` returns an
error if a model overrides `KeepItem()` but not `KeepItemFields()`. (Go cannot
enforce this at compile time, so it fails fast at the first construction
instead.)

Note: automatic invalidation works within a single process. In a multi-instance
deployment sharing one Redis cache, each process only knows about cache keys it
registered itself, so cross-process invalidation is not yet guaranteed (tracked
in `docs/todo/distributed-cache-index.md`).

#### Count And CountPrecise With Custom KeepItem

`KeepItem` runs in Go and cannot be expressed in SQL, so `Count()` and `CountPrecise()` differ on large result sets once you override it:

- `Count()` derives the exact visible count from the KeepItem-filtered **list cache** when that cache is warm and under the list cache limit (200). When the list is not cached or exceeds the limit, it falls back to a SQL `COUNT` that ignores `KeepItem` and is therefore only approximate (possibly larger than the visible total).
- `CountPrecise()` is **always exact**: it applies `KeepItem` filtering, cached under a separate key, and stays correct even for result sets larger than the list cache limit (it scans and loads matching rows, so it is more expensive on large sets).

For models that do not override `KeepItem` (the default `BaseModel` behavior), the two methods are equivalent and both use the fast path.

Use `Count()` when a cheap, possibly-approximate count is acceptable for large sets. Use `CountPrecise()` when you must show or act on the exact visible total regardless of size.

### Reuse Conditions For Count And List

Pagination often needs both a page and a total:

```go
result := threads.Where("user_id = ?", userID).Order("updated_at DESC")

page, err := result.Fetch(0, 50)
total, err := result.Count()
```

Reuse the same `CachedResult` or the same `QueryParams` so list cache and count cache stay aligned.

### Prefer Preload For Related Models

If you only need relationship data, prefer:

```go
users.
	Where("id IN (?)", ids).
	Preload("Books").
	Fetch(0, len(ids))
```

Avoid turning this into one large JOIN just to fetch relationships. `Preload` lets the main model and related models use Thing's read path separately, which improves cache reuse.

## JSON Output

Thing provides `ToJSON` for field-level JSON output control. It is a method on `*thing.Thing[T]`:

```go
users.ToJSON(user, thing.Include("id", "name"))
users.ToJSON(user, thing.Exclude("email"))
users.ToJSON(user, thing.WithFields("name,profile{avatar},-id"))
```

`WithFields` supports nested fields, exclusion, and output order. Exported zero-argument single-return methods can be exposed as virtual fields:

```go
func (u *User) FullName() string {
	return u.FirstName + " " + u.LastName
}

users.ToJSON(user, thing.WithFields("first_name,full_name"))
```

## Hooks

Use event listeners for validation, auditing, and side effects.

```go
thing.RegisterListener(thing.EventTypeBeforeSave, func(ctx context.Context, eventType thing.EventType, model interface{}, data interface{}) error {
	user, ok := model.(*User)
	if !ok {
		return nil
	}
	if user.Email == "" {
		return errors.New("email is required")
	}
	return nil
})
```

Common events:

- `EventTypeBeforeSave`
- `EventTypeAfterSave`
- `EventTypeBeforeCreate`
- `EventTypeAfterCreate`
- `EventTypeBeforeDelete`
- `EventTypeAfterDelete`
- `EventTypeBeforeSoftDelete`
- `EventTypeAfterSoftDelete`

Returning an error from a `Before*` hook aborts the operation.

## Raw SQL

For complex SQL, use the adapter from a `Thing` instance:

```go
db := users.DBAdapter()

_, err := db.Exec(ctx, "UPDATE users SET status = ? WHERE id = ?", "active", id)

var stats struct {
	Total int64 `db:"total"`
}
err = db.Get(ctx, &stats, "SELECT COUNT(*) AS total FROM users")
```

You can also access the underlying `*sql.DB`:

```go
sqlDB := users.DB()
```

Guidelines:

- High-frequency business reads should prefer `Query`, `ByID`, and `ByIDs`.
- Reporting, aggregation, and one-off background jobs can use raw SQL.
- After raw SQL writes, call `InvalidateQueryCaches(ctx)` on the affected model when cached list/count queries for that table may now be stale.

## Cache Monitoring

If the cache client supports stats, inspect hit/miss data:

```go
stats, err := cacheClient.GetCacheStats(ctx)
```

When an endpoint has low cache hit rate, check:

- Whether it bypasses `ByID`, `ByIDs`, or `Query().Fetch`
- Whether it uses raw `SELECT *`
- Whether it puts JOINs, subqueries, or SQL functions into cached queries
- Whether the `WHERE` clause is parseable by Thing
- Whether it frequently updates list order fields
- Whether a business state could be stored as a normal column instead of hidden in an expression

## Selection Guide

| Scenario | Recommended |
| --- | --- |
| Read one row by primary key | `ByID` |
| Read a known set of IDs | `ByIDs` |
| Paginated list | `Where(...).Order(...).Fetch(offset, limit)` |
| List total | `Count()` with the same conditions |
| Related list | Query the relation table for IDs, then query the main model with `id IN (?)` |
| Load related models | `Preload` |
| Reporting / aggregation / one-off JOIN | Raw SQL |
| High-frequency business reads | Avoid raw `SELECT *`; return to the Thing cache path |
