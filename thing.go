package thing

import (
	"context"
	"errors"
	"fmt"
	"log"     // For placeholder logging
	"reflect" // Added for parsing env var
	"thing/internal/sqlbuilder"
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
	info    *ModelInfo             // Pre-computed metadata for type T
	builder *sqlbuilder.SQLBuilder // Add builder field
}

// --- Thing Constructors & Accessors ---

// New creates a new Thing instance with default context.Background().
// Made generic: requires type parameter T when called, e.g., New[MyModel](...).
func New[T Model](db DBAdapter, cache CacheClient) (*Thing[T], error) {
	log.Println("DEBUG: Entering New[T]") // Added log
	if db == nil {
		log.Println("DEBUG: New[T] - DB is nil") // Added log
		return nil, errors.New("DBAdapter must be non-nil")
	}
	if cache == nil {
		log.Println("DEBUG: New[T] - Cache is nil") // Added log
		return nil, errors.New("CacheClient must be non-nil")
	}
	log.Println("New Thing instance created.")

	// Pre-compute model info for T
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	log.Printf("DEBUG: New[T] - Getting model info for type: %s", modelType.Name()) // Added log
	info, err := GetCachedModelInfo(modelType)                                      // Renamed: getCachedModelInfo -> GetCachedModelInfo
	if err != nil {
		log.Printf("DEBUG: New[T] - Error getting model info: %v", err) // Added log
		return nil, fmt.Errorf("failed to get model info for type %s: %w", modelType.Name(), err)
	}
	log.Printf("DEBUG: New[T] - Got model info: %+v", info) // Added log
	// TableName is now populated within GetCachedModelInfo
	// info.TableName = getTableNameFromType(modelType) // Removed redundant call
	if info.TableName == "" {
		log.Printf("Warning: Could not determine table name for type %s during New. Relying on instance method?", modelType.Name())
	}

	log.Println("DEBUG: New[T] - Creating Thing struct") // Added log
	t := &Thing[T]{
		db:      db,
		cache:   cache,
		ctx:     context.Background(),
		info:    info,
		builder: db.Builder(),
	}
	log.Println("DEBUG: New[T] - Returning new Thing instance") // Added log
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

// DBAdapter returns the underlying DBAdapter for advanced use cases.
func (t *Thing[T]) DBAdapter() DBAdapter {
	return t.db
}
