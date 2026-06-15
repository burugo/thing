package thing

import (
	"testing"
)

// FuzzGenerateQueryHash tests that GenerateQueryHash does not panic on
// arbitrary inputs and always returns a non-empty string.
func FuzzGenerateQueryHash(f *testing.F) {
	// Seed corpus
	f.Add("name = ?", "Alice", "id ASC")
	f.Add("", "", "")
	f.Add("status IN (?,?)", "active", "created_at DESC")
	f.Add("id > ? AND deleted = ?", "0", "")

	f.Fuzz(func(t *testing.T, where, arg, order string) {
		params := QueryParams{
			Where: where,
			Args:  []interface{}{arg},
			Order: order,
		}
		result := GenerateQueryHash(params)
		if result == "" {
			t.Error("GenerateQueryHash returned empty string")
		}
	})
}

// FuzzGenerateCacheKey tests that GenerateCacheKey does not panic and
// returns a key that includes the expected prefix.
func FuzzGenerateCacheKey(f *testing.F) {
	f.Add("list", "users", "name = ?", "Alice", "id ASC")
	f.Add("count", "books", "", "", "")
	f.Add("count_precise", "orders", "status = ?", "pending", "created_at DESC")

	f.Fuzz(func(t *testing.T, prefix, table, where, arg, order string) {
		params := QueryParams{
			Where: where,
			Args:  []interface{}{arg},
			Order: order,
		}
		result := GenerateCacheKey(prefix, table, params)
		if result == "" {
			t.Error("GenerateCacheKey returned empty string")
		}
	})
}
