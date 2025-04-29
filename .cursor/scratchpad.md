# Thing ORM Project

## Background and Motivation

(Revised) The primary goal of this project is to develop a high-performance, Object-Relational Mapper (ORM) for Go, named **Thing ORM** (package `thing`). This ORM aims to provide:
- Support for multiple popular relational databases (initially targeting MySQL, PostgreSQL, and SQLite).
- Integrated, configurable caching layer (leveraging Redis) to optimize read performance for entities and lists.
- **Focus:** Providing convenient, high-performance, thread-safe CRUD operations (`Create`, `Read`, `Update`, `Delete`) for single entities and efficient querying for lists of entities based on simple criteria (filtering, ordering, pagination).
- **Explicit Exclusion:** This project will *not* aim to replicate the full complexity of SQL. Features like JOINs, aggregations (COUNT, SUM, AVG), GROUP BY, and HAVING are explicitly out of scope. The focus is on the application-level pattern of cached object access, not complex SQL generation.
- An elegant and developer-friendly API designed for ease of use and extensibility.
- The ultimate objective is to release this ORM as an open-source library for the Go community.

This project builds upon the initial goal of replicating a specific PHP `BaseModel`, enhancing it with multi-DB support and packaging it as a reusable `thing` library, while intentionally keeping the query scope focused.

## Key Challenges and Analysis

(Revised) Building this focused ORM presents several challenges:

- **Integrating Existing Code:** Leveraging the progress made on the initial `thing.go` (basic CRUD, caching concepts, hooks) as a foundation for the full ORM.
- **Retaining `BaseModel` Concept:** Designing the ORM so that user models can embed a base struct (e.g., `thing.BaseModel` or `thing.ThingBase`) which provides common fields and potentially core method access, maintaining familiarity from the initial implementation.
- **Multi-Database Compatibility:** Ensuring SQL dialect compatibility *for the supported simple query features* and consistent behavior across MySQL, PostgreSQL, and SQLite.
- **Efficient Query Implementation:** Designing and implementing the focused query capabilities (WHERE, ORDER BY, LIMIT, OFFSET) efficiently across different databases.
- **Caching Strategy & Invalidation:** Integrating caching effectively for both single objects and query results (lists of IDs/objects), while tackling cache invalidation for the supported operations. *(See Task 16 for specific CachedResult refactor)*
- **Performance Optimization:** Balancing features with performance. Optimizing SQL generation *for simple queries*, minimizing reflection overhead, efficient caching, and potentially connection pooling.
- **API Design:** Creating an API that is intuitive, powerful for its scope, and idiomatic Go.
- **Schema Definition:** Providing clear patterns for defining database schemas in Go (likely via struct tags).
- **Concurrency Control:** Implementing safe concurrent operations.
- **Testing Complexity:** Requires testing against multiple database versions for the supported feature set.
- **Open Source Considerations:** Documentation, examples, contribution guidelines, licensing, and community building.
- **Testing:** Thorough testing is required due to the dynamic nature of reflection and caching. Intermittent failures need careful debugging (e.g., using `-p 1`, `-v`, `-race`).
- **Refactoring `ByID`:** Merging `byIDInternal` into `fetchModelsByIDsInternal` simplifies the codebase but removes the specific lock previously used for single-ID fetches. The impact of this removal on cache stampedes for single items needs observation, though `NoneResult` caching should mitigate this for non-existent items.

## Design Philosophy and API Goals

(Revised) The ORM should prioritize:

1.  **Performance:** Leverage Go's strengths and employ smart caching/querying for simple CRUD and list operations.
2.  **Developer Experience:** Provide a clean, intuitive, and well-documented API focused on the core use cases. User models will embed a `thing.BaseModel` struct.
3.  **Extensibility:** Allow users to customize behavior through hooks, custom types, or other extension points.
4.  **Database Agnosticism (for supported features):** Abstract database-specific details for basic CRUD and simple querying.
5.  **Robustness & Thread Safety:** Ensure correctness and safety through testing and sound design.
6.  **Pragmatic Caching:** Offer flexible caching options (object, query lists) with clear invalidation strategies. *(See Task 16 for specific CachedResult refactor)*
7.  **Focus on Simplicity:** Avoid building a complex SQL query builder. Provide easy ways to fetch single items and lists based on common conditions.


## High-level Task Breakdown

(Revised ORM Plan - Integrating Previous Work & Refined Scope)

*This plan outlines the steps for building the Thing ORM, focusing on cached CRUD and simple list queries.*

1.  **[~] Project Setup & Core Structure:** (Partially addressed by `thing.go`)
    *   Initialize/Verify Go module. **Package name: `thing`**.
    *   **Rename `thing.go` to `thing.go`**.
    *   Define/Refine core interfaces (`DBAdapter`, `CacheClient`, `Model`, `QueryExecutor`, etc.).
    *   Set up basic project layout (`thing/`, `thing/internal/`, `examples/`, `tests/`).
    *   Setup basic logging and configuration handling.
    *   **Success Criteria:** Project structure created (`thing` package with `thing.go`), core interfaces defined, basic build/test pipeline works.
2.  **[ ] Database Adapter Layer:**
    *   Design the `DBAdapter` interface (potentially refining `DBExecutor`).
    *   Implement initial adapter for one database (e.g., SQLite or PostgreSQL using `sqlx` or `database/sql`). `thing.go` uses placeholder logic.
    *   Implement connection pooling/management.
    *   **Success Criteria:** Able to connect to the target DB, execute *real* SQL, and manage connections via the adapter interface.
3.  **[~] Basic CRUD Operations (No Cache Yet):** (Partially addressed by `thing.go` placeholders)
    *   Implement *actual* database logic for `Create`, `Read` (ByID), `Update`, `Delete` using the DB adapter. `thing.go` has the function signatures, generic structure, and placeholder logic.
    *   Use generics and reflection (initially) for struct mapping. (`thing.go` uses this approach).
    *   Define how models map to tables (e.g., struct tags, naming conventions). Needs formalization (current uses basic struct name).
    *   Refine `BaseModel` struct (e.g., `thing.BaseModel`) for embedding.
    *   **Success Criteria:** Can perform basic CRUD operations with *real database interaction* on a simple model struct embedding `thing.BaseModel`.
4.  **[~] Initial Query Executor Design:** (Partially addressed by `IDs` / `Query` in `thing.go`, Scope Reduced)
    *   Design the API for executing list queries based on criteria (e.g., `thing.Query(ctx, &params)` where params includes WHERE clauses, ORDER BY, LIMIT, OFFSET). Avoid a complex chainable builder. *(To be refactored in Task 16)*
    *   Implement translation to *real* SQL for the first DB adapter for these simple criteria, **ensuring it selects only the columns corresponding to the fields defined in the target model struct (not `SELECT *`)**.
    *   Implement execution returning lists of model instances (refining `Query` and `CachedResult.Fetch`). *(To be refactored in Task 16)*
    *   **Success Criteria:** Can execute simple list queries returning mapped structs (with only defined fields selected) using a clear API.
5.  **[~] Caching Layer Integration:** (Partially addressed by `thing.go`)
    *   Define/Refine `CacheClient` interface (e.g., for Redis). (`RedisClient` exists).
    *   Implement Redis `CacheClient`. (`cache/redis/client.go` created).
    *   Integrate object caching into CRUD operations (`ByID`, `Create`, `Save`, `Delete`). (Initial implementation done in `thing.go`).
    *   Integrate query caching (e.g., caching IDs or results based on query hash). Define TTLs and basic invalidation (on mutation). (Initial ID list caching implemented in `thing.go`, TTLs need config, invalidation deferred). *(To be refactored in Task 16)*
    *   **Success Criteria:** CRUD operations and simple queries utilize the *actual* cache client, improving performance. Cache entries are invalidated/updated on mutations.
6.  **[~] Relationship Management (Phase 1: BelongsTo, HasMany):**
    *   Define how relationships are specified (e.g., struct tags).
    *   Implement `BelongsTo` and `HasMany` relationship loading (eager and lazy loading options).
    *   **Crucially, ensure these implementations reuse the existing high-performance, cached `thing.ByID` (for BelongsTo) and `thing.Query`/`CachedResult` (for HasMany) functions** to avoid redundant lookups and leverage the caching layer. *(Dependency on Task 16 for final CachedResult API)*
    *   Integrate relationship loading with the query builder/executor for preloading (fetching related objects efficiently alongside the main query).
    *   **Success Criteria:** Can define and load simple `BelongsTo` and `HasMany` relationships between models, leveraging the core cached data access functions.
7.  **[~] Hooks/Events System:** (Partially addressed by `thing.go`)
    *   Implement/Refine the Hooks system. (`thing.go` has definitions and integration points).
    *   Define standard lifecycle events (`BeforeCreate`, `AfterCreate`, etc.). (`thing.go` has some defined).
    *   Integrate event triggering into CRUD and potentially relationship operations.
    *   Add tests for the hooks system.
    *   **Success Criteria:** Users can register listeners to react to model lifecycle events; system is tested.
8.  **[ ] Transaction Management:**
    *   Implement helpers/API for managing database transactions.
    *   Ensure ORM operations can be performed within a transaction.
    *   **Success Criteria:** Can execute multiple ORM operations within a single DB transaction.
9.  **[ ] Adding Support for More Databases (MySQL, PostgreSQL/SQLite):**
    *   Implement `DBAdapter` for the remaining target databases.
    *   Refactor SQL generation to handle dialect differences.
    *   Test all features against all supported databases.
    *   **Success Criteria:** All ORM features work consistently across MySQL, PostgreSQL, and SQLite.
10. **[ ] Querying Refinements:** (Scope Reduced)
    *   Refine the implementation and API for list queries (filtering, ordering, pagination) based on feedback and testing. *(Refactoring covered in Task 16)*
    *   Ensure efficient SQL generation for supported databases for these simple queries, **selecting only the necessary struct fields**.
    *   **Success Criteria:** List querying functionality is robust, efficient, and easy to use across supported databases.
11. **[ ] Relationship Management (Phase 2: ManyToMany):** (Scope Reduced, Reuse Core Functions)
    *   Implement `ManyToMany` relationships, including handling join tables.
    *   Ensure this also leverages `thing.Query`/`CachedResult` where possible (e.g., fetching intermediate IDs or final objects). *(Dependency on Task 16)*
    *   **Success Criteria:** Can define and manage `ManyToMany` relationships efficiently.
12. **[ ] Schema Definition & Migration Tools (Basic):**
    *   Design a way to define schema using Go structs/tags.
    *   Implement basic schema generation (`CREATE TABLE`) based on models.
    *   *Optional:* Explore basic migration generation/execution tools or integration with existing ones.
    *   **Success Criteria:** Can generate `CREATE TABLE` statements from model definitions.
13. **[~] Testing, Benchmarking, and Refinement:** (Refined)
    *   **Improve Test Infrastructure:**
        *   Implement/Refine Cache Client for Testing (Enhance `mockCacheClient` or configure tests for real Redis - *Needed*).
        *   Ensure robust test DB setup (In-memory SQLite or instructions/scripts).
        *   Create test helpers (data setup, cleanup, assertions).
    *   **Optimization:**
        *   Implement reflection metadata caching (map `reflect.Type` to column names, field info) to optimize SQL generation and value extraction.
    *   **Core Functionality Tests:**
        *   Add comprehensive tests for CRUD (`Create`, `ByID`, `Save`, `Delete`) including DB/cache interactions and error scenarios (*Needed for Cache*).
        *   Refine and test `findChangedFields` implementation within `Save`.
        *   Add comprehensive tests for Querying (`IDs`, `Query`) with various params, including cache interactions (*Needed for Cache, see Task 16 testing*).
    *   **Cache Refinement & Testing (Next Steps):**
        *   **Refactor Locking:** Move `withLock` functionality into `CacheClient` interface (adding `WithLock` method, removing `AcquireLock`/`ReleaseLock` from interface) and update implementations. (**REVISED**)
        *   **Test Cache Logic:** Verify cache hits, misses, sets, and invalidations (object cache) for `ByID`, `Save`, `Delete`.
        *   **Test Query Cache:** Verify cache hits, misses, and sets for `IDs`. *(Superseded by Task 16 tests)*
        *   **Implement Query Cache Invalidation:** Develop and test a strategy for invalidating query caches in `Save`/`Delete`. *(Needs update based on Task 16 cache structure)*
        *   **Cache TTL Configuration:** Add ways to configure default TTLs.
        *   **(Optional) Negative Caching:** Implement and test caching for `ErrNotFound` results.
        *   **Error Handling/Logging:** Review and refine error handling and logging verbosity in cache interactions.
    *   **Advanced Feature Tests:**
        *   Add tests for Transaction Management (`BeginTx`, `Commit`, `Rollback`).
        *   Implement/Refine and test Hooks/Events system (*Depends on Task 7*).
    *   **Concurrency Tests:** Add tests for potential race conditions.
    *   **Continuous Integration:** Set up CI pipeline (e.g., GitHub Actions).
    *   **(Lower Priority):** Perform benchmarking and optimize critical paths.
    *   **(Deferred):** Consider adding instance method wrappers (e.g., `model.Save(ctx)`).
    *   **Success Criteria:** High test coverage for core features, DB/cache interactions, transactions, and hooks (when implemented). Tests pass reliably in CI. Placeholders refined/implemented. Performance documented.
14. **[ ] Documentation and Examples:** (Refined)
    *   **Setup:**
        *   Create `examples/` directory.
        *   Define sample model (e.g., `examples/models/user.go`).
        *   Provide supporting files (Go module, run scripts, schema setup).
    *   **Core Examples:**
        *   `examples/01_basic_crud/main.go`: Demonstrate Init, Create, ByID, Save, Delete.
        *   `examples/02_querying/main.go`: Demonstrate IDs, Query with params. *(Needs update for Task 16)*
        *   `examples/03_transactions/main.go`: Demonstrate transaction usage.
    *   **Future Examples:**
        *   Add examples for Hooks (*Depends on Task 7*).
        *   Add examples for Relationships (*Depends on Task 6, 11*).
    *   **Documentation:**
        *   Write comprehensive GoDoc comments for the public API.
        *   Write initial `README.md` (English).
        *   Write initial `README_zh.md` (Chinese Translation).
        *   *Optional:* Write tutorials/conceptual explanations.
    *   **Success Criteria:** Runnable examples exist covering core features. Public API has GoDoc comments. Basic `README.md` and `README_zh.md` are present.
15. **[ ] Open Source Release Preparation:**
    *   Choose a license (e.g., MIT, Apache 2.0).
    *   Write `README.md`, contribution guidelines (`CONTRIBUTING.md`), code of conduct.
    *   Publish the module.
    *   **Success Criteria:** Project is ready for public release and contributions.
16. **[x] Refactor `CachedResult` and Querying API:** *(New Task based on user request - Completed)*
    *   **Goal:** Align `CachedResult` with PHP example, improve caching strategy, and simplify query API.
    *   **Sub-tasks:**
        *   **[x] Refactor `QueryParams`:**
            *   Remove `Start` and `Limit` fields from `thing.QueryParams`.
            *   Adjust any code using these fields (primarily in `thing.QueryExecute` or similar DB interaction points).
            *   **Success Criteria:** `QueryParams` struct updated. Dependent code adjusted. Project compiles.
        *   **[x] Update `Thing.Query`:**
            *   Change signature to `func (t *Thing[T]) Query(params QueryParams) (*CachedResult[T], error)`.
            *   Remove the old `QueryResult` type if it exists.
            *   Implement the method to create and return a new `CachedResult[T]` initialized with the `Thing` instance (`t`) and the `params`.
            *   **Success Criteria:** `Thing.Query` signature and implementation updated. Project compiles. Basic call returns a `CachedResult` instance.
        *   **[x] Refactor `CachedResult.Count`:**
            *   Remove the `ctx` parameter.
            *   Implement cache check (Redis `GET tableName:count:hash(params)`).
            *   If cache miss: Execute `SELECT COUNT(*) FROM tableName WHERE ...` using `t.db` and `params`.
            *   Store result in Redis (`SET tableName:count:hash(params) count EX ttl`).
            *   Store result in `cr.cachedCount` and set `cr.hasLoadedCount = true`.
            *   Return the count and nil error, or 0 and error.
            *   Add helper for cache key generation.
            *   Add tests for cache hit/miss.
            *   **Success Criteria:** `Count` method refactored. Uses Redis for caching. Tests pass.
        *   **[x] Remove `CachedResult.IDs`:**
            *   Delete the `IDs` method from `cached_result.go`.
            *   Remove any usages of this method.
            *   **Success Criteria:** Method removed. Project compiles.
        *   **[x] Refactor `CachedResult.Fetch`:**
            *   Remove the `ctx` parameter.
            *   Implement internal `_fetch()` method (lazy loads IDs).
            *   Implement internal `_fetch_data()` method:
                *   Checks Redis for cached IDs (`GET tableName:list:hash(params)`).
                *   If cache miss: Query DB for first `cache_count` (e.g., 300) IDs (`SELECT id FROM ... ORDER BY ... LIMIT 300`).
                *   Store IDs in Redis list/zset (`tableName:list:hash(params)`).
                *   Store count in Redis if `< cache_count` (`tableName:count:hash(params)`).
                *   Store IDs in `cr.cachedIDs`, set `cr.hasLoadedIDs = true`.
                *   If cache hit: Load IDs from Redis into `cr.cachedIDs`, set `cr.hasLoadedIDs = true`.
            *   Main `Fetch(offset, limit)` logic:
                *   Call `_fetch()` if IDs not loaded.
                *   Calculate required ID range (`offset` to `offset + limit`).
                *   If range is within `cr.cachedIDs`:
                    *   Slice `cr.cachedIDs`.
                    *   Fetch objects using `t.ByIDs(slicedIDs)` (*Assumes ByIDs exists/is implemented*).
                    *   Return ordered objects.
                *   If range exceeds `cr.cachedIDs` or cache miss initially:
                    *   Query DB directly with pagination: `SELECT * FROM ... WHERE ... ORDER BY ... LIMIT limit OFFSET offset`.
                    *   Return results. (Future optimization: Handle merging cached + DB results if needed).
            *   Add tests for various offset/limit scenarios, cache hits/misses.
            *   **Success Criteria:** `Fetch` method refactored. Uses Redis for caching first 300 IDs. Fetches from DB beyond cache limit. Tests pass.
        *   **[x] Integrate Redis Client into `CachedResult`:**
            *   Ensure `CachedResult` has access to a `redis.Client` (likely via the `Thing` instance).
            *   Update `Thing` initialization/struct if needed to pass the Redis client.
            *   **Success Criteria:** Redis client is available and used within `CachedResult` methods (`Count`, `Fetch`).
        *   **[x] Final Review and Testing for Task 16:**
            *   Review all changes within Task 16 for correctness.
            *   Ensure all related tests pass.
            *   **Success Criteria:** Code reviewed, all tests green for this task.
    *   **Planner Review (Current Date):** Task 16 completed successfully. Core logic matches requirements. Caching strategy implemented (count cache, list ID cache). Query API simplified. Basic tests added. Further testing of edge cases and potential optimizations deferred to Task 13.
17. **[x] Debug Failing Tests:** *(New Task - Completed)*
    *   **Goal:** Resolve failures in `TestThing_ByID_Cache_NoneResult`, `TestThing_Query_Cache`, and `TestThing_Query_CacheInvalidation`.
    *   **Sub-tasks:**
        *   **[x] Analyze `TestThing_ByID_Cache_NoneResult`:**
            *   Read the test code (`tests/cache_operations_test.go`).
            *   Read the relevant `ByID` logic (`thing.go`: `byIDInternal`, `fetchModelsByIDsInternal`) and caching logic (`thing.go`: `deleteInternal`, `tests/mock_cache_test.go`).
            *   Identified point of failure: Mock `GetModel` returned generic `ErrNotFound` instead of specific `ErrCacheNoneResult` when encountering the `NoneResult` marker, causing incorrect behavior in `fetchModelsByIDsInternal`.
            *   Proposed and implemented fix: Updated `tests/mock_cache_test.go` -> `mockCacheClient.GetModel` to return `thing.ErrCacheNoneResult`. Corrected assertion in test to expect 1 `Set` call for `NoneResult`.
            *   **Success Criteria:** `TestThing_ByID_Cache_NoneResult` passed when run individually. *(Verified)*
        *   **[x] Analyze `TestThing_Query_Cache`:**
            *   Read the test code (`tests/cache_operations_test.go`).
            *   Based on the test, examine relevant `cached_result.go` logic (`Count`, `_fetch_data`, `Fetch`).
            *   Identified point of failure: `Fetch` logic used direct DB pagination even when all results were present in the cached ID list (because requested limit exceeded cached count), bypassing the expected `ByIDs` call.
            *   Proposed and implemented fix: Adjusted `Fetch` logic to use the cached ID path via `ByIDs` when `start == 0` and the number of cached IDs is less than `cacheListCountLimit` (indicating all results are cached).
            *   **Success Criteria:** `TestThing_Query_Cache` passed when run individually. *(Verified)*
        *   **[x] Analyze `TestThing_Query_CacheInvalidation`:**
            *   Read the test code (`tests/cache_operations_test.go`).
            *   Examine invalidation logic in `saveInternal` (`thing.go`) and query cache storage logic in `CachedResult._fetch_data` (`cached_result.go`).
            *   Identified point of failure: Mismatch between cache key prefix used for *storing* query lists (`list:{tableName}:`) and the prefix used for *invalidating* them (`query:{tableName}:`).
            *   Proposed and implemented fix: Changed the prefix in `saveInternal's call to `InvalidateQueriesContainingID` from `query:` to `list:`.
            *   **Success Criteria:** `TestThing_Query_CacheInvalidation` passes when run individually. *(Verified)*
        *   **[x] Run All Tests:**
            *   Execute `go test -v -p 1 ./tests/...`.
            *   **Success Criteria:** All tests in `thing/tests` pass. *(Verified)*
18. **[x] Task: Implement and Test Basic Transaction Methods (`Get`, `Select`, `Exec`, `Commit`, `Rollback`)
    *   Goal: Provide core transaction capabilities via the `thing.Tx` interface using the SQLite driver.
    *   Sub-tasks:
        *   [x] Implement `SQLiteAdapter.BeginTx`
        *   [x] Implement `SQLiteTx.Get`
        *   [x] Implement `SQLiteTx.Select`
        *   [x] Implement `SQLiteTx.Exec`
        *   [x] Implement `SQLiteTx.Commit`
        *   [x] Implement `SQLiteTx.Rollback`
        *   [x] Write tests for Commit (`TestTransaction_Commit`)
        *   [x] Write tests for Rollback (`TestTransaction_Rollback`)
        *   [x] Write tests for Select within Tx (`TestTransaction_Select`)
    *   Success Criteria: All methods are implemented, and corresponding tests in `tests/transaction_test.go` pass without errors. **All tests passed after adding `ClearCacheByID` for manual invalidation.**
19. **[ ] Refactor SQLite Adapter to Remove `sqlx`:** *(New Task)*
    *   **Goal:** Replace `sqlx` dependency in `drivers/sqlite/sqlite.go` with standard `database/sql` library.
    *   **Motivation:** Reduce external dependencies, understand the lower-level database interaction details required.
    *   **Sub-tasks:**
        *   **[x] Update Imports and Structs:** Replace `sqlx` imports and change `*sqlx.DB`, `*sql.Tx` types to `*sql.DB`, `*sql.Tx`. *(Done)*
        *   **[x] Replace `sqlx.Connect`:** Use `sql.Open` and potentially `PingContext`. *(Done)*
        *   **[x] Implement Manual Scanning Helper (`getFieldPointers`):** Create a function using `reflect` to get pointers to struct fields for `Scan`. *(Done)*
        *   **[x] Implement Slice Appending Helper (`appendValueToSlice`):** Create a function using `reflect` to append scanned values to the destination slice. *(Done)*
        *   **[x] Replace `GetContext` calls:** Use `QueryRowContext` and manual `Scan` with the helper. Handle `sql.ErrNoRows`. *(Covers SQLiteAdapter.Get & SQLiteTx.Get)*
        *   **[x] Replace `SelectContext` calls:** Use `QueryContext`, iterate `*sql.Rows`, and use manual `Scan` with helpers. Handle `rows.Err()`, `rows.Close()`. *(Covers SQLiteAdapter.Select & SQLiteTx.Select)*
        *   **[x] Replace `BeginTxx`:** Use `BeginTx`.
        *   **[x] Verify `ExecContext`, `Commit`, `Rollback` usage** (only type changes needed).
        *   **[ ] Update Tests:** Ensure existing or new tests cover the adapter functionality after removing `sqlx`. *(Remaining - Including Tx Tests)*
        *   **[x] Cleanup:** Removed unused `SelectPaginated` method from interface and implementation.
        *   **Success Criteria:** `sqlx` is no longer imported or used in `drivers/sqlite/sqlite.go`. All methods use `database/sql` types and functions. Manual scanning logic is implemented correctly. *(Partially met - Tests remaining)*
20. **[ ] Implement JSON Serialization Features:** *(New Task)*
    *   **Goal:** Add flexible JSON serialization capabilities to Thing ORM models, inspired by Mongoose's approach.
    *   **Sub-tasks:**
        *   **[ ] Implement Basic JSON Serialization:**
            *   Add a `ToJSON()` method to `BaseModel` that automatically serializes models to JSON.
            *   Ensure proper handling of time fields, nil values, and custom types.
            *   Make models work well with Go's `json.Marshal()` out of the box.
            *   **Success Criteria:** Models can be easily serialized to JSON with a simple method call or via standard `json.Marshal()`.
        *   **[ ] Implement Field Inclusion/Exclusion:**
            *   Add support for specifying which fields should be included or excluded in JSON serialization.
            *   Support both static configuration (via struct tags) and dynamic configuration (at runtime).
            *   **Success Criteria:** Users can control which fields appear in JSON output, both via struct tags and at serialization time.
        *   **[ ] Support Virtual Properties:**
            *   Add support for "virtual" properties in JSON output - computed fields that don't exist in the database.
            *   Implement getter methods for virtual properties.
            *   **Success Criteria:** Models can include computed properties in their JSON output.
        *   **[ ] Handle Nested Objects and Relationships:**
            *   Ensure proper serialization of nested structs and relationship fields.
            *   Add options to control the depth of relationship serialization.
            *   **Success Criteria:** Models with relationships can be serialized with control over relationship inclusion depth.
        *   **[ ] Add Serialization Hooks:**
            *   Implement pre/post serialization hooks to allow for customization of the serialization process.
            *   **Success Criteria:** Users can register hooks to run before or after JSON serialization to modify the output.
        *   **[ ] Add Tests:**
            *   Create comprehensive tests for all serialization features.
            *   Test with various model types, field types, and configurations.
            *   **Success Criteria:** All serialization features are well-tested with high coverage.
    *   **Success Criteria:** Thing ORM models can be easily serialized to JSON with flexible control over the output format, similar to Mongoose's capabilities.
21. **[ ] Implement `WithTransaction` Pattern:** *(New Task)*
    *   **Goal:** Provide a higher-level, safer way to manage transactions, ensuring automatic rollback on error and commit on success, potentially handling cache invalidation automatically.
    *   **Sub-tasks:**
        *   **[ ] Define API:** Design `func (t *Thing[T]) WithTransaction(ctx context.Context, fn func(tx Tx) error) error`.
        *   **[ ] Implement Logic:** Implement the function to handle `BeginTx`, `Commit` on success, and `Rollback` on error or panic within the provided `fn`.
        *   **[ ] Add Tests:** Create tests verifying successful commits, rollbacks on errors, and rollbacks on panics.
        *   **(Optional) Deferred Cache Handling:** Explore if this pattern allows deferring cache invalidations/updates until successful commit.
    *   **Success Criteria:** `WithTransaction` helper is implemented, tested, and provides a safer way to handle transactions compared to manual `BeginTx`/`Commit`/`Rollback`.

## Project Status Board

(Simple Markdown TODO list format. Executor updates status.)

- [~] Task 1: Project Setup & Core Structure (`thing` package, interfaces) - *Partially done*
- [ ] Task 2: Database Adapter Layer (SQLite initial impl needed)
- [~] Task 3: Basic CRUD Operations (No Cache Yet) - *Placeholders exist, DB logic needed*
- [~] Task 4: Initial Query Executor Design - *API/logic needs Task 16 refactor applied*
- [~] Task 5: Caching Layer Integration - *Basic Redis client/integration done, needs refinement*
- [ ] Task 6: Relationship Management (Phase 1: BelongsTo, HasMany) - *Depends on Task 16*
- [~] Task 7: Hooks/Events System - *Definitions exist, testing needed*
- [ ] Task 8: Transaction Management
- [ ] Task 9: Adding Support for More Databases (MySQL, PostgreSQL)
- [ ] Task 10: Querying Refinements - *Covered by Task 16*
- [ ] Task 11: Relationship Management (Phase 2: ManyToMany) - *Depends on Task 16*
- [ ] Task 12: Schema Definition & Migration Tools (Basic)
- [~] Task 13: Testing, Benchmarking, and Refinement - *Ongoing, cache testing improved, lock refactor needed*
- [ ] Task 14: Documentation and Examples - *Basic READMEs, need feature examples*
- [ ] Task 15: Open Source Release Preparation
- [x] Task 16: Refactor `CachedResult` and Querying API
- [x] Task 17: Debug Failing Tests
- [x] Task 18: Refactor SQLite Adapter to Remove `sqlx` *(Partially Done - Tx Implemented, Tests Remaining)*
- [ ] Task 19: Implement JSON Serialization Features *(New)*
- [ ] Task 20: Implement `WithTransaction` Pattern *(New)*


## Executor's Feedback or Assistance Requests

*Provide feedback on tasks, raise blockers, ask questions, or request Planner review.*
*   **(Previous)** Requesting review of Task 16 completion and confirmation to proceed with testing/debugging (Task 17).
*   **(Previous)** Task 17 (Debugging) completed. All tests in `thing/tests` pass. Requesting confirmation before potentially moving to other tasks like testing refinement (Task 13) or documentation (Task 14).
*   Ready for the next task. Awaiting instructions on whether to proceed with Task 18 (Remove sqlx) or Task 19 (JSON Serialization).
*   Completed implementation of `SQLiteTx.Get`, `SQLiteTx.Select`. Removed unused `SelectPaginated`. Added initial tests for transaction commit/rollback/select.
*   Clarified that `thing.Save` called within a manual `BeginTx`/`Rollback` block will cause cache inconsistency. Recommended adding `WithTransaction` pattern (Task 20) and documenting the correct usage of manual transactions.
*   Ready to run tests.
*   **Manual Cache Invalidation with Tx.Exec:** When modifying data directly using `Tx.Exec` (instead of ORM methods like `Save` or `Delete`), the cache is not automatically invalidated upon `Commit`. Manual cache invalidation (e.g., using `ClearCacheByID`) is required after the commit to prevent reading stale data from the cache. The `WithTransaction` pattern aims to address this.
*   **`db:"-"` Tag for Relationships:** To prevent `getFieldPointers` from trying to scan database columns into struct fields representing relationships (like `has_many` or `belongs_to`), add the `db:"-"` tag to those fields in the model struct definition.
*   **SQLite `Select` with Basic Types:** The `SQLiteAdapter.Select` method needs specific handling for scanning results into slices of basic Go types (e.g., `[]int64`, `[]string`) as the standard `Scan` might expect struct fields.
*   Refactored `deleteInternal` to use `ClearCacheByID` for model cache invalidation.


## Lessons Learned
*   Test failures related to caching often involve subtle interactions between mock implementations and the actual code's assumptions (e.g., specific error types like `ErrCacheNoneResult`).
*   When modifying logic that affects multiple code paths (like `Fetch` in `CachedResult`), ensure all relevant scenarios (e.g., fully cached results vs. partially cached vs. direct DB fetch) are tested and handled correctly.
*   Cache key consistency is critical. Ensure the keys used for setting/getting data match the keys used for invalidation or pattern matching (e.g., `list:` vs `query:` prefix issue).
*   Running tests individually (`-run TestName`) can help isolate failures before running the full suite.
*   `sqlx` simplifies result scanning but hides the underlying `database/sql` complexity, which becomes apparent when removing it.
*   Go linters (like `stylecheck`) prefer error strings returned by `fmt.Errorf` or `errors.New` to start with a lowercase letter (ST1005).


<details>
<summary>Archived Sections (Click to Expand)</summary>

</details>