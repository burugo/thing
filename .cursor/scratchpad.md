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

## Executor's Feedback or Assistance Requests

- Awaiting user confirmation to proceed with execution.

# Lessons

- After structural refactoring, documentation and lint checks must be synchronized to avoid omissions and technical debt.
- For large projects, it is recommended to address style and complexity issues in phases, prioritizing functional and interface consistency.
- The number of fields returned by SHOW INDEX may differ between MySQL versions/drivers. It is recommended to use information_schema.statistics for accurate retrieval.
- For multi-database testing, it is recommended to start the services locally first. In CI, use docker-compose to ensure a consistent environment.
- mnd.ignored-numbers in golangci-lint config must be an array of strings, not numbers.
- depguard.ignore-internal is not a supported option in golangci-lint v1.64.8; remove it to avoid schema errors.

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
