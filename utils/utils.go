package utils

import (
	"sort"
)

func NormalizeValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case map[string]interface{}:
		// Sort map keys to ensure consistent ordering
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		result := make(map[string]interface{})
		for _, k := range keys {
			result[k] = NormalizeValue(v[k])
		}
		return result
	case []interface{}:
		// Make a copy to avoid modifying the original
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = NormalizeValue(item)
		}
		return result
	default:
		return v
	}
}
