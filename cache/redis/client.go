package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	// Import the core thing package for interfaces/errors
	"thing"
)

// client implements thing.CacheClient using Redis.
// Renamed from redisCache to client (internal type).
type client struct {
	redisClient *redis.Client // Renamed field for clarity
}

// Ensure client implements thing.CacheClient.
var _ thing.CacheClient = (*client)(nil)

// Options holds configuration for the Redis client.
type Options struct {
	Addr     string
	Password string
	DB       int
}

// NewClient creates a new Redis cache client wrapper.
// Changed name to NewClient and made it exported.
func NewClient(opts Options) (thing.CacheClient, func(), error) {
	redisOpts := &redis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
	}
	rdb := redis.NewClient(redisOpts)

	// Ping Redis to check connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status := rdb.Ping(ctx)
	if err := status.Err(); err != nil {
		return nil, nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	log.Println("Redis cache client initialized successfully.")
	cleanup := func() {
		if err := rdb.Close(); err != nil {
			log.Printf("Error closing Redis client: %v", err)
		}
	}
	// Return our internal client type which implements the interface
	return &client{redisClient: rdb}, cleanup, nil
}

// Get retrieves a raw string value from Redis.
func (c *client) Get(ctx context.Context, key string) (string, error) {
	val, err := c.redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", thing.ErrNotFound // Use standard not found error
	} else if err != nil {
		return "", fmt.Errorf("redis Get error for key '%s': %w", key, err)
	}
	return val, nil
}

// Set stores a raw string value in Redis.
func (c *client) Set(ctx context.Context, key string, value string, expiration time.Duration) error {
	err := c.redisClient.Set(ctx, key, value, expiration).Err()
	if err != nil {
		return fmt.Errorf("redis Set error for key '%s': %w", key, err)
	}
	return nil
}

// Delete removes a key from Redis.
func (c *client) Delete(ctx context.Context, key string) error {
	err := c.redisClient.Del(ctx, key).Err()
	if err != nil && err != redis.Nil { // Don't error if key didn't exist
		return fmt.Errorf("redis Del error for key '%s': %w", key, err)
	}
	return nil
}

// GetModel retrieves a model from Redis.
func (c *client) GetModel(ctx context.Context, key string, dest interface{}) error {
	val, err := c.redisClient.Get(ctx, key).Result() // Use Result() to get string value
	if err == redis.Nil {
		return thing.ErrNotFound // Key truly not found in Redis
	} else if err != nil {
		return fmt.Errorf("redis Get error for key '%s': %w", key, err)
	}

	// Check for NoneResult marker AFTER checking for redis.Nil
	if val == thing.NoneResult {
		return thing.ErrCacheNoneResult // Return specific error for NoneResult
	}

	// If not NoneResult, attempt to unmarshal as the destination type
	err = json.Unmarshal([]byte(val), dest) // Unmarshal from string value bytes
	if err != nil {
		// Consider logging the problematic data: log.Printf("Unmarshal error for key '%s', data: %s", key, string(val))
		return fmt.Errorf("redis Unmarshal error for key '%s' (value: '%s'): %w", key, val, err)
	}
	return nil
}

// SetModel stores a model in Redis.
func (c *client) SetModel(ctx context.Context, key string, model interface{}, expiration time.Duration) error {
	data, err := json.Marshal(model)
	if err != nil {
		return fmt.Errorf("redis Marshal error for key '%s': %w", key, err)
	}
	err = c.redisClient.Set(ctx, key, data, expiration).Err()
	if err != nil {
		return fmt.Errorf("redis Set error for key '%s': %w", key, err)
	}
	return nil
}

// DeleteModel removes a model from Redis.
func (c *client) DeleteModel(ctx context.Context, key string) error {
	err := c.redisClient.Del(ctx, key).Err()
	if err != nil && err != redis.Nil { // Don't error if key didn't exist
		return fmt.Errorf("redis Del error for key '%s': %w", key, err)
	}
	return nil
}

// GetQueryIDs retrieves a list of IDs from Redis.
// It now checks for the NoneResult marker and returns ErrQueryCacheNoneResult if found.
func (c *client) GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error) {
	val, err := c.redisClient.Get(ctx, queryKey).Result() // Use Result() to get string directly
	if err == redis.Nil {
		return nil, thing.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("redis Get error for query key '%s': %w", queryKey, err)
	}

	// Check for NoneResult marker *before* trying to unmarshal
	if val == thing.NoneResult {
		return nil, thing.ErrQueryCacheNoneResult
	}

	// If not NoneResult, attempt to unmarshal as list of IDs
	var ids []int64
	err = json.Unmarshal([]byte(val), &ids) // Unmarshal from string value
	if err != nil {
		// If unmarshaling fails, it might be unexpected data. Return error.
		return nil, fmt.Errorf("redis Unmarshal error for query key '%s' (value: '%s'): %w", queryKey, val, err)
	}
	return ids, nil
}

// SetQueryIDs stores a list of IDs in Redis.
func (c *client) SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error {
	// Handle empty slice case - store an empty JSON array? Or delete?
	// Storing empty array is safer if Get expects a list.
	if ids == nil {
		ids = []int64{} // Ensure we store "[]" not "null"
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("redis Marshal error for query key '%s': %w", queryKey, err)
	}
	err = c.redisClient.Set(ctx, queryKey, data, expiration).Err()
	if err != nil {
		return fmt.Errorf("redis Set error for query key '%s': %w", queryKey, err)
	}
	return nil
}

// DeleteQueryIDs removes a list of IDs from Redis.
func (c *client) DeleteQueryIDs(ctx context.Context, queryKey string) error {
	err := c.redisClient.Del(ctx, queryKey).Err()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("redis Del error for query key '%s': %w", queryKey, err)
	}
	return nil
}

// AcquireLock tries to acquire a lock using Redis SETNX.
func (c *client) AcquireLock(ctx context.Context, lockKey string, expiration time.Duration) (bool, error) {
	// Use a unique value for the lock holder if needed for more complex scenarios,
	// but for simple lock/unlock, a placeholder is fine.
	lockValue := "1"
	acquired, err := c.redisClient.SetNX(ctx, lockKey, lockValue, expiration).Result()
	if err != nil {
		return false, fmt.Errorf("redis SetNX error for lock key '%s': %w", lockKey, err)
	}
	return acquired, nil
}

// ReleaseLock releases a lock by deleting the key.
// Consider using Lua script for atomic check-and-delete if lock value matters.
func (c *client) ReleaseLock(ctx context.Context, lockKey string) error {
	err := c.redisClient.Del(ctx, lockKey).Err()
	// Ignore redis.Nil error, as it means the lock might have expired or already released.
	if err != nil && err != redis.Nil {
		return fmt.Errorf("redis Del error for lock key '%s': %w", lockKey, err)
	}
	return nil
}

// DeleteByPrefix removes all cache entries whose keys match the given prefix.
// Uses SCAN for safe iteration over keys.
func (c *client) DeleteByPrefix(ctx context.Context, prefix string) error {
	var cursor uint64
	var keysToDelete []string
	const scanCount = 100 // How many keys to fetch per SCAN iteration

	matchPattern := prefix + "*" // Add wildcard for SCAN

	for {
		var keys []string
		var err error
		keys, cursor, err = c.redisClient.Scan(ctx, cursor, matchPattern, scanCount).Result()
		if err != nil {
			log.Printf("ERROR: Redis SCAN error during DeleteByPrefix (prefix: %s): %v", prefix, err)
			return fmt.Errorf("redis SCAN error for prefix '%s': %w", prefix, err)
		}

		if len(keys) > 0 {
			keysToDelete = append(keysToDelete, keys...)
		}

		// Check if SCAN iteration is complete
		if cursor == 0 {
			break
		}
	}

	// If keys were found, delete them
	if len(keysToDelete) > 0 {
		log.Printf("REDIS CACHE: Deleting %d keys with prefix '%s'", len(keysToDelete), prefix)
		err := c.redisClient.Del(ctx, keysToDelete...).Err()
		if err != nil && err != redis.Nil {
			log.Printf("ERROR: Redis DEL error during DeleteByPrefix (prefix: %s): %v", prefix, err)
			return fmt.Errorf("redis DEL error for prefix '%s': %w", prefix, err)
		}
	} else {
		log.Printf("REDIS CACHE: No keys found matching prefix '%s' to delete", prefix)
	}

	return nil
}

// InvalidateQueriesContainingID finds and deletes query cache keys matching the prefix
// whose stored ID list contains the specified idToInvalidate.
// Uses SCAN for safe iteration.
func (c *client) InvalidateQueriesContainingID(ctx context.Context, prefix string, idToInvalidate int64) error {
	var cursor uint64
	var keysToDelete []string
	var invalidatedCount int
	const scanCount = 100 // How many keys to fetch per SCAN iteration

	matchPattern := prefix + "*" // Add wildcard for SCAN
	log.Printf("REDIS CACHE: Starting SCAN for prefix '%s' to invalidate keys containing ID %d", prefix, idToInvalidate)

	for {
		var keys []string
		var err error
		keys, cursor, err = c.redisClient.Scan(ctx, cursor, matchPattern, scanCount).Result()
		if err != nil {
			log.Printf("ERROR: Redis SCAN error during InvalidateQueriesContainingID (prefix: %s): %v", prefix, err)
			return fmt.Errorf("redis SCAN error for prefix '%s': %w", prefix, err)
		}

		// For each key found by SCAN, check if its list contains the ID
		for _, queryKey := range keys {
			// Get the list of IDs for this specific query cache key
			// Use the same context as Scan/Delete operations
			cachedIDs, getErr := c.GetQueryIDs(ctx, queryKey) // Use the existing method

			if getErr != nil {
				// Log error (e.g., key expired between SCAN and GET, or not an ID list) but continue
				// If it's ErrNotFound, the key disappeared, which is fine.
				if !errors.Is(getErr, thing.ErrNotFound) {
					log.Printf("WARN: Failed to get query IDs for key '%s' during invalidation check: %v", queryKey, getErr)
				}
				continue
			}

			// Check if the target model ID is in this list
			found := false
			for _, id := range cachedIDs {
				if id == idToInvalidate {
					found = true
					break
				}
			}

			// If found, mark this specific query cache key for deletion
			if found {
				keysToDelete = append(keysToDelete, queryKey)
			}
		}

		// Check if SCAN iteration is complete
		if cursor == 0 {
			break
		}
	}

	// If keys were marked for deletion, delete them
	if len(keysToDelete) > 0 {
		log.Printf("REDIS CACHE: Deleting %d query keys with prefix '%s' containing ID %d", len(keysToDelete), prefix, idToInvalidate)
		err := c.redisClient.Del(ctx, keysToDelete...).Err()
		if err != nil && err != redis.Nil {
			log.Printf("ERROR: Redis DEL error during InvalidateQueriesContainingID (prefix: %s): %v", prefix, err)
			// Return error, but some keys might have been deleted
			return fmt.Errorf("redis DEL error for prefix '%s' while invalidating: %w", prefix, err)
		}
		invalidatedCount = len(keysToDelete) // Count successfully targeted keys
	} else {
		log.Printf("REDIS CACHE: No keys found matching prefix '%s' containing ID %d to invalidate", prefix, idToInvalidate)
	}
	log.Printf("REDIS CACHE: Finished invalidation scan for prefix '%s'. Invalidated %d specific query keys containing ID %d.", prefix, invalidatedCount, idToInvalidate)

	return nil
}
