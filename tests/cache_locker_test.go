package thing_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Import the package we are testing
	"thing"
)

func TestCacheKeyLockManager_LockUnlock(t *testing.T) {
	locker := thing.NewCacheKeyLockManager()
	key := "test-key-1"

	// Lock and immediately unlock
	require.NotPanics(t, func() {
		locker.Lock(key)
		locker.Unlock(key)
	}, "Simple lock/unlock panicked")

	// Try locking again, should succeed immediately
	locked := make(chan bool)
	go func() {
		locker.Lock(key)
		// Signal that lock was acquired
		locked <- true
		locker.Unlock(key)
	}()

	select {
	case <-locked:
		// Test passed
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "Failed to re-acquire lock after unlock")
	}
}

func TestCacheKeyLockManager_ConcurrentLocking(t *testing.T) {
	locker := thing.NewCacheKeyLockManager()
	key := "concurrent-key"
	numGoroutines := 10
	var counter int
	var wg sync.WaitGroup

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			locker.Lock(key)
			// Critical section: Increment the counter
			current := counter
			time.Sleep(1 * time.Millisecond) // Simulate work
			counter = current + 1
			locker.Unlock(key)
		}()
	}

	wg.Wait()

	// Assert that the counter was incremented correctly (atomically)
	assert.Equal(t, numGoroutines, counter, "Counter was not incremented atomically by all goroutines")
}

func TestCacheKeyLockManager_DifferentKeys(t *testing.T) {
	locker := thing.NewCacheKeyLockManager()
	key1 := "key-diff-1"
	key2 := "key-diff-2"

	// Lock key1
	locker.Lock(key1)

	// Try to lock key2 in a separate goroutine, should succeed quickly
	lockedKey2 := make(chan bool)
	go func() {
		locker.Lock(key2)
		lockedKey2 <- true
		locker.Unlock(key2)
	}()

	select {
	case <-lockedKey2:
		// Success, locking key2 didn't block
	case <-time.After(50 * time.Millisecond):
		assert.Fail(t, "Locking a different key (key2) was blocked by key1")
	}

	// Unlock key1
	locker.Unlock(key1)
}

func TestCacheKeyLockManager_UnlockWarning(t *testing.T) {
	// This test relies on observing log output, which isn't ideal for automation,
	// but it checks the warning path.
	locker := thing.NewCacheKeyLockManager()
	key := "key-unlock-warn"

	// Unlock a key that was never locked
	require.NotPanics(t, func() {
		locker.Unlock(key)
	}, "Unlocking a non-locked key panicked")
	// Expect a "WARN: Attempted to Unlock..." message in the logs.
}

func TestCacheKeyLockManager_EmptyKey(t *testing.T) {
	locker := thing.NewCacheKeyLockManager()

	require.NotPanics(t, func() {
		locker.Lock("")
	}, "Locking empty key panicked")

	require.NotPanics(t, func() {
		locker.Unlock("")
	}, "Unlocking empty key panicked")

	// After locking and unlocking empty string, ensure another lock works
	require.NotPanics(t, func() {
		locker.Lock("real-key")
		locker.Unlock("real-key")
	}, "Locking real key after empty key ops panicked")
}
