package cache

import (
	"hash/fnv"
	"sync"
)

const lockShardCount = 256

// CacheKeyLockManagerInternal manages a fixed set of sharded mutexes.
// Cache keys are mapped to a shard via FNV hash, eliminating the sync.Map
// memory leak where per-key mutexes were never cleaned up.
type CacheKeyLockManagerInternal struct {
	shards [lockShardCount]sync.Mutex
}

// NewCacheKeyLockManagerInternal creates a new lock manager.
func NewCacheKeyLockManagerInternal() *CacheKeyLockManagerInternal {
	return &CacheKeyLockManagerInternal{}
}

func (m *CacheKeyLockManagerInternal) shard(key string) *sync.Mutex {
	h := fnv.New32a()
	h.Write([]byte(key))
	return &m.shards[h.Sum32()%lockShardCount]
}

// Lock acquires the mutex shard for the given cache key.
func (m *CacheKeyLockManagerInternal) Lock(key string) {
	if key == "" {
		return
	}
	m.shard(key).Lock()
}

// Unlock releases the mutex shard for the given cache key.
func (m *CacheKeyLockManagerInternal) Unlock(key string) {
	if key == "" {
		return
	}
	m.shard(key).Unlock()
}

// --- Global Instance ---

// GlobalCacheKeyLocker provides a global instance of the lock manager for cache keys.
var GlobalCacheKeyLocker = NewCacheKeyLockManagerInternal()
