package utils

import (
	"reflect"
	"sort"
)

// NormalizeValue recursively normalizes a value for consistent JSON marshaling.
// This is necessary to ensure a consistent cache key regardless of how the
// value was constructed.
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

// NewPtr returns a non-nil, zero-initialized value of type T (where T is a pointer type).
func NewPtr[T any]() T {
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	if modelType.Kind() == reflect.Ptr {
		return reflect.New(modelType.Elem()).Interface().(T)
	}
	return reflect.New(modelType).Interface().(T)
}
