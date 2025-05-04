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
