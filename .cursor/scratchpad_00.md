# Background and Motivation

Current goals:
- Refactor project structure by moving internal cache utility functions and types from cache.go to internal/cache/, improving package clarity and maintainability.
- Continuously fix golangci-lint issues (naming, style, complexity, etc.) to improve code quality.
- **All API changes (e.g., CacheStats, DBAdapter, etc.) must be reflected in the README.md examples and documentation to ensure consistency.**

# Key Challenges and Analysis

- Need to review all functions and types in cache.go, distinguishing which should be moved to internal/cache/ and which should remain in the main package.
- During migration, all references must be updated, public API must remain stable, and all tests must pass.
- Some lint issues (naming, unused params, complexity, etc.) should be fixed incrementally; some can be relaxed via config (e.g., long lines in tests, internal imports, etc.).
- Structural refactoring must strictly preserve logic and behavior; all related tests must pass.
- **Whenever the API changes, README.md examples and documentation must be updated in sync to avoid discrepancies.**
- Plan to remove TypeMapping from internal/schema/generate.go; each driver will define its own type mapping and inject it via DBAdapter.GetTypeMap().
- Plan to expose TableInfo, ColumnInfo, IndexInfo, Introspector, etc. as public types and interfaces in the main package; driver introspect will only depend on these public types.
- Plan to integrate automatic ALTER TABLE (schema diff) into AutoMigrate, but by default do NOT drop columns (DROP COLUMN); only add/modify columns automatically, column deletion requires manual migration.
- Naming will use concise forms like TableInfo/ColumnInfo/IndexInfo, without a Public prefix.
- All of the above are structural decoupling and safety best practices, and will be followed in subsequent implementation.

# High-level Task Breakdown

### Structure Decoupling & Interface Refactor Plan (gorm-style driver ecosystem) — Detailed Tasks

#### 1. Interface and Type Migration
- [ ] 1.1 Create a new interfaces.go in the main package, defining all core interfaces and types: DBAdapter, CacheClient, SQLBuilder, Dialector, etc.
- [ ] 1.2 Migrate all relevant interfaces/types from internal/interfaces and internal/sqlbuilder to interfaces.go in the main package, with necessary naming/comment improvements.
- [ ] 1.3 Check all references in the main package, drivers, and tests, and unify to use the main package interfaces.
- [ ] 1.4 Remove/deprecate migrated content from internal/interfaces.go and internal/sqlbuilder.go.
- Success: Users and driver developers only need to import the main package to implement/extend core interfaces.

#### 2. Decoupling from internal dependencies
- [ ] 2.1 Review all DBAdapter, CacheClient, and related interface method signatures, listing all parameters/types that depend on internal packages.
- [ ] 2.2 Change all interface parameters to use only types exposed by the main package or basic types (e.g., string, []interface{}), such as tableName, pkName, where, args.
- [ ] 2.3 The main package is responsible for converting ModelInfo/QueryParams to basic parameters internally; drivers only care about the minimal required info.
- [ ] 2.4 Update all driver implementations and main package call sites accordingly.
- Success: Users implementing drivers never need to import internal; type conversion is seamless inside the main package.

#### 3. SQLBuilder/Dialector migration and dependency review
- [ ] 3.1 Migrate SQLBuilder, Dialector, and all SQL generation related interfaces/types to interfaces.go in the main package.
- [ ] 3.2 Check all dependencies on SQLBuilder in the main package and drivers, ensuring only interface dependencies, not implementation dependencies.
- [ ] 3.3 If custom SQLBuilder support is needed, provide a factory function or type registration mechanism in the main package.
- Success: SQL generation logic is extensible, and there are no circular imports between main package and drivers.

#### 4. Move driver packages out of internal, into public drivers directory
- [ ] 4.1 Move drivers/db/sqlite, drivers/cache/redis, drivers/cache/localcache.go, etc. out of internal, into the drivers directory.
- [ ] 4.2 Check and fix all import paths, ensuring the main package, tests, and user code can directly import drivers.
- [ ] 4.3 Update documentation and README, with examples following gorm style.
- Success: Users can freely import official/third-party drivers, matching gorm's open ecosystem.

#### 5. Main package and drivers only depend on interfaces, never on each other's implementation
- [ ] 5.1 Check all imports in the main package and drivers, ensuring they only depend on thing/interfaces.go.
- [ ] 5.2 If any implementation dependency exists, refactor to interface dependency or decouple via factory/registration.
- Success: Dependency relationships are clear, no circular imports, structure matches gorm.

#### 6. Documentation and README update
- [ ] 6.1 Update README to clearly explain interfaces, driver usage, and extension methods.
- [ ] 6.2 Provide complete examples for main package, drivers, and user custom implementations, following gorm style.
- [ ] 6.3 Review all documentation, comments, and example code for consistency and clarity.
- Success: Users and driver developers can get started and understand everything from a single source.

### SQLBuilder/Dialector Decoupling & Migration Plan

**Goal:** Decouple SQLBuilder/Dialector logic from `internal` to enhance extensibility and align with Gorm-style driver ecosystem.
**Task Type:** `Refactoring (Structural)`

1.  **Analyze and Migrate Interfaces/Types:**
    *   `[x] 1.1` Analyze `internal/sqlbuilder/sqlbuilder.go` to identify core interfaces (`SQLBuilder`, `Dialector`) and necessary public types to migrate.（已完成，接口已迁移至 interfaces.go）
    *   `[x] 1.2` Copy identified interface/type definitions to `interfaces.go`. Keep original definitions in `internal/sqlbuilder` for now.（已完成，主包暴露接口，驱动已切换依赖）
    *   Success Criteria: `interfaces.go` updated, code compiles.（已通过）

2.  **Update Driver Dependencies:**
    *   `[x] 2.1` Modify `drivers/db/*`: Remove imports of `internal/sqlbuilder`, add imports for `thing` (or the package containing `interfaces.go`).（已完成，所有驱动已依赖主包接口）
    *   `[x] 2.2` Update references in drivers to use interfaces/types from `interfaces.go` (e.g., `thing.SQLBuilder`).（已完成）
    *   `[x] 2.3` Fix all compilation errors in drivers.（已完成）
    *   Success Criteria: Drivers compile without `internal/sqlbuilder` dependency.（已通过）

3.  **Update Core ORM Dependencies & Instantiation:**
    *   `[x] 3.1` Locate where `SQLBuilder` is created/used in core ORM code.（已完成）
    *   `[x] 3.2` **Design Decision:** Choose mechanism for providing `SQLBuilder` via interface (Recommended: Gorm-style via `Dialector`/`DBAdapter`).（已完成，采用 db.Builder() 注入）
    *   `[x] 3.3` Implement the chosen mechanism, modifying core ORM to use the interface and remove `internal/sqlbuilder` dependency.（已完成）
    *   `[x] 3.4` Fix all compilation errors in core ORM.（已完成）
    *   Success Criteria: Core ORM compiles, uses `SQLBuilder` interface, no `internal/sqlbuilder` dependency.（已通过）

4.  **Clean Up `internal/sqlbuilder`:**
    *   `[ ] 4.1` Remove duplicated interface/type definitions from `internal/sqlbuilder/sqlbuilder.go`.
    *   `[ ] 4.2` Ensure the remaining implementation in `internal/sqlbuilder` satisfies the contracts in `interfaces.go`.
    *   Success Criteria: `internal/sqlbuilder` contains only implementation, code compiles.

5.  **Comprehensive Testing:**
    *   `[x] 5.1` Run `go test -v ./tests | grep FAIL`.（已完成，全部通过）
    *   `[x] 5.2` Debug and fix any test failures *without* changing SQL generation logic.（已完成）
    *   Success Criteria: All tests pass.（已通过）

6.  **Commit and Document:**
    *   `[x] 6.1` Commit all changes atomically.（已完成）
    *   `[ ] 6.2` (Follow-up Task) Update README/docs about `SQLBuilder`/`Dialector` extensibility.
    *   Success Criteria: Code committed; documentation task recorded.

### README Update Checklist (for interface/structure refactor)

- [ ] Clearly state that all core interfaces (DBAdapter, CacheClient, SQLBuilder, Dialector, etc.) have been moved to the main package, and users/driver developers only need to import the main package.
- [ ] Show typical import paths and usage for the main package, drivers, and user custom implementations (following gorm-style examples).
- [ ] Provide updated interface signatures and implementation examples for DBAdapter, CacheClient, SQLBuilder, Dialector, etc.
- [ ] Explain that driver packages have been moved out of internal, and users can directly import drivers/db/sqlite, drivers/cache/redis, etc.
- [ ] Give detailed instructions on how to customize/extend drivers (e.g., custom DBAdapter, CacheClient, SQLBuilder).
- [ ] Explain the dependency principle between the main package and drivers (only depend on interfaces, never circular import).
- [ ] Ensure all example code, usage, and comments are updated to match the new interfaces/structure, **and that all examples are fully correct, directly runnable, and never misleading**.
- [ ] Add FAQ for migration/refactor (e.g., "Why can't I import internal?", "How do I implement a custom driver?", etc.).

### Structure Refactoring & Lint Fix Plan

1. Review cache.go and list all functions, types, and constants suitable for migration to internal/cache/.
   - Task Type: Refactoring (Structural)
   - Success: Migration list is clear and scoped.
2. Move the listed items to internal/cache/ and update all references.
   - Task Type: Refactoring (Structural)
   - Success: Migration complete, public API unchanged, code compiles.
3. Retain/adjust only the necessary APIs in the main package that are related to Thing[T] or must be public.
   - Task Type: Refactoring (Structural)
   - Success: Only essential APIs are public, structure is clear.
4. Run all tests to ensure no regressions.
   - Task Type: Refactoring (Structural)
   - Success: go test -v ./tests | grep FAIL returns no output.
5. Commit changes, including scratchpad updates.
   - Task Type: Refactoring (Structural)
   - Success: All relevant files and scratchpad are committed together.
6. Review and incrementally fix remaining golangci-lint issues:
   - Naming conventions (e.g., JSONOptions, ID, URL, etc.)
   - Unused params/variables
   - High complexity functions (gocyclo/funlen)
   - Magic numbers (mnd/goconst)
   - Other style/security suggestions (revive/gosec, etc.)
   - Task Type: Refactoring (Functional)
   - Success: Each issue fixed, lint passes, no functional changes.
7. Continuously optimize lint config, relaxing rules where appropriate (e.g., long lines in tests, internal imports, depguard, gocyclo/funlen/gosec exclusions for tests, etc.).
   - Task Type: Refactoring (Structural)
   - Success: Config is reasonable, dev experience improved.
8. **After every API change, immediately review and update README.md usage, examples, and documentation.**
   - Task Type: Documentation
   - Success: README.md is always up-to-date with the actual API, no outdated examples.

# Project Status Board

- [x] Schema 类型分离与深拷贝、表不存在行为修正、主包与驱动解耦
- [x] 自动迁移相关测试在 SQLite/MySQL/Postgres 下全部通过（已参数化，数据库不可用时自动跳过）
- [x] MySQL introspector 索引解析兼容所有字段数，字段数不匹配问题已修复
- [x] README.md 文档同步与示例更新
- [x] golint/golangci-lint 检查与修复（本阶段仅记录，未做大规模风格重构）

# Executor's Feedback or Assistance Requests

- golangci-lint 检查通过，无 error/blocker，主要为 switch 不穷举、函数过长、ifElseChain、行超长、命名风格等非阻断性问题。
- 代码可读性和健壮性已达预期，后续可分阶段优化风格和复杂度。

# Lessons

- 结构性重构后，文档和 lint 检查必须同步，避免遗漏和技术债。
- 大型项目建议分阶段处理风格和复杂度问题，优先保证功能和接口一致性。

---

**Note:** All code, comments, and documentation (including commit messages) must be written in English. 

- 规划将 internal/schema/generate.go 中的 TypeMapping 移除，由各驱动自行定义类型映射并通过 DBAdapter.GetTypeMap() 注入。
- 规划主包暴露 TableInfo/ColumnInfo/IndexInfo/Introspector 等公共类型和接口，驱动 introspect 只依赖主包类型。
- 规划 AutoMigrate 集成自动 ALTER TABLE（schema diff），但默认不自动删除字段（DROP COLUMN），只做新增/变更，删除需手动迁移。
- 命名采用 TableInfo/ColumnInfo/IndexInfo 等简洁风格，无需加 Public 前缀。
- 以上均为结构性解耦和安全性最佳实践，后续将按此方案执行。 