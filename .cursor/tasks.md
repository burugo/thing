# Task Management

## Completed Tasks
- [x] Investigate issue where `json:"-"` tag on a model field (e.g., `Password`) causes it to be missing when fetched if a cache using standard `json.Marshal/Unmarshal` is involved. `bug-fix`
  - [x] Added `TestModelJsonDash` and `TestJsonDashAffectsDBLoad` to `tests/model_test.go` to verify ORM's direct DB loading (passes when cache is bypassed).
  - [x] Added `UserWithPassword` model and `TestFetch_WithJsonDashField` to `tests/model_test.go` to specifically test `Where(...).Fetch(...)` with `json:"-"` and cache interaction.
  - [x] Identified that standard `json.Marshal` (used initially in `mockCacheClient`) omits fields with `json:"-"`, leading to data loss in cache.
  - [x] Identified that standard `json.Unmarshal` (used initially in `mockCacheClient`) respects `json:"-"` on the target struct, preventing population even if the field is in the JSON.
  - [x] Modified `marshalForMock` in `tests/mock_cache_test.go` to serialize all DB-mapped fields from `ModelInfo` into a map (using Go field names as keys), then marshal this map to JSON, thus including fields despite `json:"-"`.
  - [x] Modified `unmarshalFromMock` in `tests/mock_cache_test.go` to first unmarshal cached JSON to a map, then use `ModelInfo` and reflection to populate all DB-mapped fields in the target struct, effectively bypassing `json:"-"` during cache unmarshalling.
  - [x] `TestFetch_WithJsonDashField` now passes with the modified `mockCacheClient`, confirming that a cache (de)serialization strategy that is aware of all DB fields can preserve fields tagged with `json:"-"`.

## In Progress Tasks

- [ ] **Refactor Cache (De)Serialization to Use Gob and Selective Fields:** `ref-struct`
  - [x] **Task 1.1: Adjust `thing.CacheClient` Interface** `ref-struct`
    - [x] Subtask 1.1.1: Decide on `SetModel` signature change (Option A: modify existing `SetModel` to add `fieldsToCache []string`; Option B: new `SetModelWithFields`). Tentative decision: Option A.
    - [x] Subtask 1.1.2: Update the `CacheClient` interface definition in its Go file (e.g., `cache.go`).
  - [x] **Task 1.2: Update ORM Core's `SetModel` Invocation** `ref-func`
    - [x] Subtask 1.2.1: Identify all call sites of `CacheClient.SetModel` in ORM core (e.g., `crud.go`, `query.go`).
    - [x] Subtask 1.2.2: At these call sites, obtain `ModelInfo` for the instance being cached.
    - [x] Subtask 1.2.3: Extract the list of Go field names from `ModelInfo.Fields`.
    - [x] Subtask 1.2.4: Modify the `SetModel` calls to pass this field list.
  - [x] **Task 1.3: Update `mockCacheClient` Implementation (`tests/mock_cache_test.go`)** `ref-struct`
    - [x] Subtask 1.3.1: Adapt `mockCacheClient.SetModel` to the new interface signature (accept `fieldsToCache []string`).
    - [x] Subtask 1.3.2: Implement `SetModel` to build a map using `fieldsToCache`, then Gob encode this map. (Helper: `marshalForMockWithGob`).
    - [x] Subtask 1.3.3: Ensure `mockCacheClient.GetModel` Gob decodes to a map, then uses reflection to populate the struct from map keys. (Helper: `unmarshalFromMockWithGob`).
    - [x] Subtask 1.3.4: Update any direct test calls to `mockCacheClient.SetModel` if necessary.
  - [x] **Task 1.4: Update Production Cache Client (e.g., `drivers/cache/redis/client.go`)** `ref-struct`
    - [x] Subtask 1.4.1: Adapt its `SetModel` method to the new interface signature.
    - [x] Subtask 1.4.2: Implement `SetModel` to build a map from `modelInstance` using `fieldsToCache`, then Gob encode the map and store in Redis.
    - [x] Subtask 1.4.3: Implement `GetModel` to fetch from Redis, Gob decode to a map, then reflectively populate the target struct from the map.
  - [ ] **Task 1.5: Test Thoroughly** `bug-fix`
    - [ ] Subtask 1.5.1: Ensure `TestFetch_WithJsonDashField` (and similar tests) pass, verifying `json:"-"` fields are correctly cached.
    - [ ] Subtask 1.5.2: Run all unit and integration tests.
    - [ ] Subtask 1.5.3: Debug and fix any failures.
  - [ ] **Task 1.6: Review and Document (Optional)** `ref-func`
    - [ ] Subtask 1.6.1: Code review of all changes.
    - [ ] Subtask 1.6.2: Update internal comments/documentation regarding the new cache strategy.

## Future Tasks

## Implementation Plan
The ORM's cache (de)serialization will be refactored. Instead of relying on default `json.Marshal/Unmarshal` (which caused issues with `json:"-"`), or a complex custom JSON handler in the mock, the production cache clients will use Gob.

To avoid `internal` package import issues and allow selective field caching:
1.  The `thing.CacheClient`'s `SetModel` method will be modified to accept a `fieldsToCache []string` argument.
2.  The ORM core, when saving a model to cache, will use `schema.GetCachedModelInfo()` to get the list of Go field names (`ModelInfo.Fields`) and pass this list to `CacheClient.SetModel`.
3.  The `CacheClient` implementations (mock, redis, etc.) will:
    a.  In `SetModel`: Use reflection and the `fieldsToCache` list to create a `map[string]interface{}` from the model instance. This map will then be Gob-encoded and stored.
    b.  In `GetModel`: Retrieve the Gob-encoded bytes, decode them into a `map[string]interface{}`, and then use reflection to populate the fields of the target model struct from this map.

This approach centralizes the knowledge of "which fields to cache" in the ORM core (which can access `ModelInfo`), while keeping cache drivers generic and using the more robust Gob format for (de)serialization of a well-defined map structure.

**Relevant Files to be Modified (estimated):**
- `cache.go` (or wherever `CacheClient` interface is defined)
- `crud.go` (and potentially `query.go` or other ORM core files that call `SetModel`)
- `tests/mock_cache_test.go`
- `drivers/cache/redis/redis.go` (and any other production cache client implementations)
- `tests/model_test.go` (to ensure tests pass, possibly minor adjustments to test setup if direct `SetModel` calls were made with old signature)

**Next Step for User:** Refactor the production cache client. **The recommended approach is to switch to Gob (`encoding/gob`) or a similar binary serialization format.** This will simplify the implementation, likely improve performance, and inherently ignore `json` struct tags, thus preserving all fields. If binary serialization is not chosen, then a custom JSON (de)serialization strategy (mapping to/from an intermediate map using `ModelInfo`) as prototyped in `mockCacheClient` should be implemented.

**Relevant Files:**
- `tests/model_test.go`: Validates ORM and cache behavior.
- `tests/mock_cache_test.go`: Contains prototype for custom JSON (de)serialization (now less preferred than Gob).
- `internal/schema/metadata.go`: Provides `ModelInfo`.
- **Production Cache Client File (e.g., `drivers/cache/redis/redis.go`)**: This is the primary file to be refactored by the user. 