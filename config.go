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
// Usage:
//
//	Configure(db) // uses provided DB, default local cache
//	Configure(db, cache) // uses provided DB and cache
//	Configure(db, cache, ttl) // uses all provided
//
// This MUST be called once during application initialization before using Use[T].
func Configure(args ...interface{}) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	defaultTTL := 8 * time.Hour
	var db DBAdapter
	var cache CacheClient
	var ttl time.Duration

	switch len(args) {
	case 1:
		// One argument: must be DBAdapter
		db, _ = args[0].(DBAdapter)
		if db == nil {
			return errors.New("Configure: first argument must be a DBAdapter")
		}
		cache = DefaultLocalCache
		log.Println("thing.Configure: Using provided DBAdapter and default local cache.")
	case 2:
		// Two arguments: DBAdapter, CacheClient
		db, _ = args[0].(DBAdapter)
		cache, _ = args[1].(CacheClient)
		if db == nil {
			return errors.New("Configure: first argument must be a DBAdapter")
		}
		if cache == nil {
			cache = DefaultLocalCache
			log.Println("thing.Configure: Cache is nil, using DefaultLocalCache")
		}
	case 3:
		// Three arguments: DBAdapter, CacheClient, TTL
		db, _ = args[0].(DBAdapter)
		cache, _ = args[1].(CacheClient)
		ttl, _ = args[2].(time.Duration)
		if db == nil {
			return errors.New("Configure: first argument must be a DBAdapter")
		}
		if cache == nil {
			cache = DefaultLocalCache
			log.Println("thing.Configure: Cache is nil, using DefaultLocalCache")
		}
	default:
		return errors.New("Configure: must provide at least a DBAdapter")
	}

	if ttl > 0 {
		globalCacheTTL = ttl
	} else {
		globalCacheTTL = defaultTTL
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
