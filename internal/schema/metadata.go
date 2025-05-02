package schema

import (
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

// --- Model Metadata Cache ---

// ComparableFieldInfo holds pre-computed metadata for a single field used during comparisons.
type ComparableFieldInfo struct {
	GoName       string                     // Go field name
	DBColumn     string                     // Database column name
	Index        []int                      // Index for fast field access via FieldByIndex
	IsZero       func(v reflect.Value) bool // Function to check if value is zero/empty
	Kind         reflect.Kind               // Field kind (string, int, struct, etc.)
	Type         reflect.Type               // Field type (for more detailed type checking)
	IsEmbedded   bool                       // Whether this is from an embedded struct
	IgnoreInDiff bool                       // Whether to ignore this field during diffing (e.g., tags like db:"-")
}

// ModelInfo holds pre-computed metadata about a model type T.
type ModelInfo struct {
	TableName        string                // Renamed: tableName -> TableName
	PkName           string                // Database name of the primary key field (Exported)
	Columns          []string              // Renamed: columns -> Columns
	Fields           []string              // Corresponding Go struct field names (Exported)
	FieldToColumnMap map[string]string     // Map Go field name to its corresponding DB column name (Exported)
	ColumnToFieldMap map[string]string     // Map DB column name to its corresponding Go field name (Exported)
	CompareFields    []ComparableFieldInfo // Fields to compare during diff operations (new)
}

// modelCache stores ModelInfo structs, keyed by reflect.Type.
// Renamed: modelCache -> ModelCache
var ModelCache sync.Map // map[reflect.Type]*ModelInfo

// getCachedModelInfo retrieves or computes/caches metadata for a given Model type.
// Renamed: getCachedModelInfo -> GetCachedModelInfo (Exported)
// Returns *ModelInfo now
func GetCachedModelInfo(modelType reflect.Type) (*ModelInfo, error) {
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct type, got %s", modelType.Kind())
	}

	if cached, ok := ModelCache.Load(modelType); ok {
		return cached.(*ModelInfo), nil
	}

	info := ModelInfo{ // Using renamed struct
		FieldToColumnMap: make(map[string]string),
		ColumnToFieldMap: make(map[string]string),
		CompareFields:    make([]ComparableFieldInfo, 0),
	}
	pkDbName := ""

	numFields := modelType.NumField()
	info.Columns = make([]string, 0, numFields) // Using renamed field
	info.Fields = make([]string, 0, numFields)

	var processFields func(structType reflect.Type, parentIndex []int, isEmbedded bool)
	processFields = func(structType reflect.Type, parentIndex []int, isEmbedded bool) {
		for i := 0; i < structType.NumField(); i++ {
			field := structType.Field(i)
			dbTag := field.Tag.Get("db")
			diffTag := field.Tag.Get("diff")   // Optional diff tag
			thingTag := field.Tag.Get("thing") // Check for thing tag (used for relations)

			// Skip relationship fields (identified by having a thing tag with rel=...)
			// More robust check might parse the tag, but checking for non-empty is sufficient for now.
			if thingTag != "" {
				log.Printf("DEBUG GetCachedModelInfo: Skipping field %s with 'thing' tag", field.Name)
				continue
			}

			// If field is embedded struct, process its fields recursively
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				// For BaseModel, process fields directly since they're important
				if field.Type.Name() == "BaseModel" {
					baseModelType := field.Type
					for j := 0; j < baseModelType.NumField(); j++ {
						baseField := baseModelType.Field(j)
						baseDbTag := baseField.Tag.Get("db")
						baseDiffTag := baseField.Tag.Get("diff") // Optional diff tag for BaseModel fields

						if baseDbTag == "-" || !baseField.IsExported() {
							continue
						}

						// Correctly parse column name and pk tag
						columnName := baseDbTag
						isPk := false
						if strings.Contains(columnName, ",") {
							parts := strings.SplitN(columnName, ",", 2)
							columnName = parts[0]
							if parts[1] == "pk" || parts[1] == "primarykey" {
								isPk = true
							}
						}
						if columnName == "" {
							columnName = ToSnakeCase(baseField.Name) // Default to snake_case
						}

						// Store mapping for the BaseModel field
						info.Columns = append(info.Columns, columnName)
						info.Fields = append(info.Fields, baseField.Name)
						info.FieldToColumnMap[baseField.Name] = columnName
						info.ColumnToFieldMap[columnName] = baseField.Name

						// Store field info for comparison
						info.CompareFields = append(info.CompareFields, ComparableFieldInfo{
							GoName:       baseField.Name,
							DBColumn:     columnName,
							Index:        append(parentIndex, i, j), // Index into embedded struct + base model field
							IsZero:       getZeroChecker(baseField.Type),
							Kind:         baseField.Type.Kind(),
							Type:         baseField.Type,
							IsEmbedded:   true, // Mark as embedded field
							IgnoreInDiff: baseDiffTag == "-",
						})

						// Check for primary key tag in BaseModel
						if isPk {
							pkDbName = columnName
						}
					}
				} else {
					// Process other embedded structs recursively
					processFields(field.Type, append(parentIndex, i), true)
				}
				continue // Skip processing the embedded struct field itself
			}

			// Ignore unexported fields or fields tagged with `db:"-"`
			if dbTag == "-" || !field.IsExported() {
				continue
			}

			// Correctly parse column name and pk tag
			columnName := dbTag
			isPk := false
			if strings.Contains(columnName, ",") {
				parts := strings.SplitN(columnName, ",", 2)
				columnName = parts[0]
				if parts[1] == "pk" || parts[1] == "primarykey" {
					isPk = true
				}
			}
			if columnName == "" {
				columnName = ToSnakeCase(field.Name) // Default to snake_case
			}

			info.Columns = append(info.Columns, columnName)
			info.Fields = append(info.Fields, field.Name)
			info.FieldToColumnMap[field.Name] = columnName
			info.ColumnToFieldMap[columnName] = field.Name

			// Store field info for comparison
			info.CompareFields = append(info.CompareFields, ComparableFieldInfo{
				GoName:       field.Name,
				DBColumn:     columnName,
				Index:        append(parentIndex, i),
				IsZero:       getZeroChecker(field.Type),
				Kind:         field.Type.Kind(),
				Type:         field.Type,
				IsEmbedded:   isEmbedded,
				IgnoreInDiff: diffTag == "-",
			})

			// Check for primary key if not already found in embedded BaseModel
			if pkDbName == "" && isPk {
				pkDbName = columnName
			}
		}
	}

	processFields(modelType, []int{}, false)

	if pkDbName == "" {
		pkDbName = "id" // Default to "id" if no pk tag found
		log.Printf("Warning: No primary key tag found for type %s, defaulting to 'id'. Add `db:\",pk\"` tag to your primary key field.", modelType.Name())
		// Ensure 'id' is actually in the map, add it if necessary (should be covered by BaseModel usually)
		if _, ok := info.ColumnToFieldMap["id"]; !ok {
			log.Printf("ERROR: Default PK 'id' not found in mappings for type %s. Check struct definition.", modelType.Name())
			// Potentially return error here, but let's try to proceed for now
		}
	}
	info.PkName = pkDbName

	// Determine table name
	info.TableName = getTableNameFromType(modelType)

	// Store in cache
	ModelCache.Store(modelType, &info)

	log.Printf("INFO: Computed and cached model info for type: %s (Table: %s, PK: %s)", modelType.Name(), info.TableName, info.PkName)
	return &info, nil
}

// getZeroChecker returns a function optimized for checking if a value of a specific type is zero.
func getZeroChecker(t reflect.Type) func(v reflect.Value) bool {
	zero := reflect.Zero(t)
	switch t.Kind() {
	case reflect.String:
		return func(v reflect.Value) bool { return v.Len() == 0 }
	case reflect.Slice, reflect.Map:
		return func(v reflect.Value) bool { return v.IsNil() || v.Len() == 0 }
	case reflect.Ptr, reflect.Interface:
		return func(v reflect.Value) bool { return v.IsNil() }
	case reflect.Struct:
		// Special handling for time.Time
		if t == reflect.TypeOf(time.Time{}) {
			return func(v reflect.Value) bool {
				ts, ok := v.Interface().(time.Time)
				return ok && ts.IsZero()
			}
		}
		// Generic struct comparison (can be slower)
		return func(v reflect.Value) bool { return v.Interface() == zero.Interface() }
	default:
		// Use direct comparison for basic types
		return func(v reflect.Value) bool { return v.Interface() == zero.Interface() }
	}
}

// getTableNameFromType determines the database table name for a given model type.
// It prioritizes the TableName() method if implemented, otherwise uses snake_case of the struct name.
func getTableNameFromType(modelType reflect.Type) string {
	// Ensure we're working with a non-pointer type
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	// Check if the model implements the TableName interface
	modelValue := reflect.New(modelType) // Create a pointer instance to check methods
	if tableNamer, ok := modelValue.Interface().(interface{ TableName() string }); ok {
		name := tableNamer.TableName()
		if name != "" {
			return name
		}
	}

	// Fallback to snake_case plural of the struct name
	return ToSnakeCase(modelType.Name()) + "s" // Simple pluralization
}

// ToSnakeCase converts a string from CamelCase to snake_case.
func ToSnakeCase(str string) string {
	var matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
	var matchAllCap = regexp.MustCompile("([a-z0-9])([A-Z])")

	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}
