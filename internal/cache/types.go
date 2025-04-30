package cache

import (
	"context"
	"errors"
	"time"
)

// --- Local Interfaces/Structs/Vars for internal/cache --- //

// Local CacheClient interface defining only methods needed internally
type CacheClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, expiration time.Duration) error
	SetQueryIDs(ctx context.Context, queryKey string, ids []int64, expiration time.Duration) error
	// Add other methods like Delete, GetModel etc. if cache_helpers needs them
}

// Local QueryParams matching root structure
type QueryParams struct {
	Where    string
	Args     []interface{}
	Order    string
	Preloads []string
}

// Local ModelInfo matching necessary fields from root structure
// Only include fields accessed by internal/cache functions (like CheckQueryMatch)
type ModelInfo struct {
	TableName        string
	ColumnToFieldMap map[string]string
	// Add other fields like PkName if needed by internal funcs
}

// Local error variable equivalent to thing.ErrNotFound
var ErrNotFound = errors.New("cache: key not found") // Simple local definition
