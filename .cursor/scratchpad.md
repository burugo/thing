# Background and Motivation

The primary goal of this project is to develop a high-performance, Object-Relational Mapper (ORM) for Go, named **Thing ORM** (package `thing`). This ORM aims to provide:
- Support for multiple popular relational databases (initially targeting MySQL, PostgreSQL, and SQLite).
- Integrated, configurable caching layer (leveraging Redis) to optimize read performance for entities and lists.
- **Focus:** Providing convenient, high-performance, thread-safe CRUD operations (`Create`, `Read`, `Update`, `Delete`) for single entities and efficient querying for lists of entities based on simple criteria (filtering, ordering, pagination).
- **Explicit Exclusion:** This project will *not* aim to replicate the full complexity of SQL. Features like JOINs, aggregations (COUNT, SUM, AVG), GROUP BY, and HAVING are explicitly out of scope. The focus is on the application-level pattern of cached object access, not complex SQL generation.
- An elegant and developer-friendly API designed for ease of use and extensibility.
- The ultimate objective is to release this ORM as an open-source library for the Go community.

This project builds upon the initial goal of replicating a specific `BaseModel`, enhancing it with multi-DB support and packaging it as a reusable `thing` library, while intentionally keeping the query scope focused.


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
- [~] Testing, Benchmarking, and Refinement
- [x] Interface and Type Migration
- [x] Decoupling from internal dependencies
- [x] SQLBuilder/Dialector migration and dependency review
- [x] Move driver packages out of internal, into public drivers directory
- [x] Main package and drivers only depend on interfaces, never on each other's implementation
- [x] Documentation and README update

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

## Project Status Board

- [x] Fix golangci-lint config errors (mnd.ignored-numbers as string array, remove depguard.ignore-internal)
- [x] Ignore funlen and gocyclo (complexity) for all test files in golangci-lint config
- [x] Ignore errcheck (unchecked error returns) for all test files in golangci-lint config
- [x] Ignore lll (long line linter) for all test files in golangci-lint config
- [x] 修复 JSON DSL 解析器，移除无用 [] 分支，恢复 -field 排除逻辑，所有相关测试通过
- [x] Remove ineffectual assignment to fkFieldFound in preloadBelongsTo and preloadHasMany (relationships.go)
- [x] Refactor if-else chain to switch in preloadManyToMany (relationships.go, line 541)
- [x] Remove ineffectual assignment to placeholders in saveInternal else branch (crud.go)
- [x] Move type declarations out of generic function in cache.go for Go 1.18 compatibility

## Executor's Feedback or Assistance Requests

- Fixed golangci-lint config: changed mnd.ignored-numbers to a string array ['0', '1', '2', '3'] and removed unsupported depguard.ignore-internal option. Verified with golangci-lint config verify (exit code 0). Committed and pushed to github_main branch. Ready for CI re-run.
- Updated golangci-lint config to ignore funlen and gocyclo for all test files (both _test.go and tests/ directory). Committed and pushed to github_main. Linter now ignores test complexity as expected.
- Updated golangci-lint config to ignore errcheck for all test files (both _test.go and tests/ directory). Committed and pushed to github_main. Linter now ignores unchecked error returns in tests as expected.
- Updated golangci-lint config to ignore lll (long line linter) for all test files (both _test.go and tests/ directory). Committed and pushed to github_main. Linter now ignores long lines in tests as expected.
- 已根据用户需求，移除 DSL 解析器中无用的 [] 分支，并恢复了对 -field 排除字段的正确处理逻辑。
- 相关 DSL 解析和 ToJSON 测试全部通过，主流程无回归。
- 已提交 commit: 3b5fcb7
- Fixed the linter warning for ineffectual assignment to fkFieldFound in both preloadBelongsTo and preloadHasMany.
- All tests passed and the linter warning is resolved.
- Committed the change as: fix: remove ineffectual assignment to fkFieldFound in preloadBelongsTo and preloadHasMany
- No logic was changed; only the unnecessary assignment was removed as per linter guidance.
- Refactored the if-else chain for relatedElemType.Kind() in preloadManyToMany to a switch statement as per gocritic suggestion.
- Linter warning (ifElseChain) is now resolved.
- All tests passed after the change.
- Committed as: refactor: use switch instead of if-else for relatedElemType.Kind() in preloadManyToMany to fix gocritic ifElseChain warning
- Removed the ineffectual assignment to placeholders in the else branch of saveInternal in crud.go.
- Linter warning (ineffassign) is now resolved for this issue.
- All tests passed after the change.
- Committed as: fix: remove ineffectual assignment to placeholders in saveInternal else branch
- Moved 'cacheTask' and 'finalWriteTask' type declarations out of the generic function in cache.go to the package level.
- This resolves the Go 1.18 build error: 'type declarations inside generic functions are not currently supported'.
- All code now builds and runs as expected on Go 1.18+.
- Committed as: fix: move type declarations out of generic function for Go 1.18 compatibility
