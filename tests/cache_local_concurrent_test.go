package thing_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/burugo/thing"
)

func TestLocalCache_ConcurrentIncr(t *testing.T) {
	ctx := context.Background()
	// We are testing the global DefaultLocalCache instance.
	// For more isolated tests, instantiate a new localCache if its creation was exported.
	cache := thing.DefaultLocalCache

	t.Run("ConcurrentIncrAtomicity", func(t *testing.T) {
		const goroutines = 50
		const increments = 5
		counterKey := "test_concurrent_counter_atomicity"

		// Clean up the key before test
		_ = cache.Delete(ctx, counterKey)
		t.Cleanup(func() {
			_ = cache.Delete(ctx, counterKey) // Ensure cleanup after test
		})

		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < increments; j++ {
					_, err := cache.Incr(ctx, counterKey)
					if err != nil {
						t.Errorf("Goroutine %d: Error incrementing: %v", id, err)
					}
				}
			}(i)
		}
		wg.Wait()

		finalValStr, err := cache.Get(ctx, counterKey)
		if err != nil {
			t.Fatalf("Error getting final value for %s: %v", counterKey, err)
		}

		expectedVal := int64(goroutines * increments)
		finalVal, parseErr := strconv.ParseInt(finalValStr, 10, 64)
		if parseErr != nil {
			t.Fatalf("Error parsing final value '%s': %v", finalValStr, parseErr)
		}

		if finalVal != expectedVal {
			t.Errorf("Expected final counter value %d, got %d", expectedVal, finalVal)
		}
	})
}

func TestLocalCache_Locking(t *testing.T) {
	ctx := context.Background()
	cache := thing.DefaultLocalCache

	t.Run("LockMechanism", func(t *testing.T) {
		var sharedResource int
		lockKey := "test_lock_mechanism"
		const lockCompetitors = 5

		var lockWg sync.WaitGroup
		lockedOperation := func(id int) {
			defer lockWg.Done()

			acquired, err := cache.AcquireLock(ctx, lockKey, 1*time.Second) // Use a timeout for the test
			if err != nil {
				t.Errorf("Goroutine %d: AcquireLock failed: %v", id, err)
				return
			}

			if !acquired {
				t.Errorf("Goroutine %d: Failed to acquire lock", id)
				return
			}

			// Simulate critical section work
			oldValue := sharedResource
			time.Sleep(10 * time.Millisecond)
			sharedResource = oldValue + 1

			err = cache.ReleaseLock(ctx, lockKey)
			if err != nil {
				t.Errorf("Goroutine %d: ReleaseLock failed: %v", id, err)
			}
		}

		for i := 0; i < lockCompetitors; i++ {
			lockWg.Add(1)
			go lockedOperation(i)
		}
		lockWg.Wait()

		if sharedResource != lockCompetitors {
			t.Errorf("Expected final shared resource value %d, got %d", lockCompetitors, sharedResource)
		}
	})
}
