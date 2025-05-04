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

## Project Status Board

- [x] 删除 internal/cache/helpers.go（已完成）
- [x] 检查/确认 cache.go 结构性重构空间有限（已完成）
- [x] 强调避免直接运行 go test -v ./tests（已完成）
- [ ] ~~localCache 迁移到 internal/drivers/cache/localcache.go，并确保 NoneResult 相关逻辑与 mock/redis 保持一致~~（已取消，见反馈）
- [ ] 检查/修正 localCache GetModel、Get、GetQueryIDs 等方法对 NoneResult 的处理，确保与 mockCache/redis 行为一致
- [x] Review cache.go for migration candidates
- [x] Migrate to internal/cache/ and update references（已评估，无需迁移/已完成。主包缓存相关内容因接口暴露和依赖关系，绝大多数无法迁移到 internal，详见反馈。）
- [x] Retain/adjust necessary public APIs in main package（已评估，当前暴露范围合理，无需调整）
- [ ] Full test validation
    - [ ] Commit changes
- [ ] Review and fix remaining lint issues
- [ ] Optimize lint config
- [ ] Sync README.md with every API change
- [ ] localCache 需要实现真实的 CacheStats 统计逻辑（如 hit/miss/total 等），目前仅为空实现。需补充计数器和相关方法，保证与 mock/redis 统计能力一致。

## Executor's Feedback or Assistance Requests

- 已根据用户决策撤销 localCache 的迁移，localCache 及 DefaultLocalCache 保持在主包（cache.go）。
- 原因：CacheClient 接口的 GetCacheStats 必须返回主包定义的 CacheStats 类型，localCache 作为默认实现无法 decouple 到 internal 或 drivers 目录，否则会导致循环依赖或接口不兼容。
- 代码已恢复迁移前状态，所有 DefaultLocalCache 引用已指向主包。
- 运行 go test -v ./tests | grep FAIL，发现部分测试因接口/类型不兼容（如 DBAdapter/CacheClient/Tx 类型签名不一致）导致构建失败，这与 localCache 迁移无关，需后续统一接口。
- localCache 目前的 GetCacheStats 仅返回空 map，没有实际统计逻辑。后续需补充 hit/miss/total 等计数器，实现完整的缓存监控能力。

## Lessons

- Include info useful for debugging in the program output.
- Read the file before you try to edit it.
- Always ask before using the -force git command
- 避免直接运行 `go test -v ./tests`，推荐用 `go test -v ./tests | grep FAIL` 或按需单测，防止输出过多影响调试。
- When the user says "去掉/移除/删除xxx" (remove/delete something) as a direct code modification instruction, do not ask for mode, immediately switch to executor and execute.

---

**Note:** All code, comments, and documentation (including commit messages) must be written in English. 