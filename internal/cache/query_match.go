package cache

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	log "github.com/burugo/thing/internal/logging"
	"github.com/burugo/thing/internal/types"
	// Import regexp for potential future use or complex LIKE
	// "thing" // REMOVED import of root package
)

// isArgNil checks if an interface value represents nil.
// It handles untyped nil and typed nil pointers/interfaces/maps/slices/chans/funcs.
func isArgNil(argValue interface{}) bool {
	if argValue == nil {
		return true // Untyped nil
	}
	val := reflect.ValueOf(argValue)
	kind := val.Kind()
	// Check if it's a type that *can* be nil
	isNilable := kind == reflect.Pointer || kind == reflect.Interface || kind == reflect.Map || kind == reflect.Slice || kind == reflect.Chan || kind == reflect.Func
	// If it's a nilable type, check if it *is* nil (this IsNil call is safe due to the kind check)
	if isNilable {
		return val.IsNil() // Only call IsNil on nilable types
	}
	return false // Non-nilable types (int, string, etc.) can never be nil
}

// CheckQueryMatch evaluates if a given model instance satisfies the WHERE clause
// defined in the provided QueryParams.
// It now takes the model as interface{} and requires ModelInfo to be passed in.
//
// Supports =, LIKE, >, <, >=, <=, IN, !=, <>, NOT LIKE, NOT IN operators
// joined by AND/OR with parentheses.
// Support for complex LIKE patterns is NOT yet implemented.
func CheckQueryMatch(model interface{}, tableName string, columnToFieldMap map[string]string, params types.QueryParams) (bool, error) {
	return CheckQueryMatchWithPredicate(model, tableName, columnToFieldMap, params, nil)
}

// CheckQueryMatchWithPredicate is like CheckQueryMatch but accepts an optional pre-parsed predicate
// to avoid re-parsing the WHERE clause on every call. If pred is nil, it falls back to parsing.
func CheckQueryMatchWithPredicate(model interface{}, tableName string, columnToFieldMap map[string]string, params types.QueryParams, pred *queryPredicate) (bool, error) {
	if model == nil {
		return false, errors.New("model cannot be nil")
	}
	if tableName == "" {
		return false, errors.New("tableName cannot be empty")
	}
	if columnToFieldMap == nil {
		return false, errors.New("columnToFieldMap cannot be nil")
	}

	modelVal := reflect.ValueOf(model)
	if modelVal.Kind() != reflect.Pointer || modelVal.IsNil() {
		return false, errors.New("model must be a non-nil pointer")
	}

	predicate := pred
	if predicate == nil {
		var err error
		predicate, err = parseQueryPredicate(params)
		if err != nil {
			log.Printf("DEBUG: CheckQueryMatch could not parse WHERE clause '%s': %v", params.Where, err)
			return false, err
		}
	}
	return predicate.Match(modelVal.Elem(), tableName, columnToFieldMap)
}

func matchPredicateCondition(modelVal reflect.Value, tableName string, columnToFieldMap map[string]string, colName string, operator string, argValue interface{}) (bool, error) {
	goFieldName, ok := columnToFieldMap[colName]
	if !ok {
		if _, directFieldOK := modelVal.Type().FieldByName(colName); directFieldOK {
			goFieldName = colName
		} else {
			return false, fmt.Errorf("column '%s' from WHERE clause not found in map for table '%s'", colName, tableName)
		}
	}

	fieldVal := modelVal.FieldByName(goFieldName)
	if !fieldVal.IsValid() {
		return false, fmt.Errorf("field '%s' (for column '%s') not valid on model for table '%s'", goFieldName, colName, tableName)
	}

	modelFieldValue := fieldVal.Interface()
	argReflectVal := reflect.ValueOf(argValue)
	var conditionMet bool
	var matchErr error

	switch operator {
	case "=":
		if isArgNil(argValue) {
			conditionMet = isFieldNil(fieldVal)
		} else {
			conditionMet = fieldEqualsArg(fieldVal, argValue)
		}
	case "!=", "<>":
		if isArgNil(argValue) {
			conditionMet = !isFieldNil(fieldVal)
		} else {
			conditionMet = !fieldEqualsArg(fieldVal, argValue)
		}
	case "LIKE":
		modelStr, modelOk, modelNil := stringValueForLike(modelFieldValue)
		patternStr, patternOk, patternNil := stringValueForLike(argValue)
		switch {
		case !modelOk || !patternOk:
			matchErr = fmt.Errorf("LIKE operator requires string field and pattern, got %T and %T for field '%s'", modelFieldValue, argValue, goFieldName)
		case modelNil || patternNil:
			conditionMet = false
		default:
			conditionMet, matchErr = matchLike(modelStr, patternStr)
			if matchErr != nil {
				matchErr = fmt.Errorf("error matching LIKE for field '%s': %w", goFieldName, matchErr)
			}
		}
	case "NOT LIKE":
		modelStr, modelOk, modelNil := stringValueForLike(modelFieldValue)
		patternStr, patternOk, patternNil := stringValueForLike(argValue)
		switch {
		case !modelOk || !patternOk:
			matchErr = fmt.Errorf("NOT LIKE operator requires string field and pattern, got %T and %T for field '%s'", modelFieldValue, argValue, goFieldName)
		case modelNil || patternNil:
			conditionMet = false
		default:
			var likeMet bool
			likeMet, matchErr = matchLike(modelStr, patternStr)
			conditionMet = !likeMet
			if matchErr != nil {
				matchErr = fmt.Errorf("error matching NOT LIKE for field '%s': %w", goFieldName, matchErr)
			}
		}
	case ">", "<", ">=", "<=":
		conditionMet, matchErr = compareValues(modelFieldValue, argValue, operator)
	case "IN":
		conditionMet, matchErr = checkInOperator(fieldVal, argReflectVal)
	case "NOT IN":
		var inMet bool
		inMet, matchErr = checkInOperator(fieldVal, argReflectVal)
		conditionMet = !inMet
	default:
		matchErr = fmt.Errorf("unsupported operator '%s'", operator)
	}

	if matchErr != nil {
		log.Printf("DEBUG: Error during CheckQueryMatch condition evaluation: %v", matchErr)
		return false, matchErr
	}
	return conditionMet, nil
}

func stringValueForLike(value interface{}) (string, bool, bool) {
	if value == nil {
		return "", false, true
	}
	val := reflect.ValueOf(value)
	if !val.IsValid() {
		return "", false, true
	}
	if val.Kind() == reflect.Pointer {
		if val.Type().Elem().Kind() != reflect.String {
			return "", false, false
		}
		if val.IsNil() {
			return "", true, true
		}
		return val.Elem().String(), true, false
	}
	str, ok := value.(string)
	return str, ok, false
}

func isFieldNil(fieldVal reflect.Value) bool {
	if !fieldVal.IsValid() {
		return true
	}
	switch fieldVal.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return fieldVal.IsNil()
	default:
		return false
	}
}

func fieldEqualsArg(fieldVal reflect.Value, argValue interface{}) bool {
	if !fieldVal.IsValid() {
		return false
	}
	if isFieldNil(fieldVal) {
		return false
	}

	actualValue := fieldVal.Interface()
	argReflectVal := reflect.ValueOf(argValue)
	if fieldVal.Kind() == reflect.Pointer && argReflectVal.Kind() != reflect.Pointer {
		actualValue = fieldVal.Elem().Interface()
	}

	actualReflectVal := reflect.ValueOf(actualValue)
	if !actualReflectVal.IsValid() || !argReflectVal.IsValid() {
		return reflect.DeepEqual(actualValue, argValue)
	}
	if argReflectVal.Type().ConvertibleTo(actualReflectVal.Type()) {
		convertedArg := argReflectVal.Convert(actualReflectVal.Type()).Interface()
		return reflect.DeepEqual(actualValue, convertedArg)
	}
	return reflect.DeepEqual(actualValue, argValue)
}

// compareValues compares two values using >, <, >=, <= operators, handling numeric and string types.
func compareValues(modelVal, argVal interface{}, operator string) (bool, error) {
	// Dereference pointers to get actual values
	mVal := reflect.ValueOf(modelVal)
	if mVal.Kind() == reflect.Pointer {
		if mVal.IsNil() {
			return false, errors.New("cannot compare a nil model value")
		}
		mVal = mVal.Elem()
	}

	aVal := reflect.ValueOf(argVal)
	if aVal.Kind() == reflect.Pointer {
		if aVal.IsNil() {
			return false, errors.New("cannot compare against a nil argument value")
		}
		aVal = aVal.Elem()
	}

	// Handle time.Time comparison BEFORE other types
	modelTime, modelIsTime := mVal.Interface().(time.Time)
	argTime, argIsTime := aVal.Interface().(time.Time)

	if modelIsTime && argIsTime {
		switch operator {
		case ">":
			return modelTime.After(argTime), nil
		case "<":
			return modelTime.Before(argTime), nil
		case ">=":
			return modelTime.After(argTime) || modelTime.Equal(argTime), nil
		case "<=":
			return modelTime.Before(argTime) || modelTime.Equal(argTime), nil
		default:
			return false, fmt.Errorf("unsupported operator %s for time.Time comparison", operator)
		}
	}

	// Existing numeric and string comparison logic
	switch mVal.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		aInt, ok := convertToInt64(aVal)
		if !ok {
			return false, fmt.Errorf("cannot compare %s: model is int type (%T), but arg is incompatible type (%T)", operator, modelVal, argVal)
		}
		mInt := mVal.Int()
		switch operator {
		case ">":
			return mInt > aInt, nil
		case "<":
			return mInt < aInt, nil
		case ">=":
			return mInt >= aInt, nil
		case "<=":
			return mInt <= aInt, nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		aUint, ok := convertToUint64(aVal)
		if !ok {
			return false, fmt.Errorf("cannot compare %s: model is uint type (%T), but arg is incompatible type (%T)", operator, modelVal, argVal)
		}
		mUint := mVal.Uint()
		switch operator {
		case ">":
			return mUint > aUint, nil
		case "<":
			return mUint < aUint, nil
		case ">=":
			return mUint >= aUint, nil
		case "<=":
			return mUint <= aUint, nil
		}
	case reflect.Float32, reflect.Float64:
		aFloat, ok := convertToFloat64(aVal)
		if !ok {
			return false, fmt.Errorf("cannot compare %s: model is float type (%T), but arg is incompatible type (%T)", operator, modelVal, argVal)
		}
		mFloat := mVal.Float()
		switch operator {
		case ">":
			return mFloat > aFloat, nil
		case "<":
			return mFloat < aFloat, nil
		case ">=":
			return mFloat >= aFloat, nil
		case "<=":
			return mFloat <= aFloat, nil
		}
	case reflect.String:
		if aVal.Kind() != reflect.String {
			return false, fmt.Errorf("cannot compare %s: model is string, but arg is not (%T)", operator, argVal)
		}
		mStr := mVal.String()
		aStr := aVal.String()
		switch operator {
		case ">":
			return mStr > aStr, nil
		case "<":
			return mStr < aStr, nil
		case ">=":
			return mStr >= aStr, nil
		case "<=":
			return mStr <= aStr, nil
		}
	default:
		return false, fmt.Errorf("unsupported type %T for comparison operators (> < >= <=)", modelVal)
	}
	return false, fmt.Errorf("internal error: unexpected fallthrough in compareValues for operator %s", operator)
}

// checkInOperator checks if a model field value exists within a slice/array argument.
func checkInOperator(modelField reflect.Value, argSlice reflect.Value) (bool, error) {
	if argSlice.Kind() != reflect.Slice && argSlice.Kind() != reflect.Array {
		return false, fmt.Errorf("IN operator requires a slice or array argument, got %s", argSlice.Kind())
	}

	if argSlice.Len() == 0 {
		return false, nil // Value cannot be in an empty slice
	}

	modelFieldVal := modelField.Interface()
	// Get the type of elements in the slice argument
	sliceElemType := argSlice.Type().Elem()

	// Check if model field type is directly comparable or convertible to slice element type
	modelFieldType := modelField.Type()
	canCompareDirectly := modelFieldType.Comparable() && sliceElemType.Comparable() && modelFieldType == sliceElemType
	canConvert := modelFieldType.ConvertibleTo(sliceElemType)

	if !canCompareDirectly && !canConvert {
		// Return a specific error for incompatible types
		return false, fmt.Errorf("incompatible types for IN operator: model field type %s cannot be compared with slice element type %s", modelFieldType, sliceElemType)
	}

	// Track conversion errors
	var conversionErrors []string

	// If types are compatible or convertible, proceed with comparison
	for i := 0; i < argSlice.Len(); i++ {
		sliceElem := argSlice.Index(i)
		sliceElemVal := sliceElem.Interface()
		if reflect.DeepEqual(modelFieldVal, sliceElemVal) {
			return true, nil // Found a match directly
		}

		// If direct comparison failed but types are convertible, try conversion
		if canConvert && modelFieldType != sliceElemType {
			success, err := func() (success bool, err error) {
				// Use defer/recover to catch potential conversion panics
				defer func() {
					if r := recover(); r != nil {
						errMsg := fmt.Sprintf("Type conversion failed in IN operator: %v", r)
						log.Printf("DEBUG: %s", errMsg)
						err = errors.New(errMsg)
						success = false
					}
				}()

				// Try to convert slice element to model field type for comparison
				convertedElem := sliceElem.Convert(modelFieldType)
				convertedVal := convertedElem.Interface()

				// Compare the values after conversion
				if reflect.DeepEqual(modelFieldVal, convertedVal) {
					success = true
				}
				return
			}()

			if success {
				return true, nil
			} else if err != nil {
				conversionErrors = append(conversionErrors, err.Error())
			}
		}
	}

	// If we had conversion errors, return an error
	if len(conversionErrors) > 0 {
		errorMsg := "incompatible types for IN operator: " + strings.Join(conversionErrors, "; ")
		return false, errors.New(errorMsg)
	}

	return false, nil // No match found
}

// conversion helpers for compareValues
func convertToInt64(v reflect.Value) (int64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := v.Uint()
		if u > uint64(math.MaxInt64) {
			// Overflow, return max int64
			return math.MaxInt64, false
		}
		return int64(u), true
	case reflect.Float32, reflect.Float64:
		return int64(v.Float()), true // Truncates
	default:
		return 0, false
	}
}

func convertToUint64(v reflect.Value) (uint64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val := v.Int()
		if val < 0 {
			return 0, false
		} // Cannot convert negative int to uint
		return uint64(val), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), true
	case reflect.Float32, reflect.Float64:
		val := v.Float()
		if val < 0 {
			return 0, false
		} // Cannot convert negative float to uint
		return uint64(val), true // Truncates
	default:
		return 0, false
	}
}

func convertToFloat64(v reflect.Value) (float64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(v.Uint()), true
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	default:
		return 0, false
	}
}

// matchLike checks if a string 's' matches a SQL LIKE pattern.
// Handles '%' wildcard at the beginning, end, or both.
// Does NOT currently handle the '_' wildcard or escape characters.
func matchLike(s, pattern string) (bool, error) {
	if strings.Contains(pattern, "_") {
		return false, errors.New("LIKE pattern with '_' wildcard not supported")
	}
	// TODO: Add handling for escape characters if needed

	pattern = strings.TrimSpace(pattern) // Basic cleanup

	switch {
	case strings.HasPrefix(pattern, "%") && strings.HasSuffix(pattern, "%"):
		// Case: %value%
		searchTerm := strings.Trim(pattern, "%")
		return strings.Contains(s, searchTerm), nil
	case strings.HasPrefix(pattern, "%"):
		// Case: %value
		searchTerm := strings.TrimPrefix(pattern, "%")
		return strings.HasSuffix(s, searchTerm), nil
	case strings.HasSuffix(pattern, "%"):
		// Case: value%
		searchTerm := strings.TrimSuffix(pattern, "%")
		return strings.HasPrefix(s, searchTerm), nil
	default:
		// Case: value (no wildcards) - Treat as exact match
		return s == pattern, nil
	}
}

// TODO: Implement a more robust WHERE clause parser if complex conditions are required.
// TODO: Enhance matchLike to support '_' wildcard and escape characters.
