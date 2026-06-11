package dbscan

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

var (
	timeType        = reflect.TypeOf(time.Time{})
	fieldIndexCache sync.Map // map[reflect.Type]map[string][]int
)

// PrepareScanDest creates sql.Rows.Scan destinations for a struct value.
// Field/tag analysis is cached by struct type; each row only resolves cached field indexes.
func PrepareScanDest(structVal reflect.Value, cols []string) ([]interface{}, error) {
	if structVal.Kind() != reflect.Struct {
		return nil, fmt.Errorf("PrepareScanDest: input must be a struct value, got %s", structVal.Kind())
	}

	fieldIndexes, err := getFieldIndexes(structVal.Type())
	if err != nil {
		return nil, err
	}

	dest := make([]interface{}, len(cols))
	for i, col := range cols {
		index, ok := fieldIndexes[col]
		if !ok {
			dest[i] = new(sql.RawBytes)
			continue
		}
		field := structVal.FieldByIndex(index)
		if !field.CanAddr() {
			return nil, fmt.Errorf("PrepareScanDest: cannot take address of field for column %s", col)
		}
		dest[i] = field.Addr().Interface()
	}
	return dest, nil
}

// IsBasicType reports whether a type can be scanned directly without struct field mapping.
func IsBasicType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.Uint8
	case reflect.Struct:
		return t == timeType
	case reflect.Ptr:
		return IsBasicType(t.Elem())
	default:
		return false
	}
}

func getFieldIndexes(typ reflect.Type) (map[string][]int, error) {
	if cached, ok := fieldIndexCache.Load(typ); ok {
		return cached.(map[string][]int), nil
	}

	indexes, err := buildFieldIndexes(typ)
	if err != nil {
		return nil, err
	}
	actual, _ := fieldIndexCache.LoadOrStore(typ, indexes)
	return actual.(map[string][]int), nil
}

func buildFieldIndexes(typ reflect.Type) (map[string][]int, error) {
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("buildFieldIndexes: input must be a struct type, got %s", typ.Kind())
	}

	fieldMap := make(map[string][]int)

	// First pass: embedded fields. Direct fields in the second pass intentionally override them.
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		if !isEmbeddedStruct(field) {
			continue
		}

		embeddedMap, err := buildFieldIndexes(field.Type)
		if err != nil {
			return nil, fmt.Errorf("error processing embedded struct field %s: %w", field.Name, err)
		}
		for column, index := range embeddedMap {
			fieldMap[column] = appendIndex(field.Index, index)
		}
	}

	// Second pass: direct fields, overriding embedded fields on name clashes.
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() || isEmbeddedStruct(field) {
			continue
		}
		dbTag := field.Tag.Get("db")
		if dbTag == "-" {
			continue
		}

		column := dbTag
		if column != "" {
			column = strings.Split(column, ",")[0]
		}
		if column == "" {
			column = strings.ToLower(field.Name)
		}

		fieldMap[column] = field.Index
	}

	return fieldMap, nil
}

func isEmbeddedStruct(field reflect.StructField) bool {
	return field.Anonymous && field.Type.Kind() == reflect.Struct && field.Type != timeType
}

func appendIndex(prefix, suffix []int) []int {
	out := make([]int, 0, len(prefix)+len(suffix))
	out = append(out, prefix...)
	out = append(out, suffix...)
	return out
}
