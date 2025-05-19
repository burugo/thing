package utils

import (
	"encoding/gob"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
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

// IsZero checks if a reflect.Value is the zero value for its type.
// This handles various kinds including structs, slices, maps, pointers, and primitives.
func IsZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Invalid:
		return true
	case reflect.Array, reflect.String:
		return v.Len() == 0
	case reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.Interface:
		return v.IsNil()
	case reflect.Ptr:
		return v.IsNil() || IsZero(v.Elem()) // Recursively check pointed-to value
	case reflect.Struct:
		// Check if all fields are zero
		for i := 0; i < v.NumField(); i++ {
			if !IsZero(v.Field(i)) {
				return false
			}
		}
		return true
	default:
		// Compare with the zero value of the type
		zero := reflect.Zero(v.Type())
		return reflect.DeepEqual(v.Interface(), zero.Interface())
	}
}

// ToPtr returns a pointer to the given value (if not already a pointer)
func ToPtr[T any](v T) *T {
	if ptr, ok := any(v).(*T); ok {
		return ptr
	}
	return &v
}

// ToSnakeCase converts a string from CamelCase or PascalCase to snake_case.
func ToSnakeCase(str string) string {
	matchFirstCap := regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllCap := regexp.MustCompile("([a-z0-9])([A-Z])")

	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

// Use fully qualified type name as key: pkgPath + "." + typeName
var registeredTypeNames sync.Map // map[string]struct{}

// RegisterTypeRecursive registers the given type and all its fields recursively with gob.Register.
// It avoids duplicate registration by using the fully qualified type name as key.
// It also recovers from gob.Register panic to prevent test crash.
func RegisterTypeRecursive(t reflect.Type) {
	if t == nil {
		return
	}
	// Dereference pointer
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return // Only register struct types
	}
	typeKey := t.PkgPath() + "." + t.Name()
	if typeKey == "." { // anonymous or unnamed type
		return
	}
	// Avoid duplicate registration
	if _, loaded := registeredTypeNames.LoadOrStore(typeKey, struct{}{}); loaded {
		return
	}
	// Register struct and pointer to struct, recover panic if duplicate
	safeGobRegister := func(val interface{}) {
		defer func() {
			if r := recover(); r != nil {
				// Ignore duplicate registration panic
			}
		}()
		gob.Register(val)
	}
	safeGobRegister(reflect.New(t).Elem().Interface())
	safeGobRegister(reflect.New(t).Interface())
	// Register fields recursively
	n := t.NumField()
	for i := 0; i < n; i++ {
		f := t.Field(i)
		ft := f.Type
		// Dereference pointer
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		// Handle slice/array
		if ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			et := ft.Elem()
			RegisterTypeRecursive(et)
			continue
		}
		// Handle map
		if ft.Kind() == reflect.Map {
			RegisterTypeRecursive(ft.Key())
			RegisterTypeRecursive(ft.Elem())
			continue
		}
		// Handle struct
		if ft.Kind() == reflect.Struct && ft.PkgPath() != "" && ft.Name() != "Time" {
			RegisterTypeRecursive(ft)
		}
	}
}
