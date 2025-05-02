package thing

import (
	"errors"
	"log"
	"reflect"
	"thing/internal/schema"
)

// findChangedFieldsSimple compares two structs (original and updated) using cached metadata
// for optimized comparison.
func findChangedFieldsSimple[T any](original, updated *T, info *schema.ModelInfo) (map[string]interface{}, error) {
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

	for _, fieldInfo := range info.CompareFields {
		// Skip ignored fields, the PK field
		if fieldInfo.IgnoreInDiff || fieldInfo.DBColumn == info.PkName {
			continue
		}

		oVal := originalVal
		uVal := updatedVal
		if oVal.Kind() == reflect.Ptr {
			if oVal.IsNil() {
				continue // 跳过 nil 指针字段
			}
			oVal = oVal.Elem()
		}
		if uVal.Kind() == reflect.Ptr {
			if uVal.IsNil() {
				continue // 跳过 nil 指针字段
			}
			uVal = uVal.Elem()
		}

		oField := oVal.FieldByIndex(fieldInfo.Index)
		uField := uVal.FieldByIndex(fieldInfo.Index)

		if !oField.IsValid() || !uField.IsValid() {
			log.Printf("Warning: Field %s (DB: %s) not valid during simple change detection", fieldInfo.GoName, fieldInfo.DBColumn)
			continue
		}

		// Optimized comparison based on kind or IsZero function
		var areEqual bool
		if fieldInfo.IsZero != nil {
			areEqual = reflect.DeepEqual(oField.Interface(), uField.Interface())
		} else {
			areEqual = reflect.DeepEqual(oField.Interface(), uField.Interface())
		}

		if !areEqual {
			changed[fieldInfo.DBColumn] = uField.Interface()
		}
	}

	return changed, nil
}
