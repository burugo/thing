package thing_test

import (
	"fmt"
	"testing"

	"github.com/burugo/thing"
	"github.com/burugo/thing/internal/cache"
)

// --- Benchmark Helpers ---

// setupBenchDB creates a test DB + cache for benchmarks and returns a cleanup func.
func setupBenchDB(b *testing.B) (thing.DBAdapter, *mockCacheClient, func()) {
	b.Helper()
	db, mc, cleanup := setupTestDB(b)
	return db, mc, cleanup
}

// seedUsers inserts n users and returns their IDs.
func seedUsers(b *testing.B, th *thing.Thing[*User], n int) []int64 {
	b.Helper()
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		u := &User{Name: fmt.Sprintf("user-%d", i), Email: fmt.Sprintf("u%d@bench.dev", i)}
		if err := th.Save(u); err != nil {
			b.Fatal(err)
		}
		ids = append(ids, u.ID)
	}
	return ids
}

// --- Benchmarks ---

// BenchmarkByID_CacheMiss measures ByID when every call is a cache miss
// (model fetched from DB, then cached).
func BenchmarkByID_CacheMiss(b *testing.B) {
	db, mc, cleanup := setupBenchDB(b)
	defer cleanup()
	cache.ResetGlobalCacheIndex()

	th, err := thing.New[*User](db, mc)
	if err != nil {
		b.Fatal(err)
	}

	ids := seedUsers(b, th, 100)
	mc.Reset() // clear cache so every call is a miss

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := ids[i%len(ids)]
		if _, err := th.ByID(id); err != nil {
			b.Fatal(err)
		}
		// Clear the cached model so the next iteration is also a miss.
		mc.Reset()
	}
}

// BenchmarkByID_CacheHit measures ByID when every call hits the cache.
func BenchmarkByID_CacheHit(b *testing.B) {
	db, mc, cleanup := setupBenchDB(b)
	defer cleanup()
	cache.ResetGlobalCacheIndex()

	th, err := thing.New[*User](db, mc)
	if err != nil {
		b.Fatal(err)
	}

	ids := seedUsers(b, th, 100)
	// Warm up cache: fetch each ID once so it gets cached.
	for _, id := range ids {
		if _, err := th.ByID(id); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := ids[i%len(ids)]
		if _, err := th.ByID(id); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSave_Create measures creating new records.
func BenchmarkSave_Create(b *testing.B) {
	db, mc, cleanup := setupBenchDB(b)
	defer cleanup()
	cache.ResetGlobalCacheIndex()

	th, err := thing.New[*User](db, mc)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u := &User{Name: fmt.Sprintf("bench-%d", i), Email: fmt.Sprintf("b%d@test.dev", i)}
		if err := th.Save(u); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSave_Update measures updating existing records.
func BenchmarkSave_Update(b *testing.B) {
	db, mc, cleanup := setupBenchDB(b)
	defer cleanup()
	cache.ResetGlobalCacheIndex()

	th, err := thing.New[*User](db, mc)
	if err != nil {
		b.Fatal(err)
	}

	// Seed one user to update repeatedly.
	u := &User{Name: "update-me", Email: "update@bench.dev"}
	if err := th.Save(u); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.Name = fmt.Sprintf("updated-%d", i)
		if err := th.Save(u); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkQueryFetch measures Query(...).Fetch(offset, limit) with cache.
func BenchmarkQueryFetch(b *testing.B) {
	db, mc, cleanup := setupBenchDB(b)
	defer cleanup()
	cache.ResetGlobalCacheIndex()

	th, err := thing.New[*User](db, mc)
	if err != nil {
		b.Fatal(err)
	}

	seedUsers(b, th, 200)

	params := thing.QueryParams{Order: "id ASC"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := th.Query(params)
		if _, err := result.Fetch(0, 20); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkQueryCount measures Query(...).Count() with cache.
func BenchmarkQueryCount(b *testing.B) {
	db, mc, cleanup := setupBenchDB(b)
	defer cleanup()
	cache.ResetGlobalCacheIndex()

	th, err := thing.New[*User](db, mc)
	if err != nil {
		b.Fatal(err)
	}

	seedUsers(b, th, 200)

	params := thing.QueryParams{Order: "id ASC"}
	// Warm up: first call populates the count cache.
	if _, err := th.Query(params).Count(); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := th.Query(params).Count(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGenerateQueryHash measures the hash function used for cache keys.
func BenchmarkGenerateQueryHash(b *testing.B) {
	params := thing.QueryParams{
		Where: "name = ? AND deleted = ?",
		Args:  []interface{}{"Alice", false},
		Order: "created_at DESC",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = thing.GenerateQueryHash(params)
	}
}
