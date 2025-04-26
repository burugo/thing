package thing

import (
	"context"
	"database/sql"
	"time"
)

// Model defines the basic requirements for a struct to be managed by the ORM.
// It's intentionally minimal, relying on the embedded BaseModel and struct tags
// for most configuration (like table name, primary key).
type Model interface {
	GetID() int64
	SetID(id int64)
	TableName() string
	// Consider adding methods if reflection becomes too slow or complex,
	// e.g., GetFieldMap() map[string]interface{}
}

// CacheClient defines the interface for the caching layer.
type CacheClient interface {
	// Get retrieves a single model from the cache. `dest` should be a pointer to the model struct.
	// Returns ErrNotFound if not found, potentially a specific constant like `NoneResult` internally.
	GetModel(ctx context.Context, key string, dest Model) error
	// Set stores a single model in the cache.
	SetModel(ctx context.Context, key string, model Model, expiration time.Duration) error
	// Delete removes a single model from the cache.
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

	// Potentially add methods for atomic operations if needed (e.g., Incr)
}

// DBAdapter defines the interface for database interactions.
// Implementations will handle database-specific SQL dialects for the ORM's supported features.
type DBAdapter interface {
	// Get executes a query expected to return a single row and scans it into dest.
	Get(ctx context.Context, dest Model, query string, args ...interface{}) error
	// Select executes a query expected to return multiple rows and scans them into dest. dest must be a slice of models.
	Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	// Exec executes a query that doesn't return rows (INSERT, UPDATE, DELETE).
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)

	// BeginTx starts a transaction.
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error)

	// Close releases database resources.
	Close() error

	// TODO: Add Ping or other health check methods?
}

// Tx defines the interface for transaction operations, mirroring DBAdapter but operating within a transaction.
type Tx interface {
	Get(ctx context.Context, dest Model, query string, args ...interface{}) error
	Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Commit() error
	Rollback() error
}
