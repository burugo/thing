package thing

import (
	"errors"
	"log"
	"sync"
	"time"

	sqlite "github.com/burugo/thing/internal/drivers/db/sqlite"
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
//	Configure() // uses in-memory SQLite and local cache
//	Configure(db) // uses provided DB, local cache
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
	case 0:
		// No arguments: use in-memory SQLite and local cache
		var err error
		db, err = sqlite.NewSQLiteAdapter(":memory:")
		if err != nil {
			return errors.New("Configure: failed to create in-memory SQLite adapter: " + err.Error())
		}
		cache = defaultLocalCache
		log.Println("thing.Configure: Using in-memory SQLite and local cache.")
	case 1:
		// One argument: must be DBAdapter
		db, _ = args[0].(DBAdapter)
		if db == nil {
			return errors.New("Configure: first argument must be a DBAdapter or nil")
		}
		cache = defaultLocalCache
		log.Println("thing.Configure: Using provided DBAdapter and local cache.")
	case 2:
		// Two arguments: DBAdapter, CacheClient
		db, _ = args[0].(DBAdapter)
		cache, _ = args[1].(CacheClient)
		if db == nil {
			return errors.New("Configure: first argument must be a DBAdapter or nil")
		}
		if cache == nil {
			cache = defaultLocalCache
			log.Println("thing.Configure: Cache is nil, using defaultLocalCache")
		}
	case 3:
		// Three arguments: DBAdapter, CacheClient, TTL
		db, _ = args[0].(DBAdapter)
		cache, _ = args[1].(CacheClient)
		ttl, _ = args[2].(time.Duration)
		if db == nil {
			return errors.New("Configure: first argument must be a DBAdapter or nil")
		}
		if cache == nil {
			cache = defaultLocalCache
			log.Println("thing.Configure: Cache is nil, using defaultLocalCache")
		}
	default:
		return errors.New("Configure: too many arguments")
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
