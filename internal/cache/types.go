package cache

import (
	"errors"
)

// --- Local Interfaces/Structs/Vars for internal/cache --- //
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
