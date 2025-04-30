package thing

import (
	"errors"
	"log"
	"sync"
	"time"
)

// --- Global Configuration ---

var (
	globalDB     DBAdapter   // Set via Configure
	globalCache  CacheClient // Set via Configure
	isConfigured bool
	configMutex  sync.RWMutex
	// Global cache TTL, determined at startup
	globalCacheTTL time.Duration
)

// Configure sets up the package-level database and cache clients, and the global cache TTL.
// This MUST be called once during application initialization before using Use[T].
// It accepts an optional time.Duration argument to set the global cache TTL.
// If no TTL is provided, it defaults to 8 hours.
func Configure(db DBAdapter, cache CacheClient, ttl ...time.Duration) error { // Added optional ttl parameter
	configMutex.Lock()
	defer configMutex.Unlock()

	defaultTTL := 8 * time.Hour // Define default TTL

	// --- Configure Global TTL ---
	if len(ttl) > 0 {
		// Use the first provided TTL value if it's positive
		if ttl[0] > 0 {
			globalCacheTTL = ttl[0]
			log.Printf("Configuring global cache TTL: %s (from argument)", globalCacheTTL)
		} else {
			globalCacheTTL = defaultTTL
			log.Printf("Warning: Provided TTL is not positive (%s). Using default: %s", ttl[0], globalCacheTTL)
		}
	} else {
		// No TTL provided, use the default
		globalCacheTTL = defaultTTL
		log.Printf("Configuring global cache TTL: %s (default)", globalCacheTTL)
	}
	// --- End Configure Global TTL ---
	// if isConfigured {
	// 	log.Println("Thing package already configured. Re-configuring...")
	// }

	if db == nil {
		return errors.New("DBAdapter must be non-nil")
	}
	if cache == nil {
		return errors.New("CacheClient must be non-nil")
	}
	globalDB = db
	globalCache = cache
	isConfigured = true
	log.Println("Thing package configured globally with DB and Cache adapters.")

	return nil
}
