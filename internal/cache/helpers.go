package cache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// "thing" // REMOVED import of root package

// Assuming redis is used, adjust if needed

// AddIDToListIfNotExists adds an ID to a list if it's not already present.
// Exported: Needed by root cache.go
func AddIDToListIfNotExists(ids []int64, idToAdd int64) []int64 {
	for _, id := range ids {
		if id == idToAdd {
			return ids // Already exists
		}
	}
	return append(ids, idToAdd)
}

// RemoveIDFromList removes an ID from a list.
// Exported: Needed by root cache.go
func RemoveIDFromList(ids []int64, idToRemove int64) []int64 {
	newIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != idToRemove {
			newIDs = append(newIDs, id)
		}
	}
	return newIDs
}

// SetCachedListIDs stores a list of IDs as a JSON array string in the cache.
// Exported: Needed by root cache.go?
func SetCachedListIDs(ctx context.Context, cacheClient CacheClient, key string, ids []int64, ttl time.Duration) error {
	if ids == nil {
		// Avoid storing nil slice, store empty array instead if needed or delete?
		// Let's store empty array for consistency.
		ids = []int64{}
	}
	// jsonData, err := json.Marshal(ids)
	// if err != nil {
	// 	return fmt.Errorf("failed to marshal list for key '%s': %w", key, err)
	// }

	var err error // Declare err here
	err = cacheClient.SetQueryIDs(ctx, key, ids, ttl)
	if err != nil {
		return fmt.Errorf("failed to set cached list '%s': %w", key, err)
	}
	return nil
}

// GetCachedCount retrieves an integer count from the cache.
// Exported: Needed by root cache.go
func GetCachedCount(ctx context.Context, cacheClient CacheClient, key string) (int64, error) {
	countStr, err := cacheClient.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrNotFound) { // Use local ErrNotFound
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

// SetCachedCount stores an integer count in the cache.
// Exported: Needed by root cache.go?
func SetCachedCount(ctx context.Context, cacheClient CacheClient, key string, count int64, ttl time.Duration) error {
	countStr := strconv.FormatInt(count, 10)
	err := cacheClient.Set(ctx, key, countStr, ttl)
	if err != nil {
		return fmt.Errorf("failed to set cached count '%s': %w", key, err)
	}
	return nil
}
