package thing

import (
	"errors"
	"log"
	"os"
	"reflect"
	"time"
)

// --- Change Detection ---

// findChangedFields acts as a dispatcher for change detection.
// It prefers the optimized simple comparison if metadata is available,
// otherwise falls back to the reflection-based method.
func findChangedFields[T any](original, updated *T) (map[string]interface{}, error) {
	// Get type information
	t := reflect.TypeOf(original).Elem()
	typename := t.Name()

	// Try to retrieve cached model info for this type
	info, _ := GetCachedModelInfo(t)

	// If we have cached model info with comparison metadata, use the optimized simple path
	if info != nil && len(info.CompareFields) > 0 {
		startTime := time.Now()
		result, err := findChangedFieldsSimple(original, updated, info)
		if err == nil {
			if os.Getenv("DEBUG_DIFF") != "" {
				log.Printf("DEBUG: [%s] Simple comparison took %v for %d fields",
					typename, time.Since(startTime), len(info.CompareFields))
			}
			return result, nil
		}
		// If simple comparison fails, fall back to reflection
		log.Printf("WARNING: Simple comparison failed for %s: %v. Falling back to reflection.",
			typename, err)
	}

	// Fall back to reflection-based approach if no cached info or simple comparison failed
	startTime := time.Now()
	result, err := findChangedFieldsReflection(original, updated, info)
	if os.Getenv("DEBUG_DIFF") != "" {
		log.Printf("DEBUG: [%s] Reflection comparison took %v", typename, time.Since(startTime))
	}
	return result, err
}

// findChangedFieldsReflection compares two structs (original and updated)
// and returns a map of column names to the changed values from the updated struct.
// DEPRECATED in favor of findChangedFieldsSimple.
func findChangedFieldsReflection[T any](original, updated *T, info *ModelInfo) (map[string]interface{}, error) {
	changed := make(map[string]interface{})
	originalVal := reflect.ValueOf(original).Elem()
	updatedVal := reflect.ValueOf(updated).Elem()
	typeName := originalVal.Type().Name()

	if originalVal.Type() != updatedVal.Type() {
		return nil, errors.New("original and updated values must be of the same type")
	}

	var processFields func(oVal, uVal reflect.Value, path string)
	processFields = func(oVal, uVal reflect.Value, path string) {
		structType := oVal.Type()
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)
			fieldName := field.Name
			currentPath := path + "." + fieldName

			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				oField := oVal.Field(i)
				uField := uVal.Field(i)
				if oField.IsValid() && uField.IsValid() {
					log.Printf("DEBUG: [%s%s] Recursing into embedded struct", typeName, currentPath)
					processFields(oField, uField, currentPath)
				} else {
					log.Printf("DEBUG: [%s%s] Skipping invalid embedded struct", typeName, currentPath)
				}
				continue
			}

			if !field.IsExported() {
				log.Printf("DEBUG: [%s%s] Skipping unexported field", typeName, currentPath)
				continue
			}

			dbColName, exists := info.FieldToColumnMap[fieldName]
			if !exists || dbColName == "-" || dbColName == info.PkName {
				log.Printf("DEBUG: [%s%s] Skipping field (no db tag, ignored, or PK). Exists: %v, DBCol: %s, PK: %s", typeName, currentPath, exists, dbColName, info.PkName)
				continue
			}

			originalFieldVal := oVal.FieldByName(fieldName)
			updatedFieldVal := uVal.FieldByName(fieldName)

			if !originalFieldVal.IsValid() || !updatedFieldVal.IsValid() {
				log.Printf("Warning: Field %s%s not valid during change detection", typeName, currentPath)
				continue
			}

			originalInterface := originalFieldVal.Interface()
			updatedInterface := updatedFieldVal.Interface()

			areEqual := reflect.DeepEqual(originalInterface, updatedInterface)
			log.Printf("DEBUG: [%s%s] Comparing DB:'%s' Original: [%v] (%T), Updated: [%v] (%T), Equal: %v",
				typeName, currentPath, dbColName, originalInterface, originalInterface, updatedInterface, updatedInterface, areEqual)

			if !areEqual {
				log.Printf("DEBUG: [%s%s] Change DETECTED for DB column '%s'", typeName, currentPath, dbColName)
				changed[dbColName] = updatedInterface
			}
		}
	}

	log.Printf("DEBUG: Starting change detection for type %s", typeName)
	processFields(originalVal, updatedVal, "")
	log.Printf("DEBUG: Raw changed map for %s: %v", typeName, changed)

	// --- Handle UpdatedAt specifically ---
	var updatedAtDBColName string
	updatedAtGoFieldName := "UpdatedAt"
	if info != nil { // Ensure info is not nil before accessing
		if col, ok := info.FieldToColumnMap[updatedAtGoFieldName]; ok {
			updatedAtDBColName = col
		}
	}

	if updatedAtDBColName != "" && len(changed) == 1 {
		if _, isOnlyChange := changed[updatedAtDBColName]; isOnlyChange {
			log.Printf("DEBUG: Removing '%s' from changed map as it's the only change.", updatedAtDBColName)
			delete(changed, updatedAtDBColName)
		}
	}
	log.Printf("DEBUG: Final changed map for %s after UpdatedAt check: %v", typeName, changed)

	return changed, nil
}

// findChangedFieldsSimple compares two structs (original and updated) using cached metadata
// for optimized comparison.
func findChangedFieldsSimple[T any](original, updated T, info *ModelInfo) (map[string]interface{}, error) {
	changed := make(map[string]interface{})

	// Robust nil pointer checks
	origVal := reflect.ValueOf(original)
	updVal := reflect.ValueOf(updated)
	if origVal.Kind() == reflect.Ptr && origVal.IsNil() {
		return changed, errors.New("findChangedFieldsSimple: original is nil pointer")
	}
	if updVal.Kind() == reflect.Ptr && updVal.IsNil() {
		return changed, errors.New("findChangedFieldsSimple: updated is nil pointer")
	}

	originalVal := origVal.Elem()
	updatedVal := updVal.Elem()

	if originalVal.Type() != updatedVal.Type() {
		return nil, errors.New("original and updated values must be of the same type")
	}

	// Get the UpdatedAt field name for special handling later
	var updatedAtDBColName string
	updatedAtGoFieldName := "UpdatedAt"
	if col, ok := info.FieldToColumnMap[updatedAtGoFieldName]; ok {
		updatedAtDBColName = col
	}

	for _, fieldInfo := range info.CompareFields {
		// Skip ignored fields, the PK field, and UpdatedAt (handled separately at the end)
		if fieldInfo.IgnoreInDiff || fieldInfo.DBColumn == info.PkName || fieldInfo.DBColumn == updatedAtDBColName {
			continue
		}

		oField := originalVal.FieldByIndex(fieldInfo.Index)
		uField := updatedVal.FieldByIndex(fieldInfo.Index)

		if !oField.IsValid() || !uField.IsValid() {
			log.Printf("Warning: Field %s (DB: %s) not valid during simple change detection", fieldInfo.GoName, fieldInfo.DBColumn)
			continue
		}

		// Optimized comparison based on kind or IsZero function
		var areEqual bool
		if fieldInfo.IsZero != nil {
			// For basic types or types with custom zero checkers, direct comparison might suffice if values are simple
			// However, DeepEqual is generally safer, especially for structs, slices, etc.
			// Let's stick to DeepEqual for reliability unless performance profiling shows a bottleneck here.
			areEqual = reflect.DeepEqual(oField.Interface(), uField.Interface())
		} else {
			// Fallback to DeepEqual if no IsZero function provided (shouldn't happen with current GetCachedModelInfo)
			areEqual = reflect.DeepEqual(oField.Interface(), uField.Interface())
		}

		if !areEqual {
			changed[fieldInfo.DBColumn] = uField.Interface()
		}
	}

	// Exclude UpdatedAt only if it's the *sole* change
	if updatedAtDBColName != "" && len(changed) == 0 {
		// If changed map is empty *before* checking UpdatedAt, check UpdatedAt now.
		for _, fieldInfo := range info.CompareFields {
			if fieldInfo.DBColumn == updatedAtDBColName {
				oField := originalVal.FieldByIndex(fieldInfo.Index)
				uField := updatedVal.FieldByIndex(fieldInfo.Index)
				if oField.IsValid() && uField.IsValid() && !reflect.DeepEqual(oField.Interface(), uField.Interface()) {
					// UpdatedAt changed, but since it's the only potential change, we still return an empty map.
					// changed[fieldInfo.DBColumn] = uField.Interface() // Don't actually add it
					log.Printf("DEBUG: UpdatedAt (%s) is the only changed field, ignoring.", updatedAtDBColName)
				}
				break
			}
		}
	}

	return changed, nil
}
