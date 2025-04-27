package redis

import (
	"context"
	"encoding/json"
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

// GetModel retrieves a model from Redis.
func (c *client) GetModel(ctx context.Context, key string, dest interface{}) error {
	val, err := c.redisClient.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return thing.ErrNotFound // Use standard not found error
	} else if err != nil {
		return fmt.Errorf("redis Get error for key '%s': %w", key, err)
	}
	err = json.Unmarshal(val, dest)
	if err != nil {
		// Consider logging the problematic data: log.Printf("Unmarshal error for key '%s', data: %s", key, string(val))
		return fmt.Errorf("redis Unmarshal error for key '%s': %w", key, err)
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
func (c *client) GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error) {
	val, err := c.redisClient.Get(ctx, queryKey).Bytes()
	if err == redis.Nil {
		return nil, thing.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("redis Get error for query key '%s': %w", queryKey, err)
	}
	var ids []int64
	err = json.Unmarshal(val, &ids)
	if err != nil {
		return nil, fmt.Errorf("redis Unmarshal error for query key '%s': %w", queryKey, err)
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
