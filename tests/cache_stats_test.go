package thing_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// cache_stats_test.go
//
// This test verifies that the cache client (mockCacheClient) correctly tracks operation statistics
// such as total calls, misses, and allows calculation of hit rates for Get, GetModel, GetQueryIDs, etc.
// It ensures that GetCacheStats returns accurate counters for monitoring and debugging purposes.

// TestMockCacheClient_GetCacheStats verifies that mockCacheClient tracks operation counters correctly.
func TestMockCacheClient_GetCacheStats(t *testing.T) {
	// Initialize a fresh mock cache client via setupTestDB or direct instantiation
	mock := &mockCacheClient{}
	mock.Reset()

	ctx := context.Background()

	// No operations yet: stats should be empty
	stats := mock.GetCacheStats(ctx)
	assert.Empty(t, stats.Counters, "Expected no counters before any operations")

	// Perform various cache operations to increment counters
	_, err := mock.Get(ctx, "key")
	_ = err
	err = mock.Set(ctx, "key", "value", time.Second)
	_ = err
	err = mock.Delete(ctx, "key")
	_ = err

	err = mock.GetModel(ctx, "model", &struct{ ID int }{1})
	_ = err
	err = mock.SetModel(ctx, "model", &struct{ ID int }{1}, time.Second)
	_ = err
	err = mock.DeleteModel(ctx, "model")
	_ = err

	_, err = mock.GetQueryIDs(ctx, "query1")
	_ = err
	err = mock.SetQueryIDs(ctx, "query1", []int64{1, 2}, time.Second)
	_ = err
	err = mock.DeleteQueryIDs(ctx, "query1")
	_ = err

	_, err = mock.AcquireLock(ctx, "lockKey", time.Second)
	_ = err
	err = mock.ReleaseLock(ctx, "lockKey")
	_ = err

	err = mock.SetCount(ctx, "cntKey", 5, time.Second)
	_ = err
	_, err = mock.GetCount(ctx, "cntKey")
	_ = err

	// Retrieve stats
	stats = mock.GetCacheStats(ctx)

	expected := map[string]int{
		"Get":             1,
		"GetMiss":         1,
		"Set":             1,
		"Delete":          1,
		"GetModel":        1,
		"GetModelMiss":    1,
		"SetModel":        1,
		"DeleteModel":     1,
		"GetQueryIDs":     1,
		"GetQueryIDsMiss": 1,
		"SetQueryIDs":     1,
		"DeleteQueryIDs":  1,
		"AcquireLock":     1,
		"ReleaseLock":     1,
		"SetCount":        1,
		"GetCount":        1,
	}
	assert.Equal(t, expected, stats.Counters, "Counters should match expected values after operations")
}
