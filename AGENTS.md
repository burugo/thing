# Thing ORM — Agent Navigation Guide

## Overview

Thing is a Go ORM library with generics, built-in two-level caching (local/Redis), and support for SQLite, MySQL, and PostgreSQL. It uses `Thing[T Model]` as the central generic access point.

**Go minimum version:** 1.22 (`go.mod`)
**Lint:** `golangci-lint run ./...` (config in `.golangci.yml`, includes `errcheck` + `staticcheck`)
**Test:** `go test -race -count=1 ./...`

## Architecture

```
User Code
    │
    ▼
thing.Configure(db, cache)          ← Global init (config.go)
thing.Use[*User]() / thing.New[*User](db, cache)
    │
    ▼
Thing[T] ──────────────────────────── Central generic struct (thing.go)
    │
    ├─ Save/Delete/ByID/ByIDs       ← CRUD operations (crud.go)
    ├─ Query(params).Fetch/Count    ← List queries with caching (query.go)
    ├─ Preload (BelongsTo/HasMany/ManyToMany) ← Relationship loading (relationships.go)
    ├─ Hooks (Before/AfterSave etc.) ← Event system (hooks.go)
    └─ AutoMigrate                   ← Schema migration (migrate.go)
         │
         ├─ CacheClient interface    ← Cache layer (interfaces.go, cache.go)
         │   ├─ localCache (sync.Map)   ← Default in-memory (cache.go:636+)
         │   └─ redis.Client            ← Redis driver (drivers/cache/redis/)
         │
         └─ DBAdapter interface      ← DB layer (interfaces.go)
             ├─ sqlite               ← drivers/db/sqlite/
             ├─ mysql                ← drivers/db/mysql/
             └─ postgres             ← drivers/db/postgres/
```

## Key Files Index

| File | Lines | Purpose |
|------|------:|---------|
| `thing.go` | 120 | `Thing[T]` struct, `New`, `Use`, `WithContext` constructors |
| `interfaces.go` | 84 | Core interfaces: `Model`, `DBAdapter`, `CacheClient`, `SQLBuilder`, `Dialector` |
| `config.go` | 141 | Global config (`Configure`, `ConfigureWithConfig`), atomic TTL (`getGlobalCacheTTL`) |
| `crud.go` | 781 | `Save`, `ByID`, `ByIDs`, `Delete`, `SaveMany`, `DeleteMany`, `fetchModelsByIDsInternal` |
| `query.go` | ~800 | `CachedResult[T]`, `Query`, `Fetch`, `Count`, `CountPrecise`, `First`, cache key generation |
| `cache.go` | 1145 | Cache invalidation engine, `localCache` impl, `WithLock`, `CacheKeyLockManager` |
| `relationships.go` | 770 | `Preload`, `preloadBelongsTo`, `preloadHasMany`, `preloadManyToMany` |
| `model.go` | 144 | `BaseModel` struct (embed for ID/timestamps/soft-delete), helper functions |
| `hooks.go` | 110 | Event system: `RegisterListener`, `triggerEvent`, lifecycle event types |
| `json.go` | 612 | Custom JSON serialization with field selection (`ToJSON`, `ParseFieldOptions`) |
| `migrate.go` | 113 | `AutoMigrate`, `GenerateMigrationSQL`, introspector registry |
| `index.go` | 14 | `Index` struct and `IndexProvider` interface for programmatic indexes |
| `logging.go` | 37 | Log level/logger configuration (`SetLogLevel`, `SetLogger`) |

## Internal Packages

| Package | Path | Purpose |
|---------|------|---------|
| `schema` | `internal/schema/` | Struct metadata extraction: `ModelInfo`, field→column mapping, tag parsing, index detection |
| `cache` | `internal/cache/` | `CacheIndex` (query registration/invalidation), `CheckQueryMatch` predicate evaluation |
| `dbscan` | `internal/dbscan/` | Maps `database/sql.Rows` → Go structs via cached column→field index |
| `sqlbuilder` | `internal/sqlbuilder/` | Dialect-aware SQL generation (SELECT, INSERT, UPDATE, DELETE, COUNT) |
| `utils` | `internal/utils/` | Gob registration, value normalization, table name derivation |
| `logging` | `internal/logging/` | Level-filtered internal logger |
| `types` | `internal/types/` | Shared `QueryParams` struct (breaks import cycles) |

## Critical Mechanisms

### 1. Cache Invalidation (cache.go — the largest and most complex file)

On every `Save`/`Delete`, `invalidateQueryCaches` runs:
1. **Gather phase**: Iterates all registered query keys for the model's table via `GlobalCacheIndex`.
2. **Match check**: Uses `CheckQueryMatchWithPredicate` to evaluate if the changed model matches each cached query's WHERE clause.
3. **Compute phase**: Determines add/remove actions for list cache IDs.
4. **Execute phase**: Applies changes (delete keys or update ID lists) under per-key locks.

Important: `GlobalCacheIndex` (in `internal/cache/index.go`) is a global singleton protected by `sync.RWMutex`. It maps `tableName → []registeredQuery{cacheKey, queryParams, predicate}`.

### 2. Atomic Cache TTL (config.go)

`globalCacheTTL` is stored as `atomic.Int64` (nanoseconds). Read via `getGlobalCacheTTL()`, write via `setGlobalCacheTTL()`. This avoids locking on every cache Set call across all hot paths.

### 3. Query Flow (query.go)

`Thing.Query(params)` returns `CachedResult[T]` (lazy). On `Fetch(offset, limit)`:
1. Check cache for ID list (`GetQueryIDs`)
2. If miss → DB query → filter by `KeepItem()` → cache IDs
3. Fetch models by IDs (batch, with object cache)
4. On inconsistency → invalidate cache, retry from DB

### 4. Relationship Preloading (relationships.go)

`Preload("Author", "Tags")` triggers after `Fetch`. Three strategies:
- **BelongsTo**: Collect FK values → batch fetch related models → set fields via reflect
- **HasMany**: For each parent ID → query related table → batch fetch
- **ManyToMany**: Query join table → collect related IDs → batch fetch

All use `fetchModelsByIDsInternal` which checks object cache first, then batches DB queries.

### 5. Model Metadata (internal/schema/metadata.go)

`GetCachedModelInfo(type)` returns `*ModelInfo` with:
- `TableName`, `Fields`, `Columns`, `ColumnToFieldMap`, `FieldToColumnMap`
- `PKName`, `HasCustomKeepItem`, `HasCustomKeepItemFields`
- Index definitions from struct tags + `IndexProvider` interface

Cached in a `sync.Map` keyed by `reflect.Type`.

### 6. SQL Identifier Validation

SQLite and MySQL introspectors validate table/index names with `^[a-zA-Z_][a-zA-Z0-9_]*$` regex before string concatenation into PRAGMA/SHOW queries. PostgreSQL uses parameterized queries.

## Concurrency Model

- **`localCache`**: All fields are `sync.Map` or `atomic` types. Cleanup goroutine uses `select` on `stopCleanup` channel.
- **`GlobalCacheIndex`**: Protected by `sync.RWMutex`.
- **Global config** (`globalDB`, `globalCache`, `isConfigured`): Protected by `configMutex` (sync.RWMutex).
- **Cache TTL**: `atomic.Int64` — lock-free reads.
- **Event listeners**: `listenerMutex` (sync.RWMutex).
- **Per-key cache locks**: `CacheKeyLockManagerInternal` uses `sync.Map` of `*sync.Mutex`.

## Testing

| Directory | What |
|-----------|------|
| `tests/` | Integration tests against SQLite (basic ops, queries, relationships, hooks, benchmarks) |
| Root `*_test.go` | Unit tests (`cache_internal_test.go`), fuzz tests (`fuzz_test.go`) |
| `tests/benchmark_test.go` | 7 benchmarks: ByID cache hit/miss, Save create/update, Query fetch/count, hash generation |

Test helper: `setupTestDB(t)` creates in-memory SQLite + `mockCacheClient`.

## Conventions

- Error strings: lowercase, no punctuation (Go convention, enforced by `staticcheck`)
- Use `reflect.PointerTo()` not deprecated `reflect.PtrTo()`
- All `rows.Scan` errors must be checked; `rows.Err()` must be called after iteration
- No empty `else`/`if` branches (SA9003)
- `recover()` calls must assign to `_` for errcheck compliance
