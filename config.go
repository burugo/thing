package thing

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/burugo/thing/internal/logging"
)

// --- Global Configuration ---

var (
	globalDB     DBAdapter
	globalCache  CacheClient
	isConfigured bool
	configMutex  sync.RWMutex
	// Global cache TTL stored atomically (nanoseconds) for lock-free reads.
	atomicCacheTTLNs atomic.Int64
	// Cache version for query cache key isolation across restarts
	cacheVersion int64
)

// globalCacheTTL returns the current global cache TTL, safe for concurrent reads.
func getGlobalCacheTTL() time.Duration {
	return time.Duration(atomicCacheTTLNs.Load())
}

// setGlobalCacheTTL stores the TTL atomically.
func setGlobalCacheTTL(d time.Duration) {
	atomicCacheTTLNs.Store(int64(d))
}

// Config holds configuration for the Thing ORM.
type Config struct {
	DB       DBAdapter   // User must initialize and provide
	Cache    CacheClient // User must initialize and provide
	TTL      time.Duration
	Logger   Logger
	LogLevel LogLevel
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

	// Set cache version only on first configuration (survives multiple Configure calls)
	if !isConfigured {
		cacheVersion = time.Now().Unix()
		log.Printf("thing.Configure: cacheVersion set to %d", cacheVersion)
	}

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
		setGlobalCacheTTL(ttl)
	} else {
		setGlobalCacheTTL(defaultTTL)
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
		return errors.New("dbAdapter must be non-nil")
	}
	if cfg.Cache == nil {
		return errors.New("cacheClient must be non-nil")
	}
	if cfg.Logger != nil {
		SetLogger(cfg.Logger)
	}
	if cfg.LogLevel != LogDefault {
		SetLogLevel(cfg.LogLevel)
	}
	if cfg.TTL > 0 {
		return Configure(cfg.DB, cfg.Cache, cfg.TTL)
	}
	return Configure(cfg.DB, cfg.Cache)
}

// GetCacheVersion returns the current cache version for query cache key generation.
// This version changes on each application restart, ensuring stale cache keys are ignored.
func GetCacheVersion() int64 {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return cacheVersion
}
