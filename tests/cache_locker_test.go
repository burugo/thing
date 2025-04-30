package thing_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	// Import the package we are testing

	"thing/internal/cache"
)

func TestCacheKeyLockManager_LockUnlock(t *testing.T) {
	manager := cache.NewCacheKeyLockManager()
	key := "test_key_1"

	// Lock and unlock normally
	manager.Lock(key)
	// In a real scenario, a critical section would be here.
	manager.Unlock(key)

	// Try to lock again, should succeed
	done := make(chan bool)
	go func() {
		manager.Lock(key)
		manager.Unlock(key)
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting for second lock acquisition")
	}
}

func TestCacheKeyLockManager_ConcurrentLock(t *testing.T) {
	manager := cache.NewCacheKeyLockManager()
	key := "concurrent_key"
	numGoroutines := 10
	var wg sync.WaitGroup
	counter := 0

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			manager.Lock(key)
			// Critical section simulation
			temp := counter
			time.Sleep(1 * time.Millisecond) // Introduce potential race without lock
			counter = temp + 1
			manager.Unlock(key)
		}()
	}

	wg.Wait()
	assert.Equal(t, numGoroutines, counter, "Counter should be incremented by each goroutine")
}

func TestCacheKeyLockManager_MultipleKeys(t *testing.T) {
	manager := cache.NewCacheKeyLockManager()
	key1 := "multi_key_1"
	key2 := "multi_key_2"
	var wg sync.WaitGroup

	// Lock key1
	manager.Lock(key1)

	// Try to lock key2 concurrently, should succeed immediately
	doneKey2 := make(chan bool)
	wg.Add(1)
	go func() {
		defer wg.Done()
		manager.Lock(key2)
		manager.Unlock(key2)
		close(doneKey2)
	}()

	select {
	case <-doneKey2:
		// Key2 lock acquired successfully
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Timed out waiting to acquire lock on key2")
	}

	// Unlock key1
	manager.Unlock(key1)
	wg.Wait()
}

func TestCacheKeyLockManager_UnlockNonexistent(t *testing.T) {
	manager := cache.NewCacheKeyLockManager()
	// Unlocking a key that was never locked should not panic
	assert.NotPanics(t, func() {
		manager.Unlock("nonexistent_key")
	})
}

func TestCacheKeyLockManager_EmptyKey(t *testing.T) {
	manager := cache.NewCacheKeyLockManager()
	// Locking or unlocking an empty key should not panic or block indefinitely
	assert.NotPanics(t, func() {
		manager.Lock("")
	})
	assert.NotPanics(t, func() {
		manager.Unlock("")
	})
}
