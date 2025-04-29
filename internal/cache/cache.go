package cache

import (
	"context"
	"fmt"
	"log"
)

// GenerateCacheKey creates a standard cache key string for a single model.
func GenerateCacheKey(tableName string, id int64) string {
	// Format: {tableName}:{id}
	return fmt.Sprintf("%s:%d", tableName, id)
}

// QueryParams structure (copy from main package for internal use)
type QueryParams struct {
	Where    string        // Raw WHERE clause (e.g., "status = ? AND name LIKE ?")
	Args     []interface{} // Arguments for the WHERE clause placeholders
	Order    string        // Raw ORDER BY clause (e.g., "created_at DESC")
	Start    int           // Offset (for pagination)
	Limit    int           // Limit (for pagination)
	Preloads []string      // List of relationship names to eager-load (e.g., ["Author", "Comments"])
}

// Interface for CacheClient (copy needed for LockFn)
type CacheClient interface {
	WithLock(ctx context.Context, lockKey string, action func(ctx context.Context) error) error
}

// WithLock acquires a lock, executes the action, and releases the lock.
func WithLock(ctx context.Context, cache CacheClient, lockKey string, action func(ctx context.Context) error) error {
	if cache == nil {
		log.Printf("Warning: Proceeding without lock for key '%s', cache client is nil", lockKey)
		return action(ctx)
	}
	return cache.WithLock(ctx, lockKey, action)
}
