// DEPRECATED: All interfaces/types must now be imported from the main package (thing/interfaces.go). This file will be removed soon.
// Do not use these definitions; update all code to use thing.DBAdapter, thing.CacheClient, thing.Tx, thing.CacheStats, etc.

package interfaces

import (
	"context"
	"database/sql"
	"time"

	"github.com/burugo/thing/internal/schema"
	"github.com/burugo/thing/internal/sqlbuilder"
	"github.com/burugo/thing/internal/types"
)

// CacheClient defines the interface for the caching layer.
type CacheClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, expiration time.Duration) error
	Delete(ctx context.Context, key string) error

	GetModel(ctx context.Context, key string, dest interface{}) error
	SetModel(ctx context.Context, key string, model interface{}, expiration time.Duration) error
	DeleteModel(ctx context.Context, key string) error

	GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error)
	SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error
	DeleteQueryIDs(ctx context.Context, queryKey string) error

	AcquireLock(ctx context.Context, lockKey string, expiration time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, lockKey string) error

	GetCacheStats(ctx context.Context) CacheStats
}

// CacheStats holds cache operation counters for monitoring.
type CacheStats struct {
	Counters map[string]int // Operation name to count
}

// DBAdapter defines the interface for database interactions.
// Implementations will handle database-specific SQL dialects for the ORM's supported features.
type DBAdapter interface {
	Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	GetCount(ctx context.Context, info *schema.ModelInfo, params types.QueryParams) (int64, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error)
	Close() error
	DB() *sql.DB
	Builder() *sqlbuilder.SQLBuilder
	DialectName() string
}

// Tx defines the interface for transaction operations, mirroring DBAdapter but operating within a transaction.
type Tx interface {
	Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Commit() error
	Rollback() error
}
