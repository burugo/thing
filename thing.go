package thing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"     // For placeholder logging
	"reflect" // Added for parsing env var

	"github.com/burugo/thing/internal/schema"
)

// --- Thing Core Struct ---

// Model is the base interface for all ORM models.
type Model interface {
	KeepItem() bool
	GetID() int64
}

// Thing is the central access point for ORM operations, analogous to gorm.DB.
// It holds database/cache clients and the context for operations.
type Thing[T Model] struct {
	db      DBAdapter
	cache   CacheClient
	ctx     context.Context
	info    *schema.ModelInfo // Pre-computed metadata for type T
	builder SQLBuilder        // Use interface type
}

// --- Thing Constructors & Accessors ---

// New creates a new Thing instance with default context.Background().
// Accepts one or more CacheClient; if none provided, uses defaultLocalCache.
func New[T Model](db DBAdapter, cache CacheClient) (*Thing[T], error) {
	log.Println("DEBUG: Entering New[T]") // Added log
	if db == nil {
		log.Println("DEBUG: New[T] - DB is nil") // Added log
		return nil, errors.New("DBAdapter must be non-nil")
	}
	if cache == nil {
		log.Println("DEBUG: New[T] - No cache provided, using defaultLocalCache")
		cache = DefaultLocalCache
	}
	log.Println("New Thing instance created.")
	// Pre-compute model info for T
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	log.Printf("DEBUG: New[T] - Getting model info for type: %s", modelType.Name())
	info, err := schema.GetCachedModelInfo(modelType)
	if err != nil {
		log.Printf("DEBUG: New[T] - Error getting model info: %v", err)
		return nil, fmt.Errorf("failed to get model info for type %s: %w", modelType.Name(), err)
	}
	log.Printf("DEBUG: New[T] - Got model info: %+v", info)
	if info.TableName == "" {
		log.Printf("Warning: Could not determine table name for type %s during New. Relying on instance method?", modelType.Name())
	}
	log.Println("DEBUG: New[T] - Creating Thing struct")
	t := &Thing[T]{
		db:      db,
		cache:   cache,
		ctx:     context.Background(),
		info:    info,
		builder: db.Builder(),
	}
	log.Println("DEBUG: New[T] - Returning new Thing instance")
	return t, nil
}

// Use returns a Thing instance for the specified type T, using the globally
// configured DBAdapter and CacheClient.
// The package MUST be configured using Configure() before calling Use[T].
func Use[T Model]() (*Thing[T], error) {
	configMutex.RLock()
	defer configMutex.RUnlock()
	if !isConfigured {
		return nil, errors.New("thing.Use[T] called before thing.Configure()")
	}
	// Create a new Thing instance using the global adapters
	return New[T](globalDB, globalCache)
}

// --- Thing Public Methods ---

// WithContext returns a shallow copy of Thing with the context replaced.
// This is used to set the context for a specific chain of operations.
func (t *Thing[T]) WithContext(ctx context.Context) *Thing[T] { // Returns *Thing
	if ctx == nil {
		log.Println("Warning: nil context passed to WithContext, using context.Background()")
		ctx = context.Background()
	}
	// Create a shallow copy and replace the context
	newThing := *t     // Copy struct values (dbAdapter, cacheClient, old ctx)
	newThing.ctx = ctx // Set the new context
	return &newThing   // Return pointer to the copy
}

// Cache returns the underlying CacheClient associated with this Thing instance.
func (t *Thing[T]) Cache() CacheClient {
	return t.cache
}

// GlobalDB returns the global DBAdapter (for internal use, e.g., AutoMigrate)
func GlobalDB() DBAdapter {
	return globalDB
}

// DB returns the underlying *sql.DB for advanced/raw SQL use cases.
func (t *Thing[T]) DB() *sql.DB {
	return t.db.DB()
}
