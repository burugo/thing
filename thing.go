// getValues extracts values from a model instance corresponding to columns.
func getValues(modelPtr interface{}, columns []string) ([]interface{}, error) {
	val := reflect.ValueOf(modelPtr)
	if val.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("getValues requires a pointer to a struct, got %T", modelPtr)
	}
	val = val.Elem()
	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("getValues requires a pointer to a struct, got pointer to %s", val.Kind())
	}

	// Map column names to their extracted values
	currentValuesMap := make(map[string]interface{})
	var mapValues func(v reflect.Value)
	mapValues = func(v reflect.Value) {
		if v.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			fieldVal := v.Field(i)
			if field.Name == "BaseModel" && field.Type == reflect.TypeOf(BaseModel{}) {
				mapValues(fieldVal) // Recurse
				continue
			}
			dbTag := field.Tag.Get("db")
			if dbTag == "-" {
				continue
			}
			colName := dbTag
			if colName == "" {
				colName = field.Name /* TODO: Snake case? */
			}
			// Check if this column is needed (optimization, avoids storing all fields)
			needed := false
			for _, requestedCol := range columns {
				if requestedCol == colName {
					needed = true
					break
				}
			}
			if needed {
				currentValuesMap[colName] = fieldVal.Interface()
			}
		}
	}
	mapValues(val)

	// Populate orderedValues based on the columns slice order
	orderedValues := make([]interface{}, len(columns))
	for i, colName := range columns {
		if val, ok := currentValuesMap[colName]; ok {
			orderedValues[i] = val
		} else {
			// This shouldn't happen if getColumns and getValues logic are consistent
			return nil, fmt.Errorf("value for column '%s' not found in model %T during getValues mapping", colName, modelPtr)
		}
	}

	return orderedValues, nil
}

// buildInsertSQL generates a standard INSERT statement.
func buildInsertSQL(tableName string, modelType reflect.Type, pkName string) (string, []string) {
	cols := getColumns(modelType, false, pkName) // Exclude PK for INSERT
	placeholders := make([]string, len(cols))
	for i := range cols {
		placeholders[i] = "?" // Assuming ? placeholders, adapter might need to adjust
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "))
	return query, cols
}

// buildSelectSQL generates a standard SELECT statement.
func buildSelectSQL(tableName string, modelType reflect.Type, pkName string) string {
	cols := getColumns(modelType, true, pkName) // Include PK for SELECT
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", strings.Join(cols, ", "), tableName, pkName)
}

// Create Implementation
func Create[T interface{ Model }](ctx context.Context, model *T) error {
	// ... (Refactor this function next) ...
	return nil
}

// ... rest of file ... 