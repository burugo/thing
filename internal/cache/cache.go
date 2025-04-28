package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"

	"thing/internal/utils"
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

// GenerateQueryCacheKey generates a cache key for a query based on table name and params hash.
func GenerateQueryCacheKey(tableName string, params interface{}) (string, error) {
	// Create a copy of params to avoid modifying the original
	paramsCopy, ok := params.(QueryParams)
	if !ok {
		return "", fmt.Errorf("params must be of type QueryParams, got %T", params)
	}

	// Normalize values (especially in Args)
	normalizedArgs := make([]interface{}, len(paramsCopy.Args))
	for i, arg := range paramsCopy.Args {
		normalizedArgs[i] = utils.NormalizeValue(arg)
	}
	paramsCopy.Args = normalizedArgs

	// Marshal to JSON
	paramsJSON, err := json.Marshal(paramsCopy)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query params to JSON: %w", err)
	}

	// Calculate SHA-256 hash
	hasher := sha256.New()
	hasher.Write(paramsJSON)
	paramsHash := hex.EncodeToString(hasher.Sum(nil))

	// Format: query:{tableName}:{hash}
	return fmt.Sprintf("query:%s:%s", tableName, paramsHash), nil
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
