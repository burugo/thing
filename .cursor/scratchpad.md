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
17. **[ ] Debug Failing Tests:** *(New Task)*
    *   **Goal:** Resolve failures in `TestThing_ByID_Cache_NoneResult` and `TestThing_Query_Cache`.
    *   **Sub-tasks:**
        *   **[ ] Analyze `TestThing_ByID_Cache_NoneResult`:**
            *   Read the test code (`thing/tests/thing_test.go`).
            *   Read the relevant `ByID` logic in `thing.go` and caching logic (`cache.go`, `cached_result.go`, `thing.go`).
            *   Identify the exact point of failure.
            *   Propose and implement a fix.
            *   **Success Criteria:** `TestThing_ByID_Cache_NoneResult` passes when run individually.
        *   **[ ] Analyze `TestThing_Query_Cache`:**
            *   Read the test code (`thing/tests/thing_test.go`).
            *   Based on the test, examine relevant `cached_result.go` logic (`Count`, `_fetch_data`, `Fetch`).
            *   Add logging if needed to trace execution and cache state.
            *   Propose and implement a fix.
            *   **Success Criteria:** `TestThing_Query_Cache` passes when run individually.
        *   **[ ] Run All Tests:**
            *   Execute `go test -v -p 1 thing/tests`.
            *   **Success Criteria:** All tests in `thing/tests` pass.

## Project Status Board

(Revised for Integration & Scope)

- [x] Project Setup & Core Structure (Done: Module init, `thing` pkg, `thing.go` rename, dirs, interfaces defined)
- [x] Database Adapter Layer (Initial - Done: SQLite adapter implemented with `sqlx`, including transaction support)
- [x] Basic CRUD Operations (Done: `Create`, `Update`, `Delete`, `ByID`, `Save` refactored to use DBAdapter)
- [x] Initial Query Executor Design (Done: `IDs`, `Query` refactored to use DBAdapter and SQL builder) *(Refactored in Task 16)*
- [x] Caching Layer Integration (Done: Redis Client impl; Object/query cache logic integrated; Basic cache interaction tests added) *(Query caching refactored in Task 16)*
- [x] Relationship Management (Phase 1: BelongsTo, HasMany) - *Note: Keep simple, reuse core funcs.* (Done: Implemented eager loading via `QueryParams.Preloads`. Requires testing.) *(Dependency on Task 16)*
- [~] Hooks/Events System (Partially done, needs testing/refinement)
- [x] Transaction Management (Done: Implemented in SQLite adapter)
- [ ] Adding Support for More Databases
- [ ] Querying Refinements (Scope Reduced) *(Refactored in Task 16)*
- [ ] Relationship Management (Phase 2: ManyToMany) - *Note: Keep simple, reuse core funcs.* *(Dependency on Task 16)*
- [ ] Schema Definition & Migration Tools (Basic)
- [~] Testing, Benchmarking, and Refinement (Partial: Initial test setup, basic cache tests added. *Refined plan added.*) *(Query cache tests superseded by Task 16)*
  - [~] Implement placeholders (`findChangedFields` in `Save` uses basic reflection, needs refinement & tests)
  - [x] Mock DB/Redis Tests (Done: Enhanced `mockCacheClient` in `tests/thing_test.go`)
  - [x] Replace DB Placeholders (`ByID`, `Create`, `Save`, `Delete`, `IDs` use adapter)
  - [ ] Cache TTL Configuration (*Next after Task 16?*)
  - [ ] Locking refinement (Using basic cache client lock methods)
  - [ ] Test Hooks/Events (*Depends on Task 7*)
  - [x] Add CRUD tests (Done: Basic object cache tests for ByID, Save, Delete)
  - [x] Add Querying tests (Done: Basic query cache tests for IDs) *(Superseded by Task 16)*
  - [ ] Add Transaction tests
  - [ ] Add Concurrency tests
  - [ ] Setup CI
- [x] Refactor `CachedResult` and Querying API (Task 16 - Done)
- [ ] Debug Failing Tests (Task 17 - In Progress)
  - [ ] Analyze `TestThing_ByID_Cache_NoneResult`
  - [ ] Analyze `TestThing_Query_Cache`
  - [ ] Run All Tests

## Executor's Feedback or Assistance Requests

*(No feedback yet)*

## Lessons

- Use `go test -v -p 1` to run tests sequentially and get verbose output, especially helpful for debugging race conditions or cache interactions.
- Use `go test -race` to detect race conditions.
- When refactoring caching logic (`CachedResult`), ensure both count and list cache keys are generated consistently and correctly (e.g., using normalized parameters and distinct prefixes).
- Remember to handle the `NoneResult` cache marker explicitly for both count and list caches to distinguish between "not cached yet" and "cached empty result".
- `json.Marshal` can fail, especially with complex types or pointers. Log errors and handle them gracefully (e.g., by skipping cache or returning an error).
- Normalizing query parameters (especially argument values) before hashing is crucial for cache key consistency.
- When fetching partial results (like the first N IDs for list caching), if the number of results fetched is less than the limit N, it implies the total result count is known. Use this opportunity to update the count cache.
- Include helpful debugging info in log messages (e.g., cache keys, number of items fetched/cached).
- Always read a file before attempting to edit it, especially when updating plans or complex code.
- Ask before using potentially destructive commands like `git push --force`.

<details>
<summary>Previous Session Logs</summary>

*(Logs from previous interactions, if any)*

</details>

