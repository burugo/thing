package thing

import (
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
)

// CheckQueryMatch evaluates if a given model instance satisfies the WHERE clause
// defined in the provided QueryParams.
//
// Current implementation only supports simple equality checks (e.g., "field = ?").
// Support for other operators (>, <, LIKE, IN, etc.) is NOT yet implemented.
func (t *Thing[T]) CheckQueryMatch(model *T, params QueryParams) (bool, error) {
	if model == nil {
		return false, errors.New("model cannot be nil")
	}
	if t.info == nil {
		return false, errors.New("Thing instance missing model info")
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
		return false, fmt.Errorf("mismatched number of placeholders ('?') and arguments: %d vs %d in WHERE clause '%s'", expectedArgs, len(args), whereClause)
	}

	// Very basic parser for "column = ?" clauses joined by "AND"
	conditions := strings.Split(whereClause, " AND ")
	if len(conditions) != expectedArgs {
		// If splitting by AND doesn't match the number of args/placeholders,
		// it's likely a more complex clause we don't support yet.
		log.Printf("WARN: CheckQueryMatch currently only supports simple 'column = ?' conditions joined by AND. Clause: '%s'", whereClause)
		// For now, we'll conservatively return false and an error indicating limited support.
		// A more robust implementation would involve a proper SQL parser.
		return false, fmt.Errorf("unsupported WHERE clause structure: '%s'. Only simple 'col = ?' AND 'col2 = ?' supported", whereClause)
	}

	modelVal := reflect.ValueOf(model).Elem()

	for i, condition := range conditions {
		parts := strings.SplitN(strings.TrimSpace(condition), "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[1]) != "?" {
			log.Printf("WARN: CheckQueryMatch encountered non-'=' condition part: '%s'", condition)
			return false, fmt.Errorf("unsupported condition structure in WHERE clause: '%s'. Only 'col = ?' supported", condition)
		}

		colName := strings.Trim(strings.TrimSpace(parts[0]), "`"'") // Remove potential quotes
		argValue := args[i]

		// Find the Go field name corresponding to the DB column name
		goFieldName, ok := t.info.columnToFieldMap[colName]
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

		// Compare model field value with the argument value
		// Use reflect.DeepEqual for a general comparison, might need refinement for specific types.
		// Normalize argValue before comparison if necessary (e.g., ensure types match fieldVal)
		modelFieldValue := fieldVal.Interface()

		// Basic type matching/conversion attempt
		argReflectVal := reflect.ValueOf(argValue)
		if argReflectVal.Type().ConvertibleTo(fieldVal.Type()) {
			convertedArg := argReflectVal.Convert(fieldVal.Type()).Interface()
			if !reflect.DeepEqual(modelFieldValue, convertedArg) {
				log.Printf("DEBUG CheckQueryMatch: Field '%s' mismatch. Model: [%v] (%T), Arg: [%v] (%T, Converted: %T)",
					goFieldName, modelFieldValue, modelFieldValue, argValue, argValue, convertedArg)
				return false, nil // Condition not met
			}
		} else {
			// If types are not directly convertible, rely on DeepEqual (might fail for some cases)
			if !reflect.DeepEqual(modelFieldValue, argValue) {
				log.Printf("DEBUG CheckQueryMatch: Field '%s' mismatch (non-convertible types). Model: [%v] (%T), Arg: [%v] (%T)",
					goFieldName, modelFieldValue, modelFieldValue, argValue, argValue)
				return false, nil // Condition not met
			}
		}

		// If we reach here, this condition matched. Continue to the next.
		log.Printf("DEBUG CheckQueryMatch: Condition '%s' matched for field '%s'", condition, goFieldName)
	}

	// All conditions matched
	return true, nil
}

// TODO: Extend CheckQueryMatch to support more operators (IN, !=, >, <, LIKE) if needed.
// TODO: Implement a more robust WHERE clause parser if complex conditions are required.
