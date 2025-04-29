package thing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9" // Assuming redis is used, adjust if needed
)

// Placeholder for cache helper functions

// invalidateObjectCache deletes a single model instance from the cache using its table name and ID.
func invalidateObjectCache(ctx context.Context, cache CacheClient, tableName string, id int64) error {
	if cache == nil {
		// Silently return if cache is not configured
		return nil
	}
	if tableName == "" || id == 0 {
		log.Printf("WARN: invalidateObjectCache called with empty tableName or zero ID. Skipping.")
		return nil // Or return an error? For now, just log and skip.
	}

	cacheKey := generateCacheKey(tableName, id)
	cacheDelStart := time.Now()
	errCacheDel := cache.DeleteModel(ctx, cacheKey)
	cacheDelDuration := time.Since(cacheDelStart)

	if errCacheDel != nil {
		// Log the error but don't necessarily fail the calling operation (e.g., Save)
		log.Printf("WARN: Failed to delete model from cache for key %s: %v (%s)", cacheKey, errCacheDel, cacheDelDuration)
		return errCacheDel // Return the error so the caller is aware, though it might ignore it.
	}

	log.Printf("DEBUG: Invalidated object cache for key %s (%s)", cacheKey, cacheDelDuration)
	return nil
}

// getCachedListIDs retrieves a list of IDs stored as a JSON array string from the cache.
// It returns an empty slice if the key is not found (ErrCacheMiss).
func getCachedListIDs(ctx context.Context, cacheClient CacheClient, key string) ([]int64, error) {
	jsonData, err := cacheClient.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, redis.Nil) { // Use thing.ErrNotFound again
			return []int64{}, nil // Not found is not an error here, just an empty list
		}
		return nil, fmt.Errorf("failed to get cached list '%s': %w", key, err)
	}

	if jsonData == "" { // Handle empty string case explicitly
		return []int64{}, nil
	}

	var ids []int64
	if err := json.Unmarshal([]byte(jsonData), &ids); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached list '%s': %w", key, err)
	}
	return ids, nil
}

// setCachedListIDs stores a list of IDs as a JSON array string in the cache.
func setCachedListIDs(ctx context.Context, cacheClient CacheClient, key string, ids []int64, ttl time.Duration) error {
	if ids == nil {
		// Avoid storing nil slice, store empty array instead if needed or delete?
		// Let's store empty array for consistency.
		ids = []int64{}
	}
	jsonData, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("failed to marshal list for key '%s': %w", key, err)
	}

	err = cacheClient.Set(ctx, key, string(jsonData), ttl)
	if err != nil {
		return fmt.Errorf("failed to set cached list '%s': %w", key, err)
	}
	return nil
}

// getCachedCount retrieves an integer count from the cache.
// It returns 0 if the key is not found (ErrCacheMiss).
func getCachedCount(ctx context.Context, cacheClient CacheClient, key string) (int64, error) {
	countStr, err := cacheClient.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, redis.Nil) { // Use thing.ErrNotFound again
			return 0, nil // Not found is not an error here, count is 0
		}
		return 0, fmt.Errorf("failed to get cached count '%s' ('%s'): %w", key, countStr, err)
	}

	if countStr == "" { // Handle empty string case
		return 0, nil
	}

	count, err := strconv.ParseInt(countStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse cached count '%s' ('%s'): %w", key, countStr, err)
	}
	return count, nil
}

// setCachedCount stores an integer count in the cache.
func setCachedCount(ctx context.Context, cacheClient CacheClient, key string, count int64, ttl time.Duration) error {
	countStr := strconv.FormatInt(count, 10)
	err := cacheClient.Set(ctx, key, countStr, ttl)
	if err != nil {
		return fmt.Errorf("failed to set cached count '%s': %w", key, err)
	}
	return nil
}

// Helper to add an ID to a list if it's not already present.
func addIDToListIfNotExists(ids []int64, idToAdd int64) []int64 {
	for _, id := range ids {
		if id == idToAdd {
			return ids // Already exists
		}
	}
	return append(ids, idToAdd)
}

// Helper to remove an ID from a list.
func removeIDFromList(ids []int64, idToRemove int64) []int64 {
	newIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != idToRemove {
			newIDs = append(newIDs, id)
		}
	}
	return newIDs
}

/* <<< Function moved to thing.go as method Thing.updateAffectedQueryCaches >>> */

/* <<< Function moved to thing.go as method Thing.handleDeleteInQueryCaches >>> */
