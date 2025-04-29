package thing

import "errors"

// ErrNotFound is returned when a requested item (e.g., cache key, database record) is not found.
var ErrNotFound = errors.New("thing: requested item not found")

// Additional package-level errors
var (
	ErrLockNotAcquired = errors.New("thing: could not acquire lock")
	// ErrQueryCacheNoneResult indicates that the cache holds the marker for an empty query result set.
	ErrQueryCacheNoneResult = errors.New("thing: cached query result indicates no matching records")
	// ErrCacheNoneResult indicates the cache key exists but holds the NoneResult marker.
	ErrCacheNoneResult = errors.New("thing: cache indicates record does not exist (NoneResult marker found)")
	ErrInvalidID       = errors.New("thing: invalid ID format")
	ErrModelNotSet     = errors.New("thing: model not set")
	ErrNilContext      = errors.New("thing: nil context provided")
	ErrTransactionDone = errors.New("thing: transaction has already been committed or rolled back")
	ErrDatabaseNotSet  = errors.New("thing: database adapter not set")
	ErrCacheNotSet     = errors.New("thing: cache client not set")
	ErrInvalidPage     = errors.New("thing: invalid page number, must be >= 1")
)

// TODO: Add other package-level errors here as needed.
