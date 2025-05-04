package cache

import (
	"log"
	"sync"
)

// CacheKeyLockManagerInternal manages a map of mutexes, one for each cache key.
// It uses sync.Map for efficient concurrent access.
type CacheKeyLockManagerInternal struct {
	locks sync.Map // map[string]*sync.Mutex
}

// NewCacheKeyLockManagerInternal creates a new lock manager.
func NewCacheKeyLockManagerInternal() *CacheKeyLockManagerInternal {
	return &CacheKeyLockManagerInternal{}
}

// Lock acquires the mutex associated with the given cache key.
// If the mutex does not exist, it is created.
// This operation blocks until the lock is acquired.
func (m *CacheKeyLockManagerInternal) Lock(key string) {
	if key == "" {
		// Avoid locking on empty key, though this shouldn't happen in practice.
		return
	}
	// LoadOrStore ensures that only one mutex is created per key.
	// It returns the existing mutex if found, or stores and returns the new one.
	mutex, _ := m.locks.LoadOrStore(key, &sync.Mutex{})
	// log.Printf("DEBUG: Acquiring lock for key '%s'", key)
	mutex.(*sync.Mutex).Lock()
	// log.Printf("DEBUG: Acquired lock for key '%s'", key)
}

// Unlock releases the mutex associated with the given cache key.
// It is crucial to call Unlock after the critical section protected by Lock.
// Typically used with defer: `defer cacheKeyLocker.Unlock(key)`.
func (m *CacheKeyLockManagerInternal) Unlock(key string) {
	if key == "" {
		return
	}
	// Load retrieves the mutex associated with the key.
	if mutex, ok := m.locks.Load(key); ok {
		mutex.(*sync.Mutex).Unlock()
		// log.Printf("DEBUG: Released lock for key '%s'", key)
	} else {
		// This case should ideally not happen if Lock was called correctly.
		// Log a warning if it does.
		log.Printf("WARN: Attempted to Unlock a key ('%s') that was not locked or doesn't exist.", key)
	}

	// Note on garbage collection:
	// Mutexes stored in the sync.Map will not be garbage collected automatically
	// if the keys become obsolete. For long-running applications with a vast and
	// changing set of cache keys, a mechanism to occasionally sweep and remove
	// unused mutexes might be necessary to prevent memory leaks.
	// However, for many applications, the overhead is negligible.
	// Example cleanup strategy (run periodically):
	/*
	   m.locks.Range(func(key, value interface{}) bool {
	       mu := value.(*sync.Mutex)
	       // Try to acquire the lock briefly. If successful, it means it's likely unused.
	       if mu.TryLock() {
	           mu.Unlock() // Release immediately
	           // Consider removing the lock if it hasn't been used recently (requires tracking usage)
	           // m.locks.Delete(key)
	       }
	       return true
	   })
	*/
}

// --- Global Instance ---

// GlobalCacheKeyLocker provides a global instance of the lock manager for cache keys.
var GlobalCacheKeyLocker = NewCacheKeyLockManagerInternal()
