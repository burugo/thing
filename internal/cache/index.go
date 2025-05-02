package cache

import (
	"fmt"
	"log"
	"sync"
)

// Global instance of the cache index.
// This should be initialized once, potentially during application startup.
var GlobalCacheIndex = NewCacheIndex()

// CacheIndex tracks which query cache keys might be affected by changes to a specific table.
// It maintains a map from table names to a set of cache keys, and a map from cache keys
// back to their corresponding QueryParams.
// EXPORTED
type CacheIndex struct {
	// tableToQueries maps a table name (string) to a set of query cache keys (map[string]bool)
	// associated with that table.
	tableToQueries map[string]map[string]bool

	// keyToParams maps a query cache key (string) back to the QueryParams used to generate it.
	// This is needed to evaluate if a changed model matches the query conditions.
	keyToParams map[string]QueryParams

	// valueIndex maps table -> field -> value (as string) -> set of cache keys.
	// Only for exact match ("=", "IN") queries. Used for高效失效定位。
	// Example: valueIndex["users"]["user_id"]["42"] = {"list:users:hash1": true, ...}
	valueIndex map[string]map[string]map[string]map[string]bool

	// FieldIndex maps table -> field -> set of cache keys (for fallback, e.g. range queries)
	// Example: FieldIndex["users"]["age"] = {"list:users:hash2": true, ...}
	FieldIndex map[string]map[string]map[string]bool

	mu sync.RWMutex // Protects access to all maps
}

// NewCacheIndex creates and initializes a new CacheIndex.
func NewCacheIndex() *CacheIndex {
	return &CacheIndex{
		tableToQueries: make(map[string]map[string]bool),
		keyToParams:    make(map[string]QueryParams),
		valueIndex:     make(map[string]map[string]map[string]map[string]bool),
		FieldIndex:     make(map[string]map[string]map[string]bool),
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

	// --- 新增: 注册值级索引 ---
	exactFields := ParseExactMatchFields(params)
	for field, vals := range exactFields {
		if _, ok := idx.valueIndex[tableName]; !ok {
			idx.valueIndex[tableName] = make(map[string]map[string]map[string]bool)
		}
		if _, ok := idx.valueIndex[tableName][field]; !ok {
			idx.valueIndex[tableName][field] = make(map[string]map[string]bool)
		}
		for _, v := range vals {
			valStr := toIndexValueString(v)
			if _, ok := idx.valueIndex[tableName][field][valStr]; !ok {
				idx.valueIndex[tableName][field][valStr] = make(map[string]bool)
			}
			idx.valueIndex[tableName][field][valStr][cacheKey] = true
		}
	}

	// --- 新增: 注册字段级索引 ---
	fields := extractAllWhereFields(params)
	for _, field := range fields {
		if _, ok := idx.FieldIndex[tableName]; !ok {
			idx.FieldIndex[tableName] = make(map[string]map[string]bool)
		}
		if _, ok := idx.FieldIndex[tableName][field]; !ok {
			idx.FieldIndex[tableName][field] = make(map[string]bool)
		}
		idx.FieldIndex[tableName][field][cacheKey] = true
	}
}

// toIndexValueString 将索引值转为字符串，便于 map key
func toIndexValueString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	case float64:
		return fmt.Sprintf("%g", x)
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprintf("%v", x)
	}
}

// extractAllWhereFields 提取 WHERE 中所有字段名
func extractAllWhereFields(params QueryParams) []string {
	var fields []string
	where := params.Where
	if where == "" {
		return fields
	}
	conditions := splitAndConditions(where)
	for _, cond := range conditions {
		cond = trimSpace(cond)
		if cond == "" {
			continue
		}
		parts := splitFields(cond)
		if len(parts) >= 1 {
			fields = append(fields, parts[0])
		}
	}
	return fields
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

// ResetGlobalCacheIndex resets the global cache index to a new empty state.
// Primarily useful for testing purposes to ensure test isolation.
func ResetGlobalCacheIndex() {
	GlobalCacheIndex = NewCacheIndex()
	log.Println("DEBUG: Global Cache Index Reset") // Add log for visibility
}

// TODO: Consider adding a DeregisterQuery method if cache keys can expire or become invalid permanently.
// This would need to remove entries from both maps.
// TODO: Consider persistence options if the index needs to survive application restarts.

// ParseExactMatchFields parses QueryParams and returns a map of field name to value slice for all exact match (=, IN) conditions.
// Only supports AND 连接的简单条件。
func ParseExactMatchFields(params QueryParams) map[string][]interface{} {
	result := make(map[string][]interface{})
	where := params.Where
	args := params.Args
	if where == "" || len(args) == 0 {
		return result
	}
	conditions := splitAndConditions(where)
	argIdx := 0
	for _, cond := range conditions {
		cond = trimSpace(cond)
		if cond == "" {
			continue
		}
		parts := splitFields(cond)
		if len(parts) == 3 && parts[1] == "=" && parts[2] == "?" && argIdx < len(args) {
			field := parts[0]
			result[field] = []interface{}{args[argIdx]}
			argIdx++
		} else if len(parts) == 3 && parts[1] == "IN" && parts[2] == "(?)" && argIdx < len(args) {
			field := parts[0]
			arg := args[argIdx]
			var vals []interface{}
			switch v := arg.(type) {
			case []interface{}:
				vals = v
			case []int:
				for _, n := range v {
					vals = append(vals, n)
				}
			case []string:
				for _, s := range v {
					vals = append(vals, s)
				}
			default:
				// 不支持的类型，跳过
			}
			if len(vals) > 0 {
				result[field] = vals
			}
			argIdx++
		} else if argIdx < len(args) {
			// 非 =/IN 条件，参数也要递增
			argIdx++
		}
	}
	return result
}

// splitAndConditions splits a WHERE string by AND (case-insensitive)
func splitAndConditions(where string) []string {
	var res []string
	for _, s := range splitByAND(where) {
		res = append(res, trimSpace(s))
	}
	return res
}

// splitFields splits a condition string by whitespace
func splitFields(s string) []string {
	var res []string
	curr := ""
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			if curr != "" {
				res = append(res, curr)
				curr = ""
			}
		} else {
			curr += string(s[i])
		}
	}
	if curr != "" {
		res = append(res, curr)
	}
	return res
}

// splitByAND splits by AND (case-insensitive)
func splitByAND(s string) []string {
	var res []string
	last := 0
	for i := 0; i+3 <= len(s); i++ {
		if (s[i] == 'A' || s[i] == 'a') && (s[i+1] == 'N' || s[i+1] == 'n') && (s[i+2] == 'D' || s[i+2] == 'd') {
			if (i == 0 || s[i-1] == ' ') && (i+3 == len(s) || s[i+3] == ' ') {
				res = append(res, s[last:i])
				last = i + 3
			}
		}
	}
	res = append(res, s[last:])
	return res
}

// trimSpace removes leading/trailing spaces/tabs
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// GetKeysByValue returns all cache keys registered for a given table, field, and value (as interface{}).
// If no keys are found, returns an empty slice.
func (idx *CacheIndex) GetKeysByValue(table, field string, value interface{}) []string {
	if table == "" || field == "" {
		return nil
	}
	valStr := toIndexValueString(value)
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var keys []string
	if idx.valueIndex == nil {
		return keys
	}
	tbl, ok := idx.valueIndex[table]
	if !ok {
		return keys
	}
	fld, ok := tbl[field]
	if !ok {
		return keys
	}
	valMap, ok := fld[valStr]
	if !ok {
		return keys
	}
	for k := range valMap {
		keys = append(keys, k)
	}
	return keys
}
