package cache

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"thing/internal/types"
)

// Global instance of the cache index.
// This should be initialized once, potentially during application startup.
var GlobalCacheIndex = NewCacheIndex()

// CacheIndex tracks which query cache keys might be affected by changes to a specific table.
// It maintains a map from table names to a set of cache keys, and a map from cache keys
// back to their corresponding QueryParams.
// EXPORTED
type CacheIndex struct {
	// keyToParams maps a query cache key (string) back to the QueryParams used to generate it.
	// This is needed to evaluate if a changed model matches the query conditions.
	keyToParams map[string]types.QueryParams

	// valueIndex maps table -> field -> value (as string) -> set of cache keys.
	// Only for exact match ("=", "IN") queries. Used for efficient invalidation location.
	// Example: valueIndex["users"]["user_id"]["42"] = {"list:users:hash1": true, ...}
	valueIndex map[string]map[string]map[string]map[string]bool

	// FieldIndex maps table -> field -> set of cache keys (for fallback, e.g. range queries)
	// Example: FieldIndex["users"]["age"] = {"list:users:hash2": true, ...}
	FieldIndex map[string]map[string]map[string]bool

	// TableToFullTableListKeys records all list cache keys with empty where clause (i.e., full table cache)
	TableToFullTableListKeys map[string]map[string]bool

	mu sync.RWMutex // Protects access to all maps
}

// NewCacheIndex creates and initializes a new CacheIndex.
func NewCacheIndex() *CacheIndex {
	return &CacheIndex{
		keyToParams:              make(map[string]types.QueryParams),
		valueIndex:               make(map[string]map[string]map[string]map[string]bool),
		FieldIndex:               make(map[string]map[string]map[string]bool),
		TableToFullTableListKeys: make(map[string]map[string]bool),
	}
}

// RegisterQuery registers a query cache key (and its associated QueryParams) as being
// associated with a specific table.
// This should be called when a query result (list or count) is cached.
// It is safe for concurrent use.
func (idx *CacheIndex) RegisterQuery(tableName, cacheKey string, params types.QueryParams) {
	if tableName == "" || cacheKey == "" {
		return // Ignore invalid input
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Register key -> params mapping
	idx.keyToParams[cacheKey] = params

	// --- 新增: 注册全表 list cache key ---
	if params.Where == "" && strings.HasPrefix(cacheKey, "list:") {
		if _, ok := idx.TableToFullTableListKeys[tableName]; !ok {
			idx.TableToFullTableListKeys[tableName] = make(map[string]bool)
		}
		idx.TableToFullTableListKeys[tableName][cacheKey] = true
	}

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

// toIndexValueString converts an index value to string for use as a map key
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

// extractAllWhereFields extracts all field names from the WHERE clause
func extractAllWhereFields(params types.QueryParams) []string {
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

// GetQueryParamsForKey returns the QueryParams associated with a given cache key.
// It returns the QueryParams and true if the key was found, otherwise zero QueryParams and false.
// It is safe for concurrent use.
func (idx *CacheIndex) GetQueryParamsForKey(cacheKey string) (types.QueryParams, bool) {
	if cacheKey == "" {
		return types.QueryParams{}, false
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
// Only supports simple conditions connected by AND.
func ParseExactMatchFields(params types.QueryParams) map[string][]interface{} {
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

// GetFullTableListKeys returns all list cache keys for the table with empty where clause (i.e., full table cache)
func (idx *CacheIndex) GetFullTableListKeys(tableName string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var keys []string
	if m, ok := idx.TableToFullTableListKeys[tableName]; ok {
		for k := range m {
			keys = append(keys, k)
		}
	}
	return keys
}
