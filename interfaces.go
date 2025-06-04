// interfaces.go
// Core interfaces for Thing ORM: DBAdapter, CacheClient, SQLBuilder, Dialector, etc.
// These are public and intended for use by users and driver developers.

package thing

import (
	"context"
	"database/sql"
	"time"
)

// DBAdapter defines the interface for database drivers.
type DBAdapter interface {
	Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	GetCount(ctx context.Context, tableName string, where string, args []interface{}) (int64, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error)
	Close() error
	DB() *sql.DB
	Builder() SQLBuilder
	DialectName() string
}

// Tx defines the interface for transaction operations.
type Tx interface {
	Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Commit() error
	Rollback() error
}

// CacheClient defines the interface for cache drivers.
type CacheClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, expiration time.Duration) error
	Delete(ctx context.Context, key string) error

	GetModel(ctx context.Context, key string, dest interface{}) error
	SetModel(ctx context.Context, key string, model interface{}, fieldsToCache []string, expiration time.Duration) error
	DeleteModel(ctx context.Context, key string) error

	GetQueryIDs(ctx context.Context, queryKey string) ([]int64, error)
	SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error
	DeleteQueryIDs(ctx context.Context, queryKey string) error

	AcquireLock(ctx context.Context, lockKey string, expiration time.Duration) (bool, error)
	ReleaseLock(ctx context.Context, lockKey string) error

	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, expiration time.Duration) error

	GetCacheStats(ctx context.Context) CacheStats
}

// CacheStats holds cache operation counters for monitoring.
type CacheStats struct {
	Counters map[string]int // Operation name to count
}

// Dialector defines how to quote identifiers and bind variables for a specific SQL dialect.
type Dialector interface {
	Quote(identifier string) string // Quote a SQL identifier (table/column name)
	Placeholder(index int) string   // Bind variable placeholder (e.g. ?, $1)
}

// SQLBuilder defines the contract for SQL generation with dialect-specific identifier quoting.
type SQLBuilder interface {
	BuildSelectSQL(tableName string, columns []string) string
	BuildSelectIDsSQL(tableName string, pkName string, where string, args []interface{}, order string) (string, []interface{})
	BuildInsertSQL(tableName string, columns []string) string
	BuildUpdateSQL(tableName string, setClauses []string, pkName string) string
	BuildDeleteSQL(tableName, pkName string) string
	BuildCountSQL(tableName string, whereClause string) string
	Rebind(query string) string
}
