package thing

import "sync"

// Global instance of the cache index.
// This should be initialized once, potentially during application startup.
var globalCacheIndex = NewCacheIndex()

// CacheIndex tracks which query cache keys might be affected by changes to a specific table.
// It maintains a map from table names to a set of cache keys, and a map from cache keys
// back to their corresponding QueryParams.
type CacheIndex struct {
	// tableToQueries maps a table name (string) to a set of query cache keys (map[string]bool)
	// associated with that table.
	tableToQueries map[string]map[string]bool

	// keyToParams maps a query cache key (string) back to the QueryParams used to generate it.
	// This is needed to evaluate if a changed model matches the query conditions.
	keyToParams map[string]QueryParams

	mu sync.RWMutex // Protects access to both maps
}

// NewCacheIndex creates and initializes a new CacheIndex.
func NewCacheIndex() *CacheIndex {
	return &CacheIndex{
		tableToQueries: make(map[string]map[string]bool),
		keyToParams:    make(map[string]QueryParams),
	}
}

// RegisterQuery registers a query cache key (and its associated QueryParams) as being
// associated with a specific table.
// This should be called when a query result (list or count) is cached.
// It is safe for concurrent use.
func (idx *CacheIndex) RegisterQuery(tableName, cacheKey string, params QueryParams) {
	if tableName == "" || cacheKey == "" {
		return // Ignore invalid input
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Register table -> key mapping
	if _, exists := idx.tableToQueries[tableName]; !exists {
		idx.tableToQueries[tableName] = make(map[string]bool)
	}
	idx.tableToQueries[tableName][cacheKey] = true

	// Register key -> params mapping
	idx.keyToParams[cacheKey] = params

	// log.Printf("DEBUG: Registered query cache key '%s' for table '%s' with params: %+v", cacheKey, tableName, params) // Optional debug log
}

// GetPotentiallyAffectedQueries returns a slice of all registered query cache keys
// associated with the given table name.
// This is used during Save/Delete operations to find caches that might need incremental updates.
// It returns an empty slice if the table name is not found or has no associated queries.
// It is safe for concurrent use.
func (idx *CacheIndex) GetPotentiallyAffectedQueries(tableName string) []string {
	if tableName == "" {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	keys := []string{} // Initialize as empty slice, not nil
	if queries, exists := idx.tableToQueries[tableName]; exists {
		// Allocate with estimated size for potential performance improvement
		keys = make([]string, 0, len(queries))
		for key := range queries {
			keys = append(keys, key)
		}
	}
	// log.Printf("DEBUG: Found %d potentially affected queries for table '%s'", len(keys), tableName) // Optional debug log
	return keys
}

// GetQueryParamsForKey returns the QueryParams associated with a given cache key.
// It returns the QueryParams and true if the key was found, otherwise zero QueryParams and false.
// It is safe for concurrent use.
func (idx *CacheIndex) GetQueryParamsForKey(cacheKey string) (QueryParams, bool) {
	if cacheKey == "" {
		return QueryParams{}, false
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	params, found := idx.keyToParams[cacheKey]
	return params, found
}

// TODO: Consider adding a DeregisterQuery method if cache keys can expire or become invalid permanently.
// This would need to remove entries from both maps.
// TODO: Consider persistence options if the index needs to survive application restarts.
