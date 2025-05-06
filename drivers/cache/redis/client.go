package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/burugo/thing"
	"github.com/burugo/thing/common"
)

// client implements thing.CacheClient using Redis.
// The counters field tracks operation statistics for monitoring (thread-safe).
type client struct {
	redisClient       *redis.Client  // Underlying Redis client
	mu                sync.Mutex     // Protects counters map
	counters          map[string]int // Operation counters for stats (e.g., "Get", "GetMiss")
	createdInternally bool           // Indicates whether redisClient was created by this struct
}

// Ensure client implements thing.CacheClient and io.Closer.
var (
	_ thing.CacheClient = (*client)(nil)
	_ io.Closer         = (*client)(nil)
)

// incrementCounter safely increments a named operation counter.
func (c *client) incrementCounter(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counters == nil {
		c.counters = make(map[string]int)
	}
	c.counters[name]++
}

// Options holds configuration for the Redis client.
type Options struct {
	Addr     string
	Password string
	DB       int
}

// Close implements io.Closer. Only closes redisClient if client.createdInternally is true.
func (c *client) Close() error {
	if c.createdInternally && c.redisClient != nil {
		return c.redisClient.Close()
	}
	return nil
}

// NewClient creates a new Redis cache client wrapper.
// If client is not nil, it will be used directly. Otherwise, opts will be used to create a new client.
func NewClient(redisCli *redis.Client, opts *Options) (thing.CacheClient, error) {
	var rdb *redis.Client
	var createdInternally bool

	if redisCli != nil {
		rdb = redisCli
		createdInternally = false
	} else {
		if opts == nil {
			opts = &Options{}
		}
		redisOpts := &redis.Options{
			Addr:     opts.Addr,
			Password: opts.Password,
			DB:       opts.DB,
		}
		rdb = redis.NewClient(redisOpts)
		createdInternally = true

		// Ping Redis to check connection
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		status := rdb.Ping(ctx)
		if err := status.Err(); err != nil {
			return nil, fmt.Errorf("failed to ping redis: %w", err)
		}
	}

	log.Println("Redis cache client initialized successfully.")
	return &client{redisClient: rdb, counters: make(map[string]int), createdInternally: createdInternally}, nil
}

// Get retrieves a raw string value from Redis.
func (c *client) Get(ctx context.Context, key string) (string, error) {
	c.incrementCounter("Get") // total calls
	val, err := c.redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		c.incrementCounter("GetMiss")
		return "", common.ErrNotFound
	} else if err != nil {
		c.incrementCounter("GetError")
		return "", fmt.Errorf("redis Get error for key '%s': %w", key, err)
	}
	c.incrementCounter("GetHit")
	return val, nil
}

// Set stores a raw string value in Redis.
func (c *client) Set(ctx context.Context, key string, value string, expiration time.Duration) error {
	c.incrementCounter("Set")
	err := c.redisClient.Set(ctx, key, value, expiration).Err()
	if err != nil {
		return fmt.Errorf("redis Set error for key '%s': %w", key, err)
	}
	return nil
}

// Delete removes a key from Redis.
func (c *client) Delete(ctx context.Context, key string) error {
	c.incrementCounter("Delete")
	err := c.redisClient.Del(ctx, key).Err()
	if err != nil && err != redis.Nil { // Don't error if key didn't exist
		return fmt.Errorf("redis Del error for key '%s': %w", key, err)
	}
	return nil
}

// GetModel retrieves a model from Redis.
func (c *client) GetModel(ctx context.Context, key string, dest interface{}) error {
	c.incrementCounter("GetModel")
	val, err := c.redisClient.Get(ctx, key).Result() // Use Result() to get string value
	if err == redis.Nil {
		c.incrementCounter("GetModelMiss")
		return common.ErrNotFound
	} else if err != nil {
		c.incrementCounter("GetModelError")
		return fmt.Errorf("redis Get error for key '%s': %w", key, err)
	}

	// Check for NoneResult marker AFTER checking for redis.Nil
	if val == common.NoneResult {
		c.incrementCounter("GetModelMiss")
		return common.ErrCacheNoneResult
	}
	c.incrementCounter("GetModelHit")

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
	c.incrementCounter("SetModel")
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
	c.incrementCounter("DeleteModel")
	err := c.redisClient.Del(ctx, key).Err()
	if err != nil && err != redis.Nil { // Don't error if key didn't exist
		return fmt.Errorf("redis Del error for key '%s': %w", key, err)
	}
	return nil
}

// GetQueryIDs retrieves a list of IDs from Redis.
// It now checks for the NoneResult marker and returns ErrQueryCacheNoneResult if found.
func (c *client) GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error) {
	c.incrementCounter("GetQueryIDs")
	val, err := c.redisClient.Get(ctx, queryKey).Result() // Use Result() to get string directly
	if err == redis.Nil {
		c.incrementCounter("GetQueryIDsMiss")
		return nil, common.ErrNotFound
	} else if err != nil {
		c.incrementCounter("GetQueryIDsError")
		return nil, fmt.Errorf("redis Get error for query key '%s': %w", queryKey, err)
	}
	c.incrementCounter("GetQueryIDsHit")

	// Remove the check for the no longer used NoneResult marker
	// if val == common.NoneResult {
	// 	return nil, common.ErrQueryCacheNoneResult
	// }

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
	c.incrementCounter("SetQueryIDs")
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
	c.incrementCounter("DeleteQueryIDs")
	err := c.redisClient.Del(ctx, queryKey).Err()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("redis Del error for query key '%s': %w", queryKey, err)
	}
	return nil
}

// AcquireLock tries to acquire a lock using Redis SETNX.
func (c *client) AcquireLock(ctx context.Context, lockKey string, expiration time.Duration) (bool, error) {
	c.incrementCounter("AcquireLock")
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
	c.incrementCounter("ReleaseLock")
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
	c.incrementCounter("DeleteByPrefix")
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

// GetCacheStats returns a snapshot of cache operation counters for monitoring.
// The returned map is a copy and safe for concurrent use.
// Typical keys: "Get", "GetMiss", "GetModel", "GetModelMiss", etc.
func (c *client) GetCacheStats(ctx context.Context) thing.CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	stats := make(map[string]int, len(c.counters))
	for k, v := range c.counters {
		stats[k] = v
	}
	return thing.CacheStats{Counters: stats}
}
