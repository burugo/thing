package thing

import (
	"context"
	"database/sql"
	"time"
)

// CacheClient defines the interface for the caching layer.
type CacheClient interface {
	// Get retrieves a raw string value from the cache.
	// Returns ErrNotFound if the key doesn't exist.
	Get(ctx context.Context, key string) (string, error)
	// Set stores a raw string value in the cache.
	Set(ctx context.Context, key string, value string, expiration time.Duration) error
	// Delete removes a raw key from the cache.
	Delete(ctx context.Context, key string) error

	// GetModel retrieves a single model from the cache. `dest` should be a pointer
	// satisfying the Model interface (e.g., *User).
	GetModel(ctx context.Context, key string, dest interface{}) error // Changed dest to interface{} for flexibility
	// SetModel stores a single model in the cache. `model` should be a pointer
	// satisfying the Model interface (e.g., *User).
	SetModel(ctx context.Context, key string, model interface{}, expiration time.Duration) error // Changed model to interface{}
	// DeleteModel removes a single model from the cache.
	DeleteModel(ctx context.Context, key string) error

	// GetQueryIDs retrieves a list of IDs for a query result from the cache.
	GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error)
	// SetQueryIDs stores a list of IDs for a query result in the cache.
	SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error
	// DeleteQueryIDs removes a query result from the cache.
	DeleteQueryIDs(ctx context.Context, queryKey string) error

	// AcquireLock attempts to acquire a distributed lock.
	AcquireLock(ctx context.Context, lockKey string, expiration time.Duration) (bool, error)
	// ReleaseLock releases a distributed lock.
	ReleaseLock(ctx context.Context, lockKey string) error

	// DeleteByPrefix removes all cache entries whose keys match the given prefix.
	// Implementations should use efficient ways to find keys (e.g., SCAN in Redis).
	DeleteByPrefix(ctx context.Context, prefix string) error

	// InvalidateQueriesContainingID finds query cache entries matching the prefix
	// and deletes any entry whose cached ID list contains the specified id.
	// Implementations should use efficient ways to find keys (e.g., SCAN in Redis).
	InvalidateQueriesContainingID(ctx context.Context, prefix string, id int64) error

	// Potentially add methods for atomic operations if needed (e.g., Incr)
}

// DBAdapter defines the interface for database interactions.
// Implementations will handle database-specific SQL dialects for the ORM's supported features.
type DBAdapter interface {
	// Get executes a query expected to return a single row and scans it into dest.
	// `dest` should be a pointer satisfying the Model interface (e.g., *User).
	Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error // Changed dest to interface{}
	// Select executes a query expected to return multiple rows and scans them into dest. dest must be a pointer to a slice of pointers (e.g., *[]*User).
	Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	// Exec executes a query that doesn't return rows (INSERT, UPDATE, DELETE).
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)

	// GetCount executes a SELECT COUNT(*) query based on the provided parameters.
	GetCount(ctx context.Context, info *ModelInfo, params QueryParams) (int64, error)

	// SelectPaginated executes a query including WHERE, ORDER BY, LIMIT, and OFFSET
	// clauses, scanning results into dest.
	// dest must be a pointer to a slice of pointers (e.g., *[]*User).
	SelectPaginated(ctx context.Context, dest interface{}, info *ModelInfo, params QueryParams, offset int, limit int) error

	// BeginTx starts a transaction.
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error)

	// Close releases database resources.
	Close() error

	// TODO: Add Ping or other health check methods?
}

// Tx defines the interface for transaction operations, mirroring DBAdapter but operating within a transaction.
type Tx interface {
	// Get executes a query within the transaction.
	// `dest` should be a pointer satisfying the Model interface (e.g., *User).
	Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error // Changed dest to interface{}
	// Select executes a query within the transaction.
	// dest must be a pointer to a slice of pointers (e.g., *[]*User).
	Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	// Exec executes a query within the transaction.
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Commit() error
	Rollback() error
}

// --- Exported Constructor Wrappers (if implementations are internal) ---
