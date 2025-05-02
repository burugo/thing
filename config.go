package thing

import (
	"errors"
	"log"
	"sync"
	"time"
)

// --- Global Configuration ---

var (
	globalDB     DBAdapter
	globalCache  CacheClient
	isConfigured bool
	configMutex  sync.RWMutex
	// Global cache TTL, determined at startup
	globalCacheTTL time.Duration
)

// Config holds configuration for the Thing ORM.
type Config struct {
	DB    DBAdapter   // User must initialize and provide
	Cache CacheClient // User must initialize and provide
	TTL   time.Duration
}

// Configure sets up the package-level database and cache clients, and the global cache TTL.
// This MUST be called once during application initialization before using Use[T].
// It accepts an optional time.Duration argument to set the global cache TTL.
// If no TTL is provided, it defaults to 8 hours.
func Configure(db DBAdapter, cache CacheClient, ttl ...time.Duration) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	defaultTTL := 8 * time.Hour
	if len(ttl) > 0 && ttl[0] > 0 {
		globalCacheTTL = ttl[0]
	} else {
		globalCacheTTL = defaultTTL
	}
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

// ConfigureWithConfig sets up the package-level database and cache clients using a Config struct.
func ConfigureWithConfig(cfg Config) error {
	if cfg.DB == nil {
		return errors.New("DBAdapter must be non-nil")
	}
	if cfg.Cache == nil {
		return errors.New("CacheClient must be non-nil")
	}
	if cfg.TTL > 0 {
		return Configure(cfg.DB, cfg.Cache, cfg.TTL)
	}
	return Configure(cfg.DB, cfg.Cache)
}
