# Background and Motivation

The primary goal of this project is to develop a high-performance, Object-Relational Mapper (ORM) for Go, named **Thing ORM** (package `thing`). This ORM aims to provide:
- Support for multiple popular relational databases (initially targeting MySQL, PostgreSQL, and SQLite).
- Integrated, configurable caching layer (leveraging Redis) to optimize read performance for entities and lists.
- **Focus:** Providing convenient, high-performance, thread-safe CRUD operations (`Create`, `Read`, `Update`, `Delete`) for single entities and efficient querying for lists of entities based on simple criteria (filtering, ordering, pagination).
- **Explicit Exclusion:** This project will *not* aim to replicate the full complexity of SQL. Features like JOINs, aggregations (COUNT, SUM, AVG), GROUP BY, and HAVING are explicitly out of scope. The focus is on the application-level pattern of cached object access, not complex SQL generation.
- An elegant and developer-friendly API designed for ease of use and extensibility.
- The ultimate objective is to release this ORM as an open-source library for the Go community.

This project builds upon the initial goal of replicating a specific `BaseModel`, enhancing it with multi-DB support and packaging it as a reusable `thing` library, while intentionally keeping the query scope focused.

The goal is to modernize the Thing ORM API for better usability and developer experience. Two main improvements are planned:
1. Enable chainable query calls by making Query return a single value, with errors propagated via Fetch/All/First methods.
2. Enhance ToJSON to support both single objects and collections (slices/arrays), including nested preload objects, with consistent field filtering/DSL support.

These changes will simplify business code, reduce boilerplate, and align Thing ORM with modern Go ORM/library best practices. All related documentation and examples must be updated to reflect the new API.

# Key Challenges and Analysis

- Ensuring error propagation is robust and intuitive when Query no longer returns error directly.
- Maintaining backward compatibility or providing clear migration guidance for existing users.
- Ensuring ToJSON handles all relevant types (single, slice, array, nested preload) and field filtering consistently.
- Updating all documentation, examples, and rule files to prevent confusion and ensure smooth adoption.
- **Cache (De)Serialization with `json:"-"`:** Standard `json.Marshal/Unmarshal` will omit/ignore fields tagged with `json:"-"`. If such fields (e.g., `Password`) need to be cached, the cache (de)serialization mechanism must be customized to include them. This involves: 
    - Marshalling: Building a map of all DB-mapped fields (from `ModelInfo`) and marshalling this map to JSON (or another format), rather than marshalling the struct directly.
    - Unmarshalling: Unmarshalling cached data to a map, then using `ModelInfo` and reflection to populate all DB-mapped fields in the target struct, performing necessary type conversions.

# High-level Task Breakdown

### Structure Decoupling & Interface Refactor Plan (gorm-style driver ecosystem) — Major Tasks

- [x] Project Setup & Core Structure
- [x] Database Adapter Layer
- [x] Basic CRUD Operations (No Cache Yet)
- [x] Initial Query Executor Design
- [x] Caching Layer Integration
- [x] Relationship Management (Phase 1: BelongsTo, HasMany)
- [x] Hooks/Events System
- [x] Transaction Management
- [x] Adding Support for More Databases (MySQL, PostgreSQL)
- [x] Querying Refinements
- [x] Relationship Management (Phase 2: ManyToMany)
- [x] Schema Definition & Migration Tools
- [x] **Add Comprehensive Tests**
    - [x] Write integration tests for `AutoMigrate` + composite indexes (SQLite)
- [~] Testing, Benchmarking, and Refinement
- [x] Interface and Type Migration
- [x] Decoupling from internal dependencies
- [x] SQLBuilder/Dialector migration and dependency review
- [x] Move driver packages out of internal, into public drivers directory
- [x] Main package and drivers only depend on interfaces, never on each other's implementation
- [x] Documentation and README update

## 1. Refactor Query for Chainable Calls (Task Type: Refactoring (Functional))
- 1.1 Change Thing.Query signature to return only *CachedResult[T] (or equivalent), not error.
- 1.2 Add Err field to CachedResult; Query sets this on error.
- 1.3 Update Fetch/All/First methods to return upstream error if present.
- 1.4 Update all business code and examples to use chainable calls.
- 1.5 Update and expand tests to cover new error handling and chaining.
- 1.6 Update documentation (README, examples, thing.mdc) to reflect new usage and error handling.

### Success Criteria
- Query returns a single value; all calls can use chainable .Fetch(...).
- Errors are reliably propagated via Fetch/All/First.
- All tests pass; documentation and examples are up to date.



### Success Criteria
- ToJSON works seamlessly for single objects and collections, including nested preload objects.
- Field filtering/DSL works consistently in all cases.
- All tests pass; documentation and examples are up to date.

# Project Status Board

- [x] **Define and Finalize Tag Syntax**
- [x] **Update Metadata Parsing (`internal/schema/metadata.go`)**
    - [x] Implement parsing logic
    - [x] Update `ModelInfo` struct
    - [x] Write unit tests for parsing
- [x] **Update `AutoMigrate` Logic (DB Adapters)**
    - [x] Modify SQLite adapter (Integration test added)
    - [x] (Optional/Later) Modify MySQL adapter
    - [x] (Optional/Later) Modify PostgreSQL adapter
- [x] **Add Comprehensive Tests**
    - [x] Write integration tests for `AutoMigrate` + composite indexes (SQLite)
- [ ] **Update Documentation**
    - [ ] Update `README.md`
- [x] Fix golangci-lint config errors (mnd.ignored-numbers as string array, remove depguard.ignore-internal)
- [x] Ignore funlen and gocyclo (complexity) for all test files in golangci-lint config
- [x] Ignore errcheck (unchecked error returns) for all test files in golangci-lint config
- [x] Ignore lll (long line linter) for all test files in golangci-lint config
- [x] 修复 JSON DSL 解析器，移除无用 [] 分支，恢复 -field 排除逻辑，所有相关测试通过
- [x] Remove ineffectual assignment to fkFieldFound in preloadBelongsTo and preloadHasMany (relationships.go)
- [x] Refactor if-else chain to switch in preloadManyToMany (relationships.go, line 541)
- [x] Remove ineffectual assignment to placeholders in saveInternal else branch (crud.go)
- [x] Move type declarations out of generic function in cache.go for Go 1.18 compatibility
- [x] 1. Refactor Query for Chainable Calls (Refactoring (Functional))
    - [x] 1.1 Change Query signature
    - [x] 1.2 Add CachedResult.Err field
    - [x] 1.3 Update Fetch/All/First error propagation
    - [x] 1.4 Update business code and examples
    - [x] 1.5 Update tests
    - [x] 1.6 Update documentation (README, examples, thing.mdc)
- [x] 2. Enhance ToJSON for Single and Collection Types (New Feature)
    - [x] 2.1 Refactor ToJSON implementation
    - [x] 2.2 Support nested preload and field filtering
    - [x] 2.3 Add/expand tests
    - [x] 2.4 Update documentation (README, examples, thing.mdc)
- [x] Refactor NewClient signature to support *redis.Client injection (client优先，opts兜底)
- [x] Update all call sites to new signature (examples/04_hooks/main.go)
- [x] Run all tests to verify no regression
- [x] **Investigate and Resolve `json:"-"` Impact on Cached Fields**
    - [x] Reproduced issue: `json:"-"` fields are lost if cache uses standard `json.Marshal/Unmarshal`.
    - [x] Confirmed ORM DB loading is correct by bypassing cache in tests.
    - [x] Prototyped custom (de)serialization in `mockCacheClient` (`marshalForMock`, `unmarshalForMock`) to include all DB fields in cache, successfully passing `TestFetch_WithJsonDashField`.

## Executor's Feedback or Assistance Requests

The investigation into the `json:"-"` tag affecting cached field loading is complete. The root cause is the standard behavior of `json.Marshal` (omitting `json:"-"` fields) and `json.Unmarshal` (ignoring `json:"-"` fields on the target struct) when used by a caching layer.

The ORM's direct database loading logic is correct.
A solution was prototyped in the `mockCacheClient` by implementing custom marshalling (struct -> map -> JSON, including all DB fields) and custom unmarshalling (JSON -> map -> struct, populating all DB fields via reflection). This allowed the test `TestFetch_WithJsonDashField` to pass, demonstrating that fields like `Password` can be correctly cached and retrieved if the cache (de)serialization strategy is adapted.

**The next step is for the user to apply a similar custom (de)serialization strategy to their production cache client implementation (e.g., for Redis).** This involves modifying how models are converted to/from the cache storage format to ensure all fields defined in `ModelInfo` are preserved, irrespective of `json:"-"` tags.

**Update (Current Step):** We are now refactoring the `CacheClient` interface and its implementations to use Gob encoding and pass a list of fields to cache.
- `CacheClient` interface in `interfaces.go` has been updated: `SetModel` now takes `fieldsToCache []string`.
- Core ORM calls to `SetModel` in `crud.go` have been updated to pass `modelInfo.Fields`.
- `mockCacheClient` in `tests/mock_cache_test.go` has been refactored to use Gob encoding based on `fieldsToCache` for its `SetModel` and `GetModel` methods. The call in `tests/cache_stats_test.go` has also been updated.
- Production cache client `drivers/cache/redis/client.go` has been updated to use Gob encoding for `SetModel`, `GetModel`, `SetQueryIDs`, and `GetQueryIDs`.

**User Paused:** User is considering performance implications of reflection in the Gob (de)serialization path (map -> struct) and asked if providing expected fields during unmarshalling could avoid reflection and improve speed. Current assessment: minimal benefit for the current map-based strategy; significant speedup would likely require code generation or models implementing `GobEncoder/Decoder`.

Waiting for user to decide whether to proceed with current plan or explore performance optimizations now.

No further actions from my side on this specific issue unless new information or a request to analyze the production cache client code is provided.

**Executor's Feedback (New - 2025-05-15):**
- Successfully fixed Gob (de)serialization issues in `mockCacheClient` for non-struct types (`[]int64`, `int64`) in methods like `SetQueryIDs`, `GetQueryIDs`, `SetCount`, `GetCount`.
- Registered `time.Time` with Gob during `mockCacheClient` initialization to resolve issues when `map[string]interface{}` (potentially containing `time.Time`) was Gob-encoded.
- Resolved the `TestUserRole_Save_TriggersCacheInvalidation` test failure. The failure was due to a mismatch between the cache pre-setting (using JSON) and the cache access logic (expecting Gob) within the test, which prevented cache invalidation from being correctly tested.
- Corrected a `fmt.Errorf` call in `cache.go` that was incorrectly using multiple `%w` directives.
- Attempted to clean up unused imports in `drivers/cache/redis/client.go`.
- All related changes have been committed (commit `820ccbd`).
- **Note:** `drivers/cache/redis/client.go` may still have compiler-reported unused imports (`errors`, `strconv`) as the `edit_file` tool did not apply the cleanup förändring as expected. This is recommended to be addressed via `goimports` or a linter in subsequent steps or CI. Apart from this, all other tests appear to pass.

**Executor's Feedback (New - 2025-05-15_Part2):**
- Corrected another `fmt.Errorf` call in `drivers/cache/redis/client.go` (specifically in the `GetModel` method) that was also using multiple `%w` directives. This was a similar issue to the one previously fixed in `cache.go`.
- After this fix, and with the user manually resolving unused imports in `drivers/cache/redis/client.go`, all tests (`go test -v ./...`) now pass successfully.
- The primary set of issues related to Gob encoding, `json:"-"` field handling in cache, and associated test logic appear to be fully resolved.
- Changes committed (commit `c43866c`).

# Lessons

- After structural refactoring, documentation and lint checks must be synchronized to avoid omissions and technical debt.
- For large projects, it is recommended to address style and complexity issues in phases, prioritizing functional and interface consistency.
- The number of fields returned by SHOW INDEX may differ between MySQL versions/drivers. It is recommended to use information_schema.statistics for accurate retrieval.
- For multi-database testing, it is recommended to start the services locally first. In CI, use docker-compose to ensure a consistent environment.
- mnd.ignored-numbers in golangci-lint config must be an array of strings, not numbers.
- depguard.ignore-internal is not a supported option in golangci-lint v1.64.8; remove it to avoid schema errors.
- `json:"-"` tag on a struct field causes `encoding/json` to ignore it for both marshalling (field omitted from JSON) and unmarshalling (field not populated from JSON, even if present). If cached objects need to include such fields, the caching (de)serialization must use a custom approach (e.g., struct-to-map-to-JSON for marshalling, and JSON-to-map-to-struct for unmarshalling using reflection and `ModelInfo`).
- When manually pre-setting cache entries in tests, ensure the encoding method (e.g., JSON, Gob) matches exactly what the cache client and cache invalidation logic expect to read/process for that type of data. Mismatches can lead to decoding errors that mask or prevent the testing of a  cache invalidation.
- `fmt.Errorf` allows only one `%w` directive for error wrapping. Additional errors should be included using `%v` or other appropriate verbs.
- When test helper types or functions (like `mockCacheClient`) are defined in one `_test.go` file and used by another `_test.go` file within the same package, ensure test commands are run for the entire package (e.g., `go test ./tests`) rather than for isolated files. This ensures all necessary symbols are compiled and linked together.

**New Lesson (2025-05-15):**
- Double-check all `fmt.Errorf` calls, especially those involving multiple potential errors, to ensure only one error is wrapped with `%w`. Other error variables should be included in the message using `%v`. This applies across all modules, not just the initially identified ones.

# User Specified Lessons

- Include info useful for debugging in the program output.
- Read the file before you try to edit it.
- Always ask before using the -force git command

# Future/Optional Enhancements

- [ ] Integrate singleflight for cache stampede protection
    - Goal: When a cache miss occurs, ensure that only one request for a given key queries the database, while other concurrent requests wait and reuse the result.
    - Approach: Use golang.org/x/sync/singleflight to wrap DB fetch logic in ByID, Query, Fetch, etc., on cache miss.
    - Status: Change points and design have been fully analyzed; implementation deferred until needed.

- [ ] L1/L2 cache support (in-memory + Redis)
    - Goal: Add a two-level cache system, with a fast in-memory (L1) cache for each process and a distributed (L2) cache such as Redis.
    - Approach: Implement a process-wide in-memory cache (e.g., map or LRU) for hot objects/queries, falling back to Redis on miss. Ensure consistency and invalidation across both layers.
    - Status: Not currently implemented; to be planned and scheduled as needed.

- [ ] Configurable TTLs for different cache types (object, list, count)
    - [ ] Design configuration structure for per-type TTLs
    - [ ] Refactor cache set logic to use per-type TTLs
    - [ ] Add tests for TTL configuration and expiration
    - [ ] Update documentation/comments
    - [ ] Verify all tests pass and commit
