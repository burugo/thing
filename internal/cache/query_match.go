package cache

import (
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	// Import regexp for potential future use or complex LIKE
	// "thing" // REMOVED import of root package
)

// CheckQueryMatch evaluates if a given model instance satisfies the WHERE clause
// defined in the provided QueryParams.
// It now takes the model as interface{} and requires ModelInfo to be passed in.
//
// Supports =, LIKE, >, <, >=, <=, IN operators joined by AND.
// Support for OR clauses or complex LIKE patterns is NOT yet implemented.
func CheckQueryMatch(model interface{}, info *ModelInfo, params QueryParams) (bool, error) {
	if model == nil {
		return false, errors.New("model cannot be nil")
	}
	if info == nil {
		return false, errors.New("model info cannot be nil")
	}

	whereClause := strings.TrimSpace(params.Where)
	args := params.Args

	// Handle the simple case of no WHERE clause
	if whereClause == "" {
		return true, nil // No conditions means it always matches
	}

	// Basic validation: Check if number of '?' matches number of args
	expectedArgs := strings.Count(whereClause, "?")
	if expectedArgs != len(args) {
		// Special case for IN: it uses one '?' but multiple implicit args in the slice
		isIN := false
		conditionsTemp := strings.Split(whereClause, " AND ")
		for _, cond := range conditionsTemp {
			if len(strings.Fields(cond)) == 3 && strings.ToUpper(strings.Fields(cond)[1]) == "IN" {
				isIN = true
				break
			}
		}
		// If it's not an IN query or if arg count still mismatch, error out
		if !isIN || expectedArgs != 1 || len(args) != 1 || reflect.ValueOf(args[0]).Kind() != reflect.Slice {
			return false, fmt.Errorf("mismatched number of placeholders ('?') and arguments: %d vs %d in WHERE clause '%s' (Note: IN expects one '?' and one slice argument)", expectedArgs, len(args), whereClause)
		}
		// If it looks like a valid IN, let the loop handle the single arg
	}

	// Very basic parser for "column OP ?" clauses joined by "AND"
	conditions := strings.Split(whereClause, " AND ")
	// We are deliberately *not* checking if len(conditions) == expectedArgs here,
	// because a single condition like "a = ? OR b = ?" might fail that check
	// but could still be parsed condition by condition if we enhance the loop.
	// However, for now, we only support AND-separated conditions.
	if strings.Contains(strings.ToUpper(whereClause), " OR ") {
		// Explicitly disallow OR for now
		return false, fmt.Errorf("unsupported WHERE clause structure: '%s'. OR clauses are not supported", whereClause)
	}

	modelVal := reflect.ValueOf(model).Elem()
	currentArgIndex := 0 // Keep track of which arg we're using

	for _, condition := range conditions {
		condition = strings.TrimSpace(condition)
		if condition == "" {
			continue
		}

		// Expect "column OP ?" format or "column IN (?)"
		parts := strings.Fields(condition) // Split by whitespace
		operator := ""
		if len(parts) >= 2 {
			operator = strings.ToUpper(parts[1])
		}

		// Validate format: either 3 parts ending in '?' or 3 parts with 'IN' and '(?)'
		validFormat := (len(parts) == 3 && parts[2] == "?") || (len(parts) == 3 && operator == "IN" && parts[2] == "(?)")

		if !validFormat {
			log.Printf("WARN: CheckQueryMatch could not parse condition: %s. Expected column OP ? or column IN (?)", condition)
			return false, fmt.Errorf("unsupported condition format: %s. Expected column OP ? or column IN (?)", condition)
		}

		colName := strings.Trim(parts[0], "\\\"`'") // Remove potential quotes
		argValue := args[currentArgIndex]
		currentArgIndex++

		// Find the Go field name corresponding to the DB column name
		goFieldName, ok := info.ColumnToFieldMap[colName]
		if !ok {
			// Maybe the WHERE clause uses the Go field name directly? Check that.
			if _, directFieldOK := modelVal.Type().FieldByName(colName); directFieldOK {
				goFieldName = colName
				ok = true
			} else {
				return false, fmt.Errorf("column '%s' from WHERE clause not found in model %s info", colName, modelVal.Type().Name())
			}
		}

		// Get the model's field value
		fieldVal := modelVal.FieldByName(goFieldName)
		if !fieldVal.IsValid() {
			return false, fmt.Errorf("field '%s' (for column '%s') not valid on model %s", goFieldName, colName, modelVal.Type().Name())
		}

		modelFieldValue := fieldVal.Interface()
		argReflectVal := reflect.ValueOf(argValue)

		// --- Perform comparison based on operator ---
		var conditionMet bool
		var matchErr error

		switch operator {
		case "=":
			// Basic type matching/conversion attempt for equality
			if argReflectVal.Type().ConvertibleTo(fieldVal.Type()) {
				convertedArg := argReflectVal.Convert(fieldVal.Type()).Interface()
				conditionMet = reflect.DeepEqual(modelFieldValue, convertedArg)
				if !conditionMet {
					log.Printf("DEBUG CheckQueryMatch (=): Field '%s' mismatch. Model: [%v] (%T), Arg: [%v] (%T, Converted: %T)",
						goFieldName, modelFieldValue, modelFieldValue, argValue, argValue, convertedArg)
				}
			} else {
				// If types are not directly convertible, rely on DeepEqual (might fail for some cases)
				conditionMet = reflect.DeepEqual(modelFieldValue, argValue)
				if !conditionMet {
					log.Printf("DEBUG CheckQueryMatch (=): Field '%s' mismatch (non-convertible types). Model: [%v] (%T), Arg: [%v] (%T)",
						goFieldName, modelFieldValue, modelFieldValue, argValue, argValue)
				}
			}

		case "LIKE":
			// Ensure both model field and arg are strings
			modelStr, modelOk := modelFieldValue.(string)
			patternStr, patternOk := argValue.(string)

			if !modelOk || !patternOk {
				matchErr = fmt.Errorf("LIKE operator requires string field and pattern, got %T and %T for field '%s'", modelFieldValue, argValue, goFieldName)
			} else {
				conditionMet, matchErr = matchLike(modelStr, patternStr)
				if matchErr != nil {
					matchErr = fmt.Errorf("error matching LIKE for field '%s': %w", goFieldName, matchErr)
				} else if !conditionMet {
					log.Printf("DEBUG CheckQueryMatch (LIKE): Field '%s' ('%s') did not match pattern '%s'", goFieldName, modelStr, patternStr)
				}
			}

		case ">":
			conditionMet, matchErr = compareValues(modelFieldValue, argValue, operator)
			if !conditionMet && matchErr == nil {
				log.Printf("DEBUG CheckQueryMatch (>): Field '%s' (%v) not greater than arg (%v)", goFieldName, modelFieldValue, argValue)
			}

		case "<":
			conditionMet, matchErr = compareValues(modelFieldValue, argValue, operator)
			if !conditionMet && matchErr == nil {
				log.Printf("DEBUG CheckQueryMatch (<): Field '%s' (%v) not less than arg (%v)", goFieldName, modelFieldValue, argValue)
			}

		case ">=":
			conditionMet, matchErr = compareValues(modelFieldValue, argValue, operator)
			if !conditionMet && matchErr == nil {
				log.Printf("DEBUG CheckQueryMatch (>=): Field '%s' (%v) not greater than or equal to arg (%v)", goFieldName, modelFieldValue, argValue)
			}

		case "<=":
			conditionMet, matchErr = compareValues(modelFieldValue, argValue, operator)
			if !conditionMet && matchErr == nil {
				log.Printf("DEBUG CheckQueryMatch (<=): Field '%s' (%v) not less than or equal to arg (%v)", goFieldName, modelFieldValue, argValue)
			}

		case "IN":
			conditionMet, matchErr = checkInOperator(fieldVal, argReflectVal)
			if !conditionMet && matchErr == nil {
				log.Printf("DEBUG CheckQueryMatch (IN): Field '%s' (%v) not in arg slice (%v)", goFieldName, modelFieldValue, argValue)
			}

		default:
			matchErr = fmt.Errorf("unsupported operator '%s' in condition '%s'", operator, condition)
		}

		// Handle errors during matching
		if matchErr != nil {
			log.Printf("WARN: Error during CheckQueryMatch condition evaluation: %v", matchErr)
			return false, matchErr // Propagate error
		}

		// If any condition is not met, the whole query doesn't match
		if !conditionMet {
			return false, nil // Condition not met, no need to check further
		}

		// If we reach here, this condition matched. Continue to the next.
		log.Printf("DEBUG CheckQueryMatch: Condition '%s' matched for field '%s'", condition, goFieldName)
	}

	// All conditions matched
	return true, nil
}

// compareValues compares two values using >, <, >=, <= operators, handling numeric and string types.
func compareValues(modelVal, argVal interface{}, operator string) (bool, error) {
	mVal := reflect.ValueOf(modelVal)
	aVal := reflect.ValueOf(argVal)

	// Handle basic types: ints, floats, strings
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
		return false, fmt.Errorf("IN operator requires a slice or array argument, got %T", argSlice.Interface())
	}

	if argSlice.Len() == 0 {
		return false, nil // SQL standard: field IN () is always false
	}

	modelVal := modelField.Interface()

	for i := 0; i < argSlice.Len(); i++ {
		sliceElement := argSlice.Index(i)
		sliceElementVal := sliceElement.Interface()

		// Attempt conversion if types are compatible
		if sliceElement.Type().ConvertibleTo(modelField.Type()) {
			convertedElement := sliceElement.Convert(modelField.Type()).Interface()
			if reflect.DeepEqual(modelVal, convertedElement) {
				return true, nil // Found a match
			}
		} else if reflect.DeepEqual(modelVal, sliceElementVal) {
			// Fallback to DeepEqual for non-convertible but potentially equal types
			return true, nil // Found a match
		}
	}

	return false, nil // No match found in the slice
}

// conversion helpers for compareValues
func convertToInt64(v reflect.Value) (int64, bool) {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(v.Uint()), true // Potential overflow ignored
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

	if strings.HasPrefix(pattern, "%") && strings.HasSuffix(pattern, "%") {
		// Case: %value%
		searchTerm := strings.Trim(pattern, "%")
		return strings.Contains(s, searchTerm), nil
	} else if strings.HasPrefix(pattern, "%") {
		// Case: %value
		searchTerm := strings.TrimPrefix(pattern, "%")
		return strings.HasSuffix(s, searchTerm), nil
	} else if strings.HasSuffix(pattern, "%") {
		// Case: value%
		searchTerm := strings.TrimSuffix(pattern, "%")
		return strings.HasPrefix(s, searchTerm), nil
	} else {
		// Case: value (no wildcards) - Treat as exact match
		return s == pattern, nil
	}
}

// TODO: Implement a more robust WHERE clause parser if complex conditions are required.
// TODO: Enhance matchLike to support '_' wildcard and escape characters.
