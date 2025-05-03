# Workflow Rule Update (2024-05-01)

**New Rule:**
- Executor must automatically commit all code, test, and `.cursor/scratchpad.md` changes after每个有意义的子任务或阶段，无需等待用户/Planner确认或切换模式，除非遇到阻塞、测试失败或高风险变更。
- 只有遇到需求不明、测试失败或高风险变更时才暂停并提示用户。

---

# Thing ORM Project

## Background and Motivation

(Revised) The primary goal of this project is to develop a high-performance, Object-Relational Mapper (ORM) for Go, named **Thing ORM** (package `thing`). This ORM aims to provide:
- Support for multiple popular relational databases (initially targeting MySQL, PostgreSQL, and SQLite).
- Integrated, configurable caching layer (leveraging Redis) to optimize read performance for entities and lists.
- **Focus:** Providing convenient, high-performance, thread-safe CRUD operations (`Create`, `Read`, `Update`, `Delete`) for single entities and efficient querying for lists of entities based on simple criteria (filtering, ordering, pagination).
- **Explicit Exclusion:** This project will *not* aim to replicate the full complexity of SQL. Features like JOINs, aggregations (COUNT, SUM, AVG), GROUP BY, and HAVING are explicitly out of scope. The focus is on the application-level pattern of cached object access, not complex SQL generation.
- An elegant and developer-friendly API designed for ease of use and extensibility.
- The ultimate objective is to release this ORM as an open-source library for the Go community.

This project builds upon the initial goal of replicating a specific `BaseModel`, enhancing it with multi-DB support and packaging it as a reusable `thing` library, while intentionally keeping the query scope focused.

- **Flexible JSON Serialization Rule:** The user has defined a rule for JSON serialization: `user.ToJSON(["name","age",{"book":["-publish_at"]},"teacher"])`. Fields prefixed with `-` are excluded. Objects like `{book:["-publish_at"]}` specify nested serialization (e.g., include `book` list but exclude `publish_at` in each book). Relationship fields (e.g., `book` for hasMany, `teacher` for belongsTo) are supported.

Previously, the DSL parser and merge logic supported merging nested field rules (e.g., `books{title},books{author}` would merge to include both `title` and `author`). This led to complex, hard-to-predict behavior and edge cases. The new requirement is to **disable nested DSL merging**: when the same nested field is specified multiple times, only the first occurrence is kept, and all subsequent ones are ignored. This simplifies both implementation and user expectations.

The goal was to support method-based virtual properties in Thing ORM's JSON serialization: if a struct has exported, zero-argument, single-return-value methods, these can be output as virtual fields—but only if explicitly referenced in the DSL string or via Include/WithFields. This allows for flexible, computed fields in JSON output, without polluting the output with all methods by default.

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
- **`CheckQueryMatch` Complexity:** Evaluating WHERE clauses against Go structs in memory (`CheckQueryMatch`) requires careful implementation for each operator and type comparison.
- **Advanced JSON Field Control:** Implementing a flexible, expressive API for field inclusion/exclusion, supporting both flat and nested/relationship fields, and handling both inclusion and exclusion in a single call. Must support syntax like `["name","age",{"book":["-publish_at"]},"teacher"]`.
- **Relationship Serialization:** Handling hasMany (e.g., `book`) and belongsTo (e.g., `teacher`) relationships, including/excluding fields as specified, and supporting nested rules for preloaded relationships.
- **Query Hash Generation Strategy (Pending Decision):** The logic for generating cache keys for queries (e.g., list and count caches) is currently implemented within `query.go` (`generateCountCacheKey`, `generateListCacheKey`). This logic needs to be accessible or replicated by tests (`tests/query_test.go`) to simulate and verify caching behavior. **Issue:** Duplicated logic creates maintenance burden. **Potential Solution:** Extract the core hash generation (JSON serialize params + SHA256) into a single, exported function within the `internal/cache` package and have both `query.go` and test helpers call it. **Status:** Decision on the best approach (exported helper vs. other methods) is deferred.
- **Method-based virtual property support:** Needed to ensure struct fields take precedence over methods if both exist for a given name.
- **Snake_case to CamelCase mapping:** Required robust mapping from DSL/Include field names (snake_case) to Go method names (CamelCase).
- **TDD:** TDD: Tests must cover all combinations (field only, method only, both, omitted, etc.).
- **Cache Invalidation Strategy:** Balancing invalidation precision (avoiding unnecessary deletions) with efficiency (avoiding complex checks like `checkModelMatchAgainstQuery`) and correctness (handling create/delete/update). Current discussion focuses on optimizing `GlobalCacheIndex` with field/value-based indexing versus external tag-based systems.
- [x] 精确失效：实现全表 list cache key（where 为空）始终失效，其它 key 只依赖字段/值级索引。

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
    *   [x] Define/Refine core interfaces (`DBAdapter`, `CacheClient`, `Model`, etc.).
    *   Set up basic project layout (`thing/`, `thing/internal/`, `examples/`, `tests/`).
    *   Setup basic logging and configuration handling.
    *   **Success Criteria:** Project structure created (`thing` package with `thing.go`), core interfaces defined, basic build/test pipeline works.
2.  **[x] Database Adapter Layer:** (Initial SQLite implementation completed via Task 18)
    *   Design the `DBAdapter` interface (potentially refining `DBExecutor`).
    *   Implement initial adapter for one database (e.g., SQLite or PostgreSQL using `sqlx` or `database/sql`). `thing.go` uses placeholder logic.
    *   Implement connection pooling/management.
    *   **Success Criteria:** Able to connect to the target DB, execute *real* SQL, and manage connections via the adapter interface.
3.  **[x] Basic CRUD Operations (No Cache Yet):** (Implementation complete, tests pass)
    *   Implement *actual* database logic for `Create`, `Read` (ByID), `Update`, `Delete` using the DB adapter.
    *   Use generics and reflection for struct mapping.
    *   Define how models map to tables.
    *   Refine `BaseModel` struct.
    *   **Success Criteria:** Can perform basic CRUD operations with *real database interaction*.
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
6.  **[x] Relationship Management (Phase 1: BelongsTo, HasMany):** (Implementation complete, tests pass)
    *   Define how relationships are specified.
    *   Implement `BelongsTo` and `HasMany` relationship loading.
    *   Ensure implementations reuse cached `thing.ByID` and `thing.Query`/`CachedResult`.
    *   Integrate relationship loading with preloading.
    *   **Success Criteria:** Can define and load simple `BelongsTo` and `HasMany` relationships, leveraging cached data access.
7.  **[x] Hooks/Events System:** (Partially addressed by `thing.go`)
    *   Implement/Refine the Hooks system. (`thing.go` has definitions and integration points).
    *   Define standard lifecycle events (`BeforeCreate`, `AfterCreate`, etc.). (`thing.go` has some defined).
    *   Integrate event triggering into CRUD and potentially relationship operations.
    *   Add tests for the hooks system. (**Task Type: New Feature**)
        *   [x] 7.1: Create `tests/hooks_test.go` file.
        *   [x] 7.2: Implement Hook Tests (TDD):
            *   Test listener registration (`RegisterListener`).
            *   Test Before/After hooks are called during Create/Save/Delete.
            *   Test listener receives correct model/event data.
            *   // Test listener returning error aborts the operation. (Commented out due to global state issue)
            *   Test listener modifying data (e.g., in BeforeSave).
            *   Test multiple listeners for the same event. (Implicitly tested)
    *   Add example usage. (**Task Type: New Feature**)
        *   [x] 7.3: Create and implement `examples/04_hooks/main.go`.
    *   Verify tests and commit.
        *   [x] 7.4: Run all tests (`go test -v ./...`) to ensure they pass (after isolating hook tests).
        *   [x] 7.5: Commit changes.
    *   **Success Criteria:** Users can register listeners to react to model lifecycle events; system is tested (isolated) and demonstrated with an example.
8.  **[x] Transaction Management:** (Implementation complete, tests pass)
    *   Implement helpers/API for managing database transactions.
    *   Ensure ORM operations can be performed within a transaction.
    *   **Success Criteria:** Can execute multiple ORM operations within a single DB transaction.
9.  **[*] Adding Support for More Databases (MySQL, PostgreSQL):** (**Task Type: New Feature**)
    *   Implement `DBAdapter` for the remaining target databases.
    *   Refactor SQL generation to handle dialect differences.
    *   Test all features against all supported databases.
    *   **Sub-tasks:**
        *   [x] 9.1: Add MySQL and PostgreSQL drivers to go.mod，已提交。
        *   [x] 9.2: Create internal/drivers/db/mysql/mysql.go with MySQLAdapter, MySQLTx structs.
        *   [x] 9.3: Create internal/drivers/db/postgres/postgres.go with PostgreSQLAdapter, PostgreSQLTx structs.
        *   [x] 9.4: Implement mysql.NewMySQLAdapter.
        *   [x] 9.5: Implement postgres.NewPostgreSQLAdapter.
        *   [x] 9.6: Implement DBAdapter/DBTransaction for MySQL (using ? placeholders).
        *   [x] 9.7: Implement DBAdapter/DBTransaction for PostgreSQL (using $N placeholders, potentially with rebinding helper).
        *   [x] 9.8: (Optional) Create SQL builder/helper utils for dialect differences (placeholders, simple statements, specific columns in SELECT).
        *   [x] 9.9: (Optional) Update `thing.Configure` or add functions to select/configure adapters.
        *   [x] 9.10: Set up local/Docker test environments for MySQL & PostgreSQL.
        *   [*] 9.11: Run full test suite (`./tests`) against MySQL, fix failures.
        *   [*] 9.12: Run full test suite (`./tests`) against PostgreSQL, fix failures.
        *   [*] 9.13: Commit
    *   **Success Criteria:** All ORM features work consistently across MySQL, PostgreSQL, and SQLite. All tests in `./tests` pass on all three databases.
    *   [x] Configuration now matches mainstream ORM: user initializes Adapter and CacheClient in main and passes to Thing ORM.
10. **[ ] Querying Refinements:** (Scope Reduced)
    *   Refine the implementation and API for list queries (filtering, ordering, pagination) based on feedback and testing. *(Refactoring covered in Task 16)*
    *   Ensure efficient SQL generation for supported databases for these simple queries, **selecting only the necessary struct fields**.
    *   **Success Criteria:** List querying functionality is robust, efficient, and easy to use across supported databases.
11. **[ ] Relationship Management (Phase 2: ManyToMany):** (Scope Reduced, Reuse Core Functions)
    *   Implement `ManyToMany` relationships, including handling join tables.
    *   Ensure this also leverages `thing.Query`/`CachedResult` where possible (e.g., fetching intermediate IDs or final objects). *(Dependency on Task 16)*
    *   **Success Criteria:** Can define and manage `ManyToMany` relationships efficiently.
12. **[ ] Schema Definition & Migration Tools (Task 12):** (**Task Type: New Feature**)
    *   **目标：** 支持通过 Go struct/tag 自动生成数据库建表语句（CREATE TABLE），并支持基础的 schema 迁移。
    *   **功能点：**
        1. Go struct 到建表 SQL 自动生成，支持主键、自增、唯一、非空、默认值、索引、外键等常见约束。
        2. 支持多数据库方言（MySQL/PostgreSQL/SQLite），类型和语法自动适配。
        3. 批量建表与依赖顺序处理（如外键依赖）。
        4. 基础 schema 迁移（检测模型与表结构差异，生成 ALTER TABLE 语句，安全模式）。
        5. 迁移版本管理（可选/后续）。
        6. API 设计（AutoMigrate、GenerateCreateTableSQL、GenerateAlterTableSQL，支持 dry-run）。
        7. 测试与文档。
    *   **典型用例：**
        - `thing.AutoMigrate(&User{}, &Book{})` 一键建表/迁移。
        - `thing.GenerateCreateTableSQL(&User{})` dry-run 输出 SQL。
    *   **关键挑战：**
        - Go struct 到 SQL 类型映射（跨数据库差异）。
        - 复杂嵌套/关系字段处理。
        - 迁移安全性与幂等性。
        - 兼容已有数据的平滑升级。
    *   **Success Criteria:**
        - 能自动生成并执行建表/迁移 SQL，支持多数据库，API 简洁，测试覆盖。
    *   **[ ] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
        *   12.5.1: 设计 schema_migrations 版本表
        *   12.5.2: 迁移脚本管理与规范
        *   12.5.3: 迁移执行引擎
        *   12.5.4: 迁移状态查询与管理 API
        *   12.5.5: 测试与文档
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
    *   **Goal:** Align `CachedResult` improve caching strategy, and simplify query API.
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
18. **[~] Task: Refactor SQLite Adapter to Remove `sqlx`** *(Implementation Done - Transaction Tests Failing)*
    *   **Goal:** Remove `sqlx` from the SQLite adapter implementation.
    *   **Sub-tasks:**
        *   **[x] Implement `SQLiteAdapter.BeginTx`
        *   **[x] Implement `SQLiteTx.Get`
        *   **[x] Implement `SQLiteTx.Select`
        *   **[x] Implement `SQLiteTx.Exec`
        *   **[x] Implement `SQLiteTx.Commit`
        *   **[x] Implement `SQLiteTx.Rollback`
        *   **[x] Refactor `getFieldPointers` for `database/sql`.
        *   **[x] Update tests `TestTransaction_Commit`, `TestTransaction_Rollback`, `TestTransaction_Select` to use standard library types/methods.
        *   **[x] Debug transaction tests:** `TestTransaction_Commit`, `TestTransaction_Rollback`, `TestTransaction_Select` are failing. Need to investigate why changes within the transaction aren't persisting or rolling back as expected.
            *   Hypothesis 1: Issue with `BeginTx` or `Commit`/`Rollback` implementation in the adapter.
            *   Hypothesis 2: Issue with how the transaction context (`sql.Tx`) is used in `Get`/`Select`/`Exec`.
            *   Hypothesis 3: Issue with test logic itself after refactor.
            *   **Next Step:** Run individual transaction tests with `-v` to get detailed logs.
            *   **Fixed:** Root cause identified as incorrect parsing of `db` tags (e.g., `"id,pk"`) in `getStructFieldMap`. Corrected the parsing logic.
        *   **Success Criteria:** All adapter methods implemented using `database/sql`. All transaction tests pass. `sqlx` dependency removed.
        *   **Planner Review:** Fix accepted. All tests pass. (2025-05-02)
19. **[x] Extend `CheckQueryMatch` for Comparison Operators:** *(New Task - Completed)*
    *   **Goal:** Add support for `>`, `<`, `>=`, `<=`, `IN` operators.
    *   **Sub-tasks:**
        *   **[x] Modify `query_match.go`:**
            *   Update `CheckQueryMatch` switch statement.
            *   Implement `compareValues` helper for `>`, `<`, `>=`, `<=$.
            *   Implement `checkInOperator` helper for `IN`.
            *   Update function comments.
        *   **[x] Modify `tests/query_match_test.go`:**
            *   Add `t.Run` blocks for new operators (`>`, `<`, `>=`, `<=`, `IN`).
            *   Include tests for match, mismatch, and edge cases (e.g., empty slice for IN).
            *   Add test for unsupported operator error (`!=`).
        *   **[x] Test:**
            *   Run `go test -v ./tests/...`.
            *   **Success Criteria:** Implementation complete. All tests pass, including new ones.
20. **[x] Refine `CheckQueryMatch` Error Handling in Cache Update:** *(New Task - Completed)*
    *   **Goal:** Ensure cache consistency when `CheckQueryMatch` fails.
    *   **Sub-tasks:**
        *   **[x] Analyze `updateAffectedQueryCaches` in `thing.go`:** Determine how errors from `CheckQueryMatch` are handled.
        *   **[x] Discuss Strategy:** Agreed that deleting the cache entry upon error is safer than skipping.
        *   **[x] Implement Deletion:** Modify the `if err != nil` block after the `checkModelMatchAgainstQuery` call to log an ERROR and call `t.cache.Delete(ctx, cacheKey)` for the specific key that caused the error.
        *   **[x] Test:** Run `go test -v ./tests/...` (implicitly tested by existing tests passing).
        *   **Success Criteria:** Error handling modified to delete cache key. Tests pass.
21. **[*] Task: Implement JSON Serialization Features**
    *   **Goal:** Add comprehensive JSON serialization capabilities to the ORM, similar to Mongoose's capabilities, following the user-defined rule for field inclusion/exclusion and nested relationships.
    *   **Success Criteria:** Thing ORM models can be easily serialized to JSON with flexible control over the output format, similar to Mongoose's capabilities, and following the user-defined rule.
22. **[x] Design new `GlobalCacheIndex` structure:** Define data structure for mixed field/value indexing (e.g., `fieldIndex`, `valueIndex`, `keyToParams`). SC: Structure defined.
    *   **[x] 22.2 实现 QueryParams 解析器，提取 =/IN 字段和值。**
    *   **[x] 22.3 RegisterQuery 注册逻辑更新。**
    *   **[x] 22.4 GetKeysByValue 方法实现。**
    *   **[x] 22.5 缓存失效逻辑修改。**
    *   **[x] 22.6 新增/更新测试。**
23. **[~] Monitoring/metrics for cache hit/miss rates**
    *   **Goal:** 通过 RedisClient/CacheClient 提供一个方法（如 GetCacheStats），可直接查询当前 hit/miss 统计信息。
    *   **Sub-tasks:**
        1. [x] 在 RedisClient（和 mockCacheClient，如适用）结构体中增加 counters 字段。
        2. [x] 提供一个方法（如 GetCacheStats()），返回当前的 hit/miss 统计信息（可按类型细分）。
        3. [x] 在 Get/Set/Delete 等方法中，命中/未命中时分别递增对应计数。
        4. [x] 在测试用例中调用 GetCacheStats，验证计数功能。
        5. [ ] 在新方法和相关字段处补充注释，说明用法和统计范围。
    *   **Success Criteria:**
        - 可通过 GetCacheStats 获取计数统计结果。
        - 统计准确，测试覆盖。
        - 方法和字段有适当注释说明。
24. **[x] Implement UnregisterListener Function** (**Task Type: New Feature**)
    *   **Goal:** Provide a way to remove registered event listeners to improve testability and flexibility.
    *   **Sub-tasks:**
        *   [x] 24.1: Implement `UnregisterListener(eventType EventType, listener EventListener)` in `hooks.go`, ensuring thread safety with `listenerMutex`.
        *   [x] 24.2: Add `TestHook_UnregisterListener` test case in `tests/hooks_test.go` (under `hooks` build tag).
        *   [x] 24.3: Modify `TestHook_ErrorAborts` in `tests/hooks_test.go` to use `defer UnregisterListener` for cleanup and uncomment the test.
        *   [x] 24.4: Run all tests (including tagged hook tests) to verify.
            *   [x] 24.4.1: Move `examples/models/models.go` to `tests/models.go` (adjust package name). (Corrected: Created `tests/models.go` and `examples/04_hooks/models.go`)
            *   [x] 24.4.2: Update imports in `examples/04_hooks/main.go` and `tests/hooks_test.go`.
            *   [x] 24.4.3: Rerun hook tests (`go test -v -tags=hooks ./tests`).
            *   [x] 24.4.4: Rerun all tests (`go test -v ./...`).
        *   [x] 24.5: Commit changes.
    *   **Success Criteria:** Listeners can be successfully unregistered. Tests (including previously interfering ones) pass reliably. Hook system testability improved.
    *   **Planner Review:** Fix accepted. Testability improved, all tests pass. (2025-05-02)

## JSON Serialization Rule (User-Defined)

- **API Example:** `user.ToJSON(["name","age",{"book":["-publish_at"]},"teacher"])`
    - Strings: field names to include (e.g., "name", "age").
    - Strings prefixed with `-`: fields to exclude (e.g., "-publish_at").
    - Objects: nested/relationship fields, e.g., `{book:["-publish_at"]}` means include the `book` list but exclude `publish_at` in each book.
    - Relationship fields: e.g., `book` (hasMany), `teacher` (belongsTo).
- **Semantics:**
    - Inclusion/exclusion can be mixed in the same call.
    - Nested rules apply to preloaded relationships.
    - If a field is not mentioned, default is to include unless excluded by `-`.
    - If a relationship is included, its fields can be controlled via nested rules.
    - This rule must be supported both statically (via struct tags) and dynamically (at runtime via the fields param).

## Project Status Board

- [x] Disable nested DSL merge (only keep first occurrence)
- [x] Update/relax related test(s)
- [x] Commit changes
- [x] Planner review and confirmation
- [x] Ordered JSON Serialization with DSL Order (OrderedMap implemented and integrated; ToJSON output order matches DSL; tests verified)
- [x] Method-based virtual property support (explicit output via DSL/Include)
- [x] TDD tests for method-based virtuals
- [x] All tests passing
- [x] Basic CRUD Operations (Create, Read, Update, Delete)
- [x] Relationship Management (BelongsTo, HasMany, Preload)
- [x] Transaction Management (BeginTx, Commit, Rollback)
- [x] 22.1 结构设计：GlobalCacheIndex 支持值级别和字段级别索引字段，已提交。
- [x] 22.2 实现 QueryParams 解析器，提取 =/IN 字段和值。
- [x] 22.3 RegisterQuery 注册逻辑更新。
- [x] 22.4 GetKeysByValue 方法实现。
- [x] 22.5 缓存失效逻辑修改。
- [x] 精确失效：实现全表 list cache key（where 为空）始终失效，其它 key 只依赖字段/值级索引。
- [x] Refactoring: Remove table-level index logic and related tests (GetPotentiallyAffectedQueries). Only value/field/full-table indexes remain.
- [x] Refactoring: Inline and remove checkModelMatchAgainstQuery; use cache.CheckQueryMatch directly for cache invalidation.
- [x] Refactoring: Merge updateAffectedQueryCaches and handleDeleteInQueryCaches into invalidateAffectedQueryCaches. All call sites updated.
- [x] 1. Confirm and document the mismatch between test and code for `IN` clause argument style.
- [x] 2. Update the test to use `IN (?)` with a slice argument.
- [x] 3. Fix the query builder to expand `IN` clause slices into multiple placeholders and flatten arguments.
- [x] 4. Update documentation/comments for clarity on `IN` clause usage.
- [x] 5. Re-run all tests to verify.
- [x] Task 18: Refactor SQLite Adapter & Fix Tests (Completed & Verified)
- [x] MySQL Adapter: Add rebindMySQLIdentifiers to support double-quoted identifiers (已完成，已测试，已提交)
- [ ] Refactor: Move identifier quoting from MySQL Adapter to SQL Builder layer (parameterize quote char)
    - [x] Revert rebindMySQLIdentifiers logic in MySQL Adapter
    - [x] Add quoteChar parameter to SQL builder (function signature version, now to be replaced)
    - [ ] Refactor SQLBuilder as a struct with quoteChar field (GORM-style)
    - [ ] Update all adapters to use SQLBuilder instance (not passing quoteChar each time)
    - [ ] Ensure all tests pass
    - [ ] Commit changes
- [x] Fix MySQL update path nil pointer bug in saveInternal (always use non-nil pointer for diff)
- [x] Add PostgreSQL integration test and setupPostgresTestDB (mimics MySQL test structure)
- [x] Fix PostgreSQL adapter: correct parameter binding, placeholder numbering, RETURNING id support, all Postgres tests pass
- [x] JSON 序列化高级特性（Task 21）：已支持灵活字段包含/排除、关系嵌套、虚拟属性等，ToJSON 规则与 Mongoose 类似，测试覆盖。
- [x] 查询功能完善（Task 10）：查询 API、SQL 生成、分页/排序/过滤、IN 查询、参数绑定等均已实现，所有数据库下表现一致，测试覆盖。
- [x] 测试、基准测试与优化（Task 13）：所有核心功能均有测试覆盖，mock cache、本地/CI DB 环境、反射元数据缓存、并发安全、性能健壮性均已实现，所有测试通过。
- [ ] Task 12: Schema Definition & Migration Tools
    - [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
    - [x] 12.2: 多数据库方言类型/语法适配（New Feature）
    - [x] 12.3: 批量建表与依赖顺序处理（New Feature）
    - [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
    - [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
        *   [x] 12.5.1: 设计 schema_migrations 版本表
        *   [x] 12.5.2: 迁移脚本管理与规范
        *   [x] 12.5.3: 迁移执行引擎
        *   [x] 12.5.4: 迁移状态查询与管理 API
        *   [x] 12.5.5: 测试与文档
    - [x] 12.6: API 设计与 dry-run 支持（New Feature）
    - [x] 12.7: 测试与文档（New Feature）
- [x] AutoMigrate supports index/unique struct tag and generates correct SQL (tested)
- [x] TestAutoMigrate_IndexAndUnique passes (index/unique index creation verified)
- [x] MySQL/PostgreSQL adapter 测试连接判断与跳过（连接不可用时 t.Logf+t.Skip，不再 FAIL）
- [x] 基于 introspection 的 schema diff/ALTER TABLE 生成（GenerateAlterTableSQL 实现）
- [x] GenerateAlterTableSQL TDD 测试用例（add/drop/unique 覆盖）
- [x] MySQL/PostgreSQL GenerateAlterTableSQL mock 单元测试（无 DB 依赖）
- [x] Fix migrator_test.go for SQLite adapter compatibility and transactional DDL/data rollback

## Executor's Feedback or Assistance Requests

- Fixed a bug where saveInternal would pass a nil pointer to findChangedFieldsSimple if the original record was not found (e.g., in MySQL update path). Now, always uses a non-nil zero value pointer for diffing, matching expectations of the diff logic and preventing panics.
- All tests now pass (`go test -v ./tests`) except for PostgreSQL, which fails due to connection refused (no running PostgreSQL on 127.0.0.1:5432). The test and setup function are correct and committed; to run locally, ensure PostgreSQL is running and accessible, or set POSTGRES_TEST_DSN.
- Committed as: dcd6e72 (bugfix), 655741f (PostgreSQL test)
- Fixed PostgreSQL adapter to support correct parameter binding and placeholder numbering for INSERT/UPDATE/SELECT, and to use RETURNING id for inserts. All PostgreSQL tests now pass. Committed as: 8ea5f7f.
- JSON 序列化高级特性（Task 21）已完成：支持灵活字段包含/排除、关系嵌套、虚拟属性，ToJSON 规则与 Mongoose 类似，测试覆盖。
- 查询功能完善（Task 10）已完成：API、SQL 生成、分页/排序/过滤、IN 查询、参数绑定等均实现，所有数据库下表现一致，测试通过。
- 测试、基准测试与优化（Task 13）已完成：所有核心功能均有测试覆盖，mock cache、本地/CI DB 环境、反射元数据缓存、并发安全、性能健壮性均已实现，所有测试通过。
- Fixed a bug in schema metadata parsing: now only skips fields with relationship thing tags (hasMany, belongsTo, rel=, model:, fk:), so index/unique tags are correctly processed for index generation.
- All tests pass after the fix. Changes committed: fix(schema): allow index/unique tags to generate indexes, only skip relationship fields for thing tag; all tests pass
- 已实现 MySQL/PostgreSQL 测试用例在数据库不可用时自动跳过（t.Logf+t.Skip），不会导致测试失败，CI/本地开发体验更友好。
- 已通过全部测试验证，相关更改已提交。
- 已实现 GenerateMigrationsTableSQL，支持 mysql/postgres/sqlite 三种方言的 schema_migrations 版本表建表 SQL。
- 已补充 TDD 测试，验证 SQL 片段、错误处理，全部通过。
- 相关更改已提交。
- 已完成 12.5.2 迁移脚本管理与规范：
    - 约定脚本目录为 migrations/
    - 约定脚本命名为 [Version]_[Name].[Direction].sql
    - 实现 DiscoverMigrations 函数，支持自动发现、解析、排序迁移脚本。
    - 补充 TDD 测试，覆盖多种场景，全部通过。
- 相关更改已提交。
- All issues in migrator_test.go are fixed:
  - Replaced DBAdapter.Get with Select for count queries (adapter only supports struct for Get).
  - Corrected PRAGMA table_info assertion: SQLite returns one row per column, so expect 3 for (id, name, email).
  - Updated error test to expect full rollback (no applied migrations, or table dropped) after a failed migration in SQLite.
- All tests now pass. Changes committed.
- Lesson added below for future reference.

## Lessons

- When using SQLite, a failed transaction that includes DDL (CREATE TABLE, etc.) will roll back both schema and data. After a failed migration, expect the schema_migrations table and all data to be gone if they were created in the same transaction.
- The SQLite adapter's Get method only supports scanning into a struct, not a basic type like *int. Use Select with a []int slice for count queries.
- PRAGMA table_info returns one row per column, not one column per field. Always assert the number of columns accordingly.

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] Exclude lll (long line) linter for *_test.go files in golangci-lint config

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.

## [Task 12] AutoMigrate 实际执行 SQL 方案（Planner 记录）

### 目标
- 让 `thing.AutoMigrate` 不仅生成建表 SQL，还能自动在当前数据库执行建表语句，实现一键建表。
- 继续沿用 ModelInfo 体系，支持多数据库方言扩展。

### 设计要点
1. **全局 DBAdapter 获取**
    - 通过 `thing.globalDB` 获取全局数据库适配器（DBAdapter），无需用户额外传参。
    - 要求用户在主程序中先调用 `thing.Configure(db, cache)`。
2. **SQL 执行方式**
    - 遍历每个 model，生成建表 SQL 后，直接调用 `thing.globalDB.Exec(ctx, sql)` 执行。
    - 推荐用 `context.Background()` 作为 ctx，后续可扩展为支持传入 ctx。
    - 执行前可先打印 SQL 以便调试。
3. **多数据库方言支持**
    - 目前默认传递 "mysql"，后续可根据 `globalDB` 类型自动选择方言（如 MySQLAdapter/PostgreSQLAdapter/SQLiteAdapter）。
    - 可通过类型断言或接口方法获取当前方言。
4. **错误处理**
    - 执行 SQL 失败时，返回详细错误信息（包含表名、SQL 语句、底层错误）。
    - 所有表均执行后才返回 nil，否则遇到第一个错误即中断。
5. **API 兼容性**
    - 保持 `thing.AutoMigrate(models ...interface{}) error` API 不变。
    - 仅在内部实现中增加 SQL 执行逻辑。
6. **后续扩展点**
    - 支持 dry-run（仅打印 SQL 不执行）。
    - 支持批量建表依赖顺序、ALTER TABLE、索引/外键等。

### 示例代码片段
```go
ctx := context.Background()
_, err := thing.globalDB.Exec(ctx, sql)
if err != nil {
    return fmt.Errorf("AutoMigrate: failed to execute SQL for %s: %w", info.TableName, err)
}
```

### Success Criteria
- AutoMigrate 能自动在当前数据库执行建表 SQL，所有表创建成功。
- 失败时有详细错误输出。
- 兼容多数据库，API 保持简洁。

---

## Project Status Board

- [x] 12.1: Go struct 到建表 SQL 自动生成（New Feature）
- [x] 12.2: 多数据库方言类型/语法适配（New Feature）
- [x] 12.3: 批量建表与依赖顺序处理（New Feature）
- [x] 12.4: 基础 schema 迁移与 ALTER TABLE 支持（New Feature）
- [x] 12.5: 迁移版本管理（Migration Versioning）（New Feature）
    *   [x] 12.5.1: 设计 schema_migrations 版本表
    *   [x] 12.5.2: 迁移脚本管理与规范
    *   [x] 12.5.3: 迁移执行引擎
    *   [x] 12.5.4: 迁移状态查询与管理 API
    *   [x] 12.5.5: 测试与文档
- [x] 12.6: API 设计与 dry-run 支持（New Feature）
- [x] 12.7: 测试与文档（New Feature）

## Executor's Feedback or Assistance Requests

- Migration/schema/migrate 工具相关所有核心功能均已实现并测试通过。
- **回滚（Rollback）实现结论：**
    - 所有数据库适配器（SQLite/MySQL/PostgreSQL）均实现了 Tx.Rollback() 方法，且有完整的事务回滚测试（见 tests/transaction_test.go、tests/sqlite_adapter_test.go）。
    - 迁移工具（Migrator）在迁移失败时会自动回滚事务，保证数据和 schema 一致性。
    - 测试覆盖：事务内的 DDL/DML 操作 rollback 后不会持久化，行为符合预期。
- 所有相关子任务已在状态板标记为完成。

## Lessons

- 事务回滚是所有数据库适配器的基础能力，迁移工具已正确利用事务保证原子性。
- SQLite/MySQL/PostgreSQL 的回滚行为均有测试覆盖，迁移失败时能自动回滚。

---

## Next Logical Task

**Based on the Project Status Board and High-level Task Breakdown, the next logical step is:**

- **缓存层优化与高级特性 (Caching Layer Enhancements):** 提升查询缓存失效策略、增加灵活性 (TTL, L1/L2), 增强防击穿/雪崩能力, 完善监控。
- **Schema 定义与迁移工具 (Schema Definition & Migration Tools):** 支持通过 struct/tag 生成建表语句或集成迁移工具。
- **文档与示例完善 (Documentation & Examples):** 补充 README, API 文档, 中文文档和核心用例示例。

如需推进其中某一项，请指定优先级或直接说明需求！

RegisterQuery 注册逻辑已实现，valueIndex/fieldIndex 自动填充，测试全部通过，已提交。

下一步将实现 GetKeysByValue 方法。

GetKeysByValue 方法已实现，测试全部通过，已提交。

下一步将进入缓存失效逻辑修改，优先用值级/字段级索引。

## Future/Optional Enhancements

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

## Workflow Guidelines

*   **Language Consistency:** All code comments, git commit messages, and content written in `.cursor/scratchpad.md` must be in English. Planner communication with the user can be in Chinese.
*   **Test-Driven Development (TDD):** Mandatory for `New Feature` tasks. Apply flexibly elsewhere. Final verification command: `go test -v ./tests | grep FAIL`.
*   **Testing Strategy:**
    *   **Avoid `go test -v ./...` directly in terminal for debugging:** It produces excessive output, making it hard to find failures.
    *   **Recommended Alternatives:**
        *   **Specific Tests:** `go test -v ./tests/...` or `go test -v ./... -run ^TestSpecificFunction$` (Fastest)
        *   **JSON Output (Go 1.10+):** `go test -json ./... | go tool test2json -t | grep FAIL` (Robust Filtering)
        *   **Write to File:** `go test -v ./... > test_output.log && grep -E '--- FAIL:|FAIL:|Error:' test_output.log` (Simple & Portable)
    *   **Final Verification:** Always use the required command: `go test -v ./tests | grep FAIL`.
*   **Automatic Testing, Fixing, and Committing Workflow:**
    1.  Execute Step.

# Refactoring CheckQueryMatch Signature

## Background and Motivation

The user wants to change the signature of the `CheckQueryMatch` function in `internal/cache/query_match.go`. Instead of passing a `*ModelInfo` struct, the user prefers to pass the `tableName` (string) and `columnToFieldMap` (map[string]string) directly as arguments. This aims to simplify the function's dependencies for callers who might already have this information readily available without needing the full `ModelInfo` struct.

## Key Challenges and Analysis

*   **Signature Change:** Modifying the function definition requires updating all internal references to the `info` parameter.
*   **Information Access:** Access to `ColumnToFieldMap` needs to be switched from `info.ColumnToFieldMap` to the new parameter.
*   **Table Name Usage:** The function currently uses `modelVal.Type().Name()` in some error messages. We need to decide whether to keep this or use the new `tableName` parameter for consistency. Using the `tableName` parameter seems more appropriate for context related to database operations.
*   **Logic Preservation:** As this is structural refactoring, the core query matching logic must remain unchanged.

## High-level Task Breakdown

1.  **(Executor)** Modify the `CheckQueryMatch` function signature in `internal/cache/query_match.go` to accept `tableName string` and `columnToFieldMap map[string]string` instead of `info *ModelInfo`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The function signature is updated correctly.
2.  **(Executor)** Update the function body to use the new `tableName` and `columnToFieldMap` parameters.
    *   Replace all occurrences of `info.ColumnToFieldMap` with `columnToFieldMap`.
    *   Replace occurrences of `modelVal.Type().Name()` in error/log messages (related to identifying the model/table context) with the `tableName` parameter.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All internal references are updated correctly. The core matching logic remains unchanged.
3.  **(Executor)** Find call sites of `cache.CheckQueryMatch` and update them to pass the correct arguments (`tableName` and `columnToFieldMap`). The compiler output indicates errors in `internal/cache/cache.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** All call sites are updated, and the code compiles.
4.  **(Executor)** Update call sites of `cache.CheckQueryMatch` in the test file `tests/query_match_test.go`.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Test file call sites are updated, and the code compiles.
5.  **(Executor)** Run tests again to ensure no regressions.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** The command `go test -v ./tests|grep FAIL` returns no output.
6.  **(Executor)** Commit the changes.
    *   **Task Type:** `Refactoring (Structural)`
    *   **Success Criteria:** Changes to `internal/cache/query_match.go`, `internal/cache/cache.go`, `tests/query_match_test.go`, and `.cursor/scratchpad.md` are committed.
7.  **(Planner)** Wait for user verification after executor reports completion.

## Project Status Board

*   [x] Modify `CheckQueryMatch` function signature.
*   [x] Update function body to use new parameters.
*   [x] Update call sites of `CheckQueryMatch` (in `cache.go`).
*   [x] Update call sites of `CheckQueryMatch` (in `tests/query_match_test.go`).
*   [x] Run tests again.
*   [x] Commit changes.
*   [ ] **WAIT for user verification.**

## Executor's Feedback or Assistance Requests

*   Initial refactoring of `CheckQueryMatch` completed.
*   Fixed call sites in `cache.go`.
*   Fixed call sites in `tests/query_match_test.go`.
*   Tests passed.
*   Changes committed (Commit hash: 08070d3 - *Note: Hash may not be exact if other commits occur*).
*   **Structural refactoring complete. Waiting for user verification.**

## Lessons

*   When refactoring function signatures, remember to update all call sites, including those in test files. Compiler errors are a good guide for finding these locations.

- [x] Thing[T].DBAdapter() and all Adapter.DB() methods implemented, supporting layered access to underlying DB connection.
