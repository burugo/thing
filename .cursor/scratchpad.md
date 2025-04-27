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
- **Caching Strategy & Invalidation:** Integrating caching effectively for both single objects and query results (lists of IDs/objects), while tackling cache invalidation for the supported operations.
- **Performance Optimization:** Balancing features with performance. Optimizing SQL generation *for simple queries*, minimizing reflection overhead, efficient caching, and potentially connection pooling.
- **API Design:** Creating an API that is intuitive, powerful for its scope, and idiomatic Go.
- **Schema Definition:** Providing clear patterns for defining database schemas in Go (likely via struct tags).
- **Concurrency Control:** Implementing safe concurrent operations.
- **Testing Complexity:** Requires testing against multiple database versions for the supported feature set.
- **Open Source Considerations:** Documentation, examples, contribution guidelines, licensing, and community building.

## Design Philosophy and API Goals

(Revised) The ORM should prioritize:

1.  **Performance:** Leverage Go's strengths and employ smart caching/querying for simple CRUD and list operations.
2.  **Developer Experience:** Provide a clean, intuitive, and well-documented API focused on the core use cases. User models will embed a `thing.BaseModel` struct.
3.  **Extensibility:** Allow users to customize behavior through hooks, custom types, or other extension points.
4.  **Database Agnosticism (for supported features):** Abstract database-specific details for basic CRUD and simple querying.
5.  **Robustness & Thread Safety:** Ensure correctness and safety through testing and sound design.
6.  **Pragmatic Caching:** Offer flexible caching options (object, query lists) with clear invalidation strategies.
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
    *   Design the API for executing list queries based on criteria (e.g., `thing.Query(ctx, &params)` where params includes WHERE clauses, ORDER BY, LIMIT, OFFSET). Avoid a complex chainable builder.
    *   Implement translation to *real* SQL for the first DB adapter for these simple criteria, **ensuring it selects only the columns corresponding to the fields defined in the target model struct (not `SELECT *`)**. 
    *   Implement execution returning lists of model instances (refining `Query` and `CachedResult.Fetch`).
    *   **Success Criteria:** Can execute simple list queries returning mapped structs (with only defined fields selected) using a clear API.
5.  **[~] Caching Layer Integration:** (Partially addressed by `thing.go`)
    *   Define/Refine `CacheClient` interface (e.g., for Redis). (`RedisClient` exists).
    *   Implement Redis `CacheClient`. (`cache/redis/client.go` created).
    *   Integrate object caching into CRUD operations (`ByID`, `Create`, `Save`, `Delete`). (Initial implementation done in `thing.go`).
    *   Integrate query caching (e.g., caching IDs or results based on query hash). Define TTLs and basic invalidation (on mutation). (Initial ID list caching implemented in `thing.go`, TTLs need config, invalidation deferred).
    *   **Success Criteria:** CRUD operations and simple queries utilize the *actual* cache client, improving performance. Cache entries are invalidated/updated on mutations.
6.  **[ ] Relationship Management (Phase 1: BelongsTo, HasMany):**
    *   Define how relationships are specified (e.g., struct tags).
    *   Implement `BelongsTo` and `HasMany` relationship loading (eager and lazy loading options).
    *   **Crucially, ensure these implementations reuse the existing high-performance, cached `thing.ByID` (for BelongsTo) and `thing.Query`/`CachedResult` (for HasMany) functions** to avoid redundant lookups and leverage the caching layer.
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
    *   Refine the implementation and API for list queries (filtering, ordering, pagination) based on feedback and testing.
    *   Ensure efficient SQL generation for supported databases for these simple queries, **selecting only the necessary struct fields**. 
    *   **Success Criteria:** List querying functionality is robust, efficient, and easy to use across supported databases.
11. **[ ] Relationship Management (Phase 2: ManyToMany):** (Scope Reduced, Reuse Core Functions)
    *   Implement `ManyToMany` relationships, including handling join tables.
    *   Ensure this also leverages `thing.Query`/`CachedResult` where possible (e.g., fetching intermediate IDs or final objects).
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
        *   Add comprehensive tests for Querying (`IDs`, `Query`) with various params, including cache interactions (*Needed for Cache*).
    *   **Cache Refinement & Testing (Next Steps):**
        *   **Test Cache Logic:** Verify cache hits, misses, sets, and invalidations (object cache) for `ByID`, `Save`, `Delete`.
        *   **Test Query Cache:** Verify cache hits, misses, and sets for `IDs`.
        *   **Implement Query Cache Invalidation:** Develop and test a strategy for invalidating query caches in `Save`/`Delete`.
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
        *   `examples/02_querying/main.go`: Demonstrate IDs, Query with params.
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

## Project Status Board

(Revised for Integration & Scope)

- [x] Project Setup & Core Structure (Done: Module init, `thing` pkg, `thing.go` rename, dirs, interfaces defined)
- [x] Database Adapter Layer (Initial - Done: SQLite adapter implemented with `sqlx`, including transaction support)
- [x] Basic CRUD Operations (Done: `Create`, `Update`, `Delete`, `ByID`, `Save` refactored to use DBAdapter)
- [x] Initial Query Executor Design (Done: `IDs`, `Query` refactored to use DBAdapter and SQL builder)
- [~] Caching Layer Integration (Done: Redis Client impl; Initial object/query cache logic added to core funcs; Needs testing & refinement)
- [ ] Relationship Management (Phase 1: BelongsTo, HasMany) - *Note: Keep simple, reuse core funcs.*
- [~] Hooks/Events System (Partially done, needs testing/refinement)
- [x] Transaction Management (Done: Implemented in SQLite adapter)
- [ ] Adding Support for More Databases
- [ ] Querying Refinements (Scope Reduced)
- [ ] Relationship Management (Phase 2: ManyToMany) - *Note: Keep simple, reuse core funcs.*
- [ ] Schema Definition & Migration Tools (Basic)
- [~] Testing, Benchmarking, and Refinement (Partial: Initial test setup with SQLite in-memory, basic ByID test. *Refined plan added.*)
  - [~] Implement placeholders (`findChangedFields` in `Save` uses basic reflection, needs refinement & tests)
  - [ ] Mock DB/Redis Tests (Needs improved Cache Mock/Implementation)
  - [x] Replace DB Placeholders (`ByID`, `Create`, `Save`, `Delete`, `IDs` use adapter)
  - [ ] Cache TTL Configuration (*Next*)
  - [ ] Locking refinement (Using basic cache client lock methods)
  - [ ] Implement reflection metadata caching
  - [ ] Test Hooks/Events (*Depends on Task 7*)
  - [ ] Add CRUD tests (*Next - Focus on Cache*)
  - [ ] Add Querying tests (*Next - Focus on Cache*)
  - [ ] Add Transaction tests
  - [ ] Add Concurrency tests
  - [ ] Setup CI
  - [ ] *(Deferred):* Instance method wrappers
- [ ] Documentation and Examples (*Refined plan added.*)
  - [ ] Create `examples/` dir & sample model
  - [ ] Add Basic CRUD example
  - [ ] Add Querying example
  - [ ] Add Transactions example
  - [ ] Write GoDocs
  - [ ] Write README.md
  - [ ] Write README_zh.md
- [ ] Open Source Release Preparation

## Executor's Feedback or Assistance Requests

- **2024-07-26:** Encountered persistent type mismatch errors in `thing/thing.go` when passing generic model pointers (`*T`) to functions expecting the `Model` interface (e.g., `cache.GetModel`, `db.Get`, `triggerEvent`).
- Attempts to pass the pointer directly or use type assertion `model.(Model)` were unsuccessful, resulting in linter errors like `*T does not implement Model` or `invalid operation: model (variable of type *T) is not an interface`.
- Requesting clarification on the exact signatures of the `Model` interface and relevant methods in `DBAdapter` and `CacheClient`.
- Need guidance on whether the function signatures, the use of generics, or the interface design needs adjustment to resolve these type compatibility issues.

### Lessons

- **2024-07-26:** In Go generics, a pointer to a type parameter (`*T`) is not automatically assignable to an interface type (`Model`) even if the type parameter `T` is constrained by that interface (`T Model`). Explicit handling or design adjustments are needed.

## Design Discussion: Resolving Generic Type Mismatches (2024-07-26)

**Problem:** Functions like `cache.GetModel`, `db.Get`, and `triggerEvent` expect a `Model` interface, but the generic functions (`ByID`, `Create`, etc.) provide `*T` (where `T` is constrained by `Model`). Go's type system prevents direct assignment/use, causing linter errors.

**Alternative Solutions Considered:**

1.  **Modify Interface Signatures:** Change `DBAdapter`/`CacheClient`/`triggerEvent` to accept `interface{}` and use internal reflection/type assertion. 
    *   *Pros:* Keeps core ORM functions generic.
    *   *Cons:* Shifts complexity, less type-safe at boundaries, requires interface definitions.
2.  **Helper Methods on `BaseModel`:** Add methods to `BaseModel` (e.g., `CacheSet`, `TriggerEvent`) that encapsulate the calls to cache/db/event functions, passing `self` (which implements `Model`). Generic functions call these helper methods on the model instance.
    *   *Pros:* Encapsulates logic, keeps core functions generic, potentially cleaner separation.
    *   *Cons:* Adds methods to `BaseModel`, requires careful invocation from generic functions (e.g., using `getBaseModelPtr`).
3.  **Non-Generic Core Functions:** Remove `[T Model]` from `ByID`, `Create`, etc. Use `interface{}` and heavy reflection.
    *   *Pros:* Avoids specific generic issue.
    *   *Cons:* Major design change, less type-safe, potentially slower, more verbose.
4.  **Pass `reflect.Type` and `interface{}`:** Modify adapter/cache functions to accept `reflect.Type` alongside `interface{}` pointer for results.
    *   *Pros:* Explicit type info for functions needing it.
    *   *Cons:* More complex signatures, still relies on reflection internally.

**Chosen Approach (2024-07-26):**

We will proceed with **Option 2 (Helper Methods on `BaseModel`)**.

**Implementation Plan:**

1.  **Add Helper Methods to `BaseModel`:**
    *   `triggerEventInternal(ctx, eventType, eventData)`: Calls global `triggerEvent` passing `b` (the `*BaseModel`).
    *   `cacheSetInternal(ctx, key, duration)`: Calls `b.cacheClient.SetModel` passing `b`.
    *   `cacheGetInternal(ctx, key)`: Calls `b.cacheClient.GetModel` passing `b` as destination.
    *   `cacheDeleteInternal(ctx, key)`: Calls `b.cacheClient.DeleteModel`.
    *   `dbGetInternal(ctx, query, args...)`: Calls `b.dbAdapter.Get` passing `b` as destination.
    *   These methods should include checks for nil clients (`b.cacheClient`, `b.dbAdapter`).
2.  **Modify Generic ORM Functions (`ByID`, `Create`, `Save`, `Delete`):**
    *   Use `getBaseModelPtr` to get the embedded `*BaseModel` (`bm`) from the generic model (`*T`).
    *   Replace direct calls to `triggerEvent`, `cache.SetModel`, `cache.GetModel`, `db.Get`, `cache.DeleteModel` with calls to the corresponding new helper methods on `bm` (e.g., `bm.triggerEventInternal(...)`, `bm.cacheSetInternal(...)`).
    *   Ensure `bm` has the necessary adapters set before calling DB/Cache helpers.

**Next Steps:**

- Executor will implement the changes outlined above in `thing.go`.

## `thing.py` Analysis and Feature Proposals

**(Kept Existing - Relevant for Feature Ideas)**

**Analysis of `thing.py` Features vs. Go BaseModel & ORMs:**

1.  **Relationships (`Relation`, `_fast_query`, `load_things`, `MultiRelation`):**
    *   `thing.py`: Implements a dynamic, cache-heavy system for relationships between different `Thing` types. `_fast_query` attempts to optimize fetching multiple relationships.
    *   `thing.go`: No explicit relationship handling yet.
    *   Typical ORMs (GORM, Ent): Offer structured relationship definitions (e.g., HasMany, BelongsTo via struct tags or schema), preload/eager-loading mechanisms, and handle foreign key constraints. Much more structured and integrated with the database schema.
    *   *Comparison:* `thing.py`'s approach is very flexible but complex and relies heavily on specific caching patterns (`_rel_cache`, `sgm`). ORMs provide a more conventional, database-centric approach.

2.  **Dynamic Properties / Schemaless (`_t` dict):**
    *   `thing.py`: Allows storing arbitrary key-value data within a `Thing` instance using the `_t` dictionary. `_essentials` defines required keys.
    *   `thing.go`: Uses statically typed Go structs.
    *   Typical ORMs: Primarily map struct fields to database columns. Some support mapping fields to JSON/JSONB database types, allowing for schemaless data storage within a structured column.
    *   *Comparison:* `thing.py` offers direct object-level flexibility. ORMs can achieve similar storage via JSON columns but access is less direct (e.g., `model.Data["key"]`).

3.  **Hooks/Callbacks (`hooks.get_hook("thing.commit")`):**
    *   `thing.py`: Uses a global hook system (`r2.lib.hooks`) triggered during `_commit`.
    *   `thing.go`: Has placeholders (`triggerEvent`) but no implemented system.
    *   Typical ORMs: Provide built-in lifecycle hooks (e.g., `BeforeSave`, `AfterCreate`, `BeforeUpdate`, `AfterDelete`) directly within the model definition or via interfaces.
    *   *Comparison:* ORM hooks are usually more tightly integrated with the model lifecycle. `thing.py` uses a more decoupled, global system.

4.  **Atomic Increment (`_incr`):**
    *   `thing.py`: Provides a method to atomically increment integer properties (`_ups`, `_downs`, or fields in `_t`), handling locks, cache updates (`update_from_cache`), and DB increments.
    *   `thing.go`: No equivalent function yet.
    *   Typical ORMs: Some offer ways to perform atomic updates (e.g., GORM's `Update("column", gorm.Expr("column + ?", 1))`).
    *   *Comparison:* `thing.py`'s `_incr` includes the full lock/cache logic specific to its architecture. ORM methods focus primarily on the DB update expression.

5.  **Search Indexing (`update_search_index`):**
    *   `thing.py`: Explicitly sends a message to an AMQP queue (`search_changes`) to trigger external search index updates.
    *   `thing.go`: No search integration.
    *   Typical ORMs: Don't typically handle search indexing directly; integration is usually done via hooks/callbacks or separate application logic.
    *   *Comparison:* This is an application-specific integration point.

6.  **Advanced Querying (`Things`, `Relations`, `MultiQuery`, `Merge`):**
    *   `thing.py`: Provides a custom query builder (`Query`, `Things`, `Relations`) with features like caching query results (list of fullnames/IDs), merging results from different queries (`Merge`), handling pagination (`_before`, `_after`), and fetching specific properties (`RelationsPropsOnly`).
    *   `thing.go`: Has basic `IDs` and `Query` functions, returning a `CachedResult` struct holding IDs.
    *   Typical ORMs: Offer sophisticated query builders allowing complex filtering, sorting, joining, grouping, and pagination, translating Go code into SQL. They usually return lists of model objects or specific selected fields.
    *   *Comparison:* `thing.py`'s query system is tailored to its specific architecture (fullname identifiers, separate caches). ORMs provide more general-purpose, SQL-centric query building.

7.  **Other Features:**
    *   `_by_fullname`: Lookup by a specific string identifier format. (Specific to `thing.py`)
    *   `_byID36`: Lookup by base36 ID. (Specific to `thing.py`)
    *   Specialized Sort Properties (`_hot`, `_score`, etc.): Calculated properties based on fields. (Application logic, not typically ORM base)
    *   `_delete` (for Relations): Specific logic for deleting relationships and clearing caches.

**Proposed New Features for `thing.go`:**

Based on the analysis, here are features we could consider adding, prioritizing usefulness and feasibility within our current Go structure:

1.  **Hooks/Events System:** (High Priority)
    *   Implement a proper event system (e.g., using interfaces or channels).
    *   Define standard lifecycle events: `BeforeCreate`, `AfterCreate`, `BeforeSave`, `AfterSave`, `BeforeDelete`, `AfterDelete`.
    *   Allow registering listeners to react to these events (e.g., for validation, triggering external actions like search indexing).
    *   *Benefit:* Increases extensibility and allows decoupling of concerns. Closer to standard ORM practice.

2.  **Atomic Increment (`Incr`):** (Medium-High Priority)
    *   Add a function `Incr[T Model](ctx context.Context, model *T, fieldName string, amount int64) error`.
    *   It should acquire a lock (using `withLock`), perform an atomic DB update (`UPDATE ... SET field = field + ? WHERE id = ?`), and update/invalidate the cache entry.
    *   May require reflection or modifying the `Model` interface to handle field access generically.
    *   *Benefit:* Provides a safe way to handle counters, a common requirement.

3.  **`UpdateOrCreate` Helper:** (Medium Priority)
    *   Add `UpdateOrCreate[T Model](ctx context.Context, queryParams QueryParams, attributesToSet map[string]interface{}) (*T, error)`.
    *   Internally uses `Query` (or `IDs`) to check existence based on `queryParams`.
    *   If exists, calls `Save` with the `attributesToSet`. If not, calls `Create` merging `queryParams` conditions and `attributesToSet`.
    *   *Benefit:* Common pattern for ensuring a record exists with certain attributes. Present in the original PHP `BaseModel`.

4.  **Schemaless Data Field (`Data`):** (Medium Priority - *If Needed*)
    *   Add an optional `Data map[string]interface{} `db:"data"` field to `BaseModel` (or suggest adding it to specific models).
    *   Requires the database column to be JSON/JSONB.
    *   Packing/unpacking needs to handle this field. `findChangedFields` needs to compare it.
    *   *Benefit:* Allows storing flexible, unstructured data if required by the application, similar to `thing.py`'s `_t`.

5.  **Basic Relationship Helpers:** (Low-Medium Priority)
    *   Add simple helper functions like:
        *   `LoadBelongsTo[T Model, R Model](ctx context.Context, owner *T, fkValue int64, relationPtr **R) error`
        *   `LoadHasMany[T Model, R Model](ctx context.Context, ownerID int64, relatedSlicePtr *[]R, foreignKeyName string) error`
    *   These would essentially use `ByID` or `Query` internally. No complex caching or `_fast_query` equivalent initially.
    *   *Benefit:* Provides convenience for common relationship loading patterns without full ORM complexity. 

# Thing ORM Development Scratchpad

## Background and Motivation

The project aims to develop a simple ORM-like library named "Thing" in Go, focusing on ease of use, basic CRUD operations, caching (object and query), and a clean interface. Recent work focused on fixing update logic bugs and managing dependencies. The next critical step is ensuring cache consistency by implementing query cache invalidation.

## Key Challenges and Analysis

*   **Query Cache Invalidation Complexity:** Precisely mapping model changes (Save/Delete) to specific affected query cache keys is difficult due to the opaque nature of the query hash.
*   **Performance vs. Correctness:** A precise invalidation strategy is performant but complex. An aggressive strategy (invalidate all queries for the table) is simpler and guarantees correctness but can negatively impact cache hit rates. We will start with the aggressive strategy.
*   **Cache Client Interface:** The `CacheClient` needs a method to delete keys by prefix/pattern, which must be implemented carefully in concrete adapters (especially Redis, using `SCAN` not `KEYS`).
*   **Testing Invalidation:** Verifying invalidation requires careful test setup to confirm cache misses after relevant operations.

## High-level Task Breakdown

1.  **Fix Update Logic Bug (COMPLETED)**
    *   Analyze `saveInternal` and `findChangedFieldsReflection`.
    *   Correct the logic for building the `UPDATE` SQL statement.
    *   Ensure `UpdatedAt` is handled correctly.
    *   **Success Criteria:** `TestThing_Save_Update` passes.

2.  **Manage Dependencies (COMPLETED)**
    *   Identify usage of `google/wire`.
    *   Remove the dependency from `go.mod` if not needed by core logic.
    *   Use build constraints (`//go:build ignore`) to keep `examples/wire.go` without making it a required dependency.
    *   **Success Criteria:** `go mod tidy` runs cleanly, `wire` dependency is removed, `examples/wire.go` remains but is ignored by default.

3.  **Implement Query Cache Invalidation (PLANNED)**
    *   **Task 3.1: Add Pattern Deletion to `CacheClient` Interface:**
        *   Define `DeleteByPrefix(ctx context.Context, prefix string) error` in `thing.CacheClient`.
        *   **Success Criteria:** Interface updated.
    *   **Task 3.2: Implement Pattern Deletion in `mockCacheClient`:**
        *   Implement `DeleteByPrefix` in `tests/thing_test.go`.
        *   **Success Criteria:** Mock implementation works correctly.
    *   **Task 3.3: Implement Pattern Deletion in Redis Cache:**
        *   Implement `DeleteByPrefix` in `thing/cache/redis/redis.go` using `SCAN`.
        *   **Success Criteria:** Redis adapter implements `DeleteByPrefix` via `SCAN`.
    *   **Task 3.4: Implement Invalidation Logic in `saveInternal`:**
        *   Call `DeleteByPrefix` after successful DB update, before lock release.
        *   **Success Criteria:** `saveInternal` calls invalidation logic.
    *   **Task 3.5: Implement Invalidation Logic in `deleteInternal`:**
        *   Call `DeleteByPrefix` after successful DB delete, before lock release.
        *   **Success Criteria:** `deleteInternal` calls invalidation logic.
    *   **Task 3.6: Add Tests for Query Cache Invalidation:**
        *   Create tests verifying query cache miss after Save/Delete.
        *   **Success Criteria:** Invalidation tests pass.

## Project Status Board

*   [x] Fix Update Logic Bug (`TestThing_Save_Update`)
*   [x] Remove `google/wire` dependency
*   [x] Implement Query Cache Invalidation
    *   [x] Task 3.1: Add `DeleteByPrefix` to `CacheClient` interface
    *   [x] Task 3.2: Implement `DeleteByPrefix` in `mockCacheClient`
    *   [x] Task 3.3: Implement `DeleteByPrefix` in Redis Cache (`SCAN`)
    *   [x] Task 3.4: Call invalidation from `saveInternal` (update path)
    *   [x] Task 3.5: Call invalidation from `deleteInternal`
    *   [x] Task 3.6: Write tests for query cache invalidation

## Executor's Feedback or Assistance Requests

*(Executor can add notes here)*

## Lessons Learned

*   Include info useful for debugging in the program output (e.g., detailed logging in `findChangedFieldsReflection`).
*   Read the file before you try to edit it (especially important for complex functions like `saveInternal`).
*   Always ask before using the `-force` git command (General Git safety).
*   `go mod tidy` adds dependencies based on *all* non-ignored `.go` files, even in `examples/`. Use build constraints like `//go:build ignore` to exclude files from dependency analysis while keeping them in the project.
*   When building SQL UPDATE statements from a map of changes, ensure the keys used to build the `SET` clause match the keys generated by the change detection logic (DB column names vs. Go field names). Iterating over the change map keys is safer than iterating over all possible fields.

## Current Status / Progress Tracking

Core logic for object caching (`ByID`, `Save`, `Delete`) and query ID caching (`IDs`, used by `Query`) has been integrated into `thing.go` using the `CacheClient` interface. The example application uses the Redis implementation. The next critical step is to refine and thoroughly test this caching functionality within `tests/thing_test.go`, including handling TTL configuration and query cache invalidation. 