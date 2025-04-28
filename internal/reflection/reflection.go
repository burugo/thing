package reflection

import (
	"reflect"
	"strings"
	"time"
	"unicode"
)

// GetTableNameFromType determines the table name from a reflect.Type.
func GetTableNameFromType(modelType reflect.Type) string {
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	// 1. First, check if the type or a pointer to the type has a TableName method
	// Create a zero value of the type to call methods on
	modelPtr := reflect.New(modelType)
	tableNameMethod := modelPtr.MethodByName("TableName")
	if tableNameMethod.IsValid() && tableNameMethod.Type().NumIn() == 0 && tableNameMethod.Type().NumOut() == 1 && tableNameMethod.Type().Out(0).Kind() == reflect.String {
		// Method found on pointer receiver
		result := tableNameMethod.Call(nil)[0].String()
		if result != "" {
			return result
		}
	}

	// 2. Check for struct field tag
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		if tableName, ok := field.Tag.Lookup("tableName"); ok && tableName != "" {
			return tableName
		}
	}

	// 3. Fall back to pluralized type name with underscore case
	typeName := modelType.Name()
	if typeName == "" {
		return "" // Anonymous struct, can't determine table name
	}

	// Convert to snake_case (e.g., UserPost => user_posts)
	var result strings.Builder
	for i, r := range typeName {
		if i > 0 && unicode.IsUpper(r) {
			result.WriteByte('_')
		}
		result.WriteRune(unicode.ToLower(r))
	}
	tableName := result.String()

	// Simple pluralization (just add "s", not handling irregular plurals)
	return tableName + "s"
}

// GetBaseModelPtr returns a pointer to the embedded BaseModel if it exists and is addressable.
func GetBaseModelPtr(value interface{}) interface{} {
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil // Not a struct or pointer to struct
	}

	// Look for field with specific name (BaseModel)
	baseField := val.FieldByName("BaseModel")
	if baseField.IsValid() && baseField.CanAddr() {
		return baseField.Addr().Interface()
	}

	return nil // BaseModel not found or not addressable
}

// SetNewRecordFlagIfBaseModel sets the isNewRecord flag if the value embeds BaseModel.
func SetNewRecordFlagIfBaseModel(value interface{}, isNew bool) {
	if bmPtr := GetBaseModelPtr(value); bmPtr != nil {
		// Look for SetNewRecordFlag method on the BaseModel
		baseModelValue := reflect.ValueOf(bmPtr)
		setFlagMethod := baseModelValue.MethodByName("SetNewRecordFlag")
		if setFlagMethod.IsValid() {
			setFlagMethod.Call([]reflect.Value{reflect.ValueOf(isNew)})
		}
	}
}

// SetCreatedAtTimestamp sets the CreatedAt field if it exists.
func SetCreatedAtTimestamp(value interface{}, t time.Time) {
	if bmPtr := GetBaseModelPtr(value); bmPtr != nil {
		baseModelValue := reflect.ValueOf(bmPtr).Elem()
		createdAtField := baseModelValue.FieldByName("CreatedAt")
		if createdAtField.IsValid() && createdAtField.CanSet() {
			createdAtField.Set(reflect.ValueOf(t))
			return
		}
	}

	// If BaseModel not found or CreatedAt not found in BaseModel,
	// try to find it directly in the struct
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return // Not a struct, can't set field
	}
	field := val.FieldByName("CreatedAt")
	if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
		field.Set(reflect.ValueOf(t))
	}
}

// SetUpdatedAtTimestamp sets the UpdatedAt field if it exists.
func SetUpdatedAtTimestamp(value interface{}, t time.Time) {
	if bmPtr := GetBaseModelPtr(value); bmPtr != nil {
		baseModelValue := reflect.ValueOf(bmPtr).Elem()
		updatedAtField := baseModelValue.FieldByName("UpdatedAt")
		if updatedAtField.IsValid() && updatedAtField.CanSet() {
			updatedAtField.Set(reflect.ValueOf(t))
			return
		}
	}

	// If BaseModel not found or UpdatedAt not found in BaseModel,
	// try to find it directly in the struct
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return // Not a struct, can't set field
	}
	field := val.FieldByName("UpdatedAt")
	if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
		field.Set(reflect.ValueOf(t))
	}
}
