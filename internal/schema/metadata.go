package schema

import (
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	driversSchema "github.com/burugo/thing/drivers/schema"
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
	DefaultValue *string                    // Parsed default value from db tag
}

// TableInfo holds the actual schema info introspected from the database (internal use only).
type TableInfo struct {
	Name       string       // Table name
	Columns    []ColumnInfo // All columns
	Indexes    []IndexInfo  // All indexes (including unique)
	PrimaryKey string       // Primary key column name (if any)
}

type ColumnInfo struct {
	Name       string  // Column name
	DataType   string  // Database type (e.g., INT, VARCHAR(255))
	IsNullable bool    // Whether the column is nullable
	IsPrimary  bool    // Whether this column is the primary key
	IsUnique   bool    // Whether this column has a unique constraint
	Default    *string // Default value (if any)
}

// IndexInfo holds metadata for a single index (normal or unique).
type IndexInfo struct {
	Name    string   // Index name
	Columns []string // Column names
	Unique  bool     // Is unique index
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
	Indexes          []IndexInfo           // 普通索引
	UniqueIndexes    []IndexInfo           // 唯一索引
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
		Indexes:          make([]IndexInfo, 0),
		UniqueIndexes:    make([]IndexInfo, 0),
	}
	pkDbName := ""

	numFields := modelType.NumField()
	info.Columns = make([]string, 0, numFields) // Using renamed field
	info.Fields = make([]string, 0, numFields)

	var processFields func(structType reflect.Type, parentIndex []int, isEmbedded bool, compositeIndexes map[string]*IndexInfo, rootInfo *ModelInfo)
	processFields = func(structType reflect.Type, parentIndex []int, isEmbedded bool, compositeIndexes map[string]*IndexInfo, rootInfo *ModelInfo) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] processFields: structType=%s, parentIndex=%v, isEmbedded=%v, panic=%v", structType.Name(), parentIndex, isEmbedded, r)
				panic(r)
			}
		}()
		for i := 0; i < structType.NumField(); i++ {
			if i >= structType.NumField() {
				log.Printf("[PANIC PREVENT] processFields: i=%d out of bounds for structType=%s (NumField=%d)", i, structType.Name(), structType.NumField())
				continue
			}
			field := structType.Field(i)
			dbTag := field.Tag.Get("db")
			diffTag := field.Tag.Get("diff") // Optional diff tag
			var columnName string
			var isPk bool
			var currentFieldName string
			var fieldType reflect.Type
			var dbTagParts []string
			var defaultValuePart *string

			if dbTag != "" {
				rawParts := strings.Split(dbTag, ",")
				dbTagParts = make([]string, 0, len(rawParts))
				for _, part := range rawParts {
					if strings.HasPrefix(strings.ToLower(strings.TrimSpace(part)), "default:") {
						valStr := strings.TrimPrefix(strings.TrimSpace(part), "default:")
						if len(valStr) >= 2 && valStr[0] == '\'' && valStr[len(valStr)-1] == '\'' {
							valStr = valStr[1 : len(valStr)-1]
						}
						tempValStr := valStr // Create a new variable for the pointer
						defaultValuePart = &tempValStr
					} else {
						dbTagParts = append(dbTagParts, part)
					}
				}
			}

			switch {
			case field.Anonymous && field.Type.Kind() == reflect.Struct && field.Type.Name() == "BaseModel":
				// Robust check: ensure structType.Field(i) exists, is struct, and is BaseModel
				if i >= structType.NumField() || structType.Field(i).Type.Kind() != reflect.Struct || structType.Field(i).Name != "BaseModel" || structType.Field(i).Type.Name() != "BaseModel" {
					// log.Printf("[WARN] processFields: Skipping BaseModel aggregation at i=%d, structType=%s, parentIndex=%v (not BaseModel or out of bounds)", i, structType.Name(), parentIndex)
					continue
				}
				baseModelType := field.Type
				for j := 0; j < baseModelType.NumField(); j++ {
					baseField := baseModelType.Field(j)
					baseDbTag := baseField.Tag.Get("db")
					baseDiffTag := baseField.Tag.Get("diff")
					var baseDbTagParts []string
					var baseDefaultValuePart *string

					if baseDbTag != "" {
						rawBaseParts := strings.Split(baseDbTag, ",")
						baseDbTagParts = make([]string, 0, len(rawBaseParts))
						for _, part := range rawBaseParts {
							if strings.HasPrefix(strings.ToLower(strings.TrimSpace(part)), "default:") {
								valStr := strings.TrimPrefix(strings.TrimSpace(part), "default:")
								if len(valStr) >= 2 && valStr[0] == '\'' && valStr[len(valStr)-1] == '\'' {
									valStr = valStr[1 : len(valStr)-1]
								}
								tempValStr := valStr
								baseDefaultValuePart = &tempValStr
							} else {
								baseDbTagParts = append(baseDbTagParts, part)
							}
						}
					}

					if len(baseDbTagParts) > 0 && baseDbTagParts[0] == "-" || !baseField.IsExported() {
						continue
					}
					columnName = ""
					isPk = false
					if len(baseDbTagParts) > 0 {
						columnName = baseDbTagParts[0]
						if len(baseDbTagParts) > 1 {
							for _, part := range baseDbTagParts[1:] {
								if part == "pk" || part == "primarykey" {
									isPk = true
								}
							}
						}
					}
					if columnName == "" {
						columnName = ToSnakeCase(baseField.Name)
					}
					currentFieldName = baseField.Name
					fieldType = baseField.Type
					fullIndex := []int{i, j}
					rootInfo.Columns = append(rootInfo.Columns, columnName)
					rootInfo.Fields = append(rootInfo.Fields, currentFieldName)
					rootInfo.FieldToColumnMap[currentFieldName] = columnName
					rootInfo.ColumnToFieldMap[columnName] = currentFieldName
					zeroChecker := getZeroChecker(fieldType)
					rootInfo.CompareFields = append(rootInfo.CompareFields, ComparableFieldInfo{
						GoName:       currentFieldName,
						DBColumn:     columnName,
						Index:        fullIndex,
						IsZero:       zeroChecker,
						Kind:         fieldType.Kind(),
						Type:         fieldType,
						IsEmbedded:   true,
						IgnoreInDiff: baseDiffTag == "-",
						DefaultValue: baseDefaultValuePart,
					})
					if isPk && pkDbName == "" {
						pkDbName = columnName
					}
					if len(baseDbTagParts) > 1 {
						for _, part := range baseDbTagParts[1:] {
							parseIndexTagFromDb(part, columnName, compositeIndexes, rootInfo)
						}
					}
				}
				continue
			case field.Anonymous && field.Type.Kind() == reflect.Struct:
				// Only recurse for anonymous struct fields
				processFields(field.Type, append(parentIndex, i), true, compositeIndexes, rootInfo)
				continue
			default:
				// 只聚合自身，不递归、不拼接 parentIndex
				if len(dbTagParts) > 0 && dbTagParts[0] == "-" || !field.IsExported() {
					continue
				}
				columnName = ""
				isPk = false
				if len(dbTagParts) > 0 {
					columnName = dbTagParts[0]
					if len(dbTagParts) > 1 {
						for _, part := range dbTagParts[1:] {
							if part == "pk" || part == "primarykey" {
								isPk = true
							}
						}
					}
				}
				if columnName == "" {
					columnName = ToSnakeCase(field.Name)
				}
				currentFieldName = field.Name
				fieldType = field.Type
				fullIndex := []int{i}
				zeroChecker := getZeroChecker(fieldType)
				// Ensure maps are populated for default fields
				rootInfo.Columns = append(rootInfo.Columns, columnName)
				rootInfo.Fields = append(rootInfo.Fields, currentFieldName)
				rootInfo.FieldToColumnMap[currentFieldName] = columnName
				rootInfo.ColumnToFieldMap[columnName] = currentFieldName
				rootInfo.CompareFields = append(rootInfo.CompareFields, ComparableFieldInfo{
					GoName:       currentFieldName,
					DBColumn:     columnName,
					Index:        fullIndex,
					IsZero:       zeroChecker,
					Kind:         fieldType.Kind(),
					Type:         fieldType,
					IsEmbedded:   isEmbedded,
					IgnoreInDiff: diffTag == "-",
					DefaultValue: defaultValuePart,
				})
				if isPk && pkDbName == "" {
					pkDbName = columnName
				}
				if len(dbTagParts) > 1 {
					for _, part := range dbTagParts[1:] {
						parseIndexTagFromDb(part, columnName, compositeIndexes, rootInfo)
					}
				}
			}
			for name, idxInfo := range compositeIndexes {
				if idxInfo.Unique {
					rootInfo.UniqueIndexes = append(rootInfo.UniqueIndexes, *idxInfo)
				} else {
					rootInfo.Indexes = append(rootInfo.Indexes, *idxInfo)
				}
				delete(compositeIndexes, name)
			}
		}
	}

	processFields(modelType, []int{}, false, make(map[string]*IndexInfo), &info)

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
	matchFirstCap := regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllCap := regexp.MustCompile("([a-z0-9])([A-Z])")

	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

// ConvertTableInfo converts a drivers/schema.TableInfo to internal/schema.TableInfo (deep copy).
func ConvertTableInfo(src *driversSchema.TableInfo) *TableInfo {
	if src == nil {
		return nil
	}
	columns := make([]ColumnInfo, len(src.Columns))
	for i, c := range src.Columns {
		columns[i] = ColumnInfo{
			Name:       c.Name,
			DataType:   c.DataType,
			IsNullable: c.IsNullable,
			IsPrimary:  c.IsPrimary,
			IsUnique:   c.IsUnique,
			Default:    c.Default,
		}
	}
	indexes := make([]IndexInfo, len(src.Indexes))
	for i, idx := range src.Indexes {
		indexes[i] = IndexInfo{
			Name:    idx.Name,
			Columns: idx.Columns,
			Unique:  idx.Unique,
		}
	}
	return &TableInfo{
		Name:       src.Name,
		Columns:    columns,
		Indexes:    indexes,
		PrimaryKey: src.PrimaryKey,
	}
}

// 新增 parseIndexTagFromDb 辅助函数
func parseIndexTagFromDb(part string, columnName string, compositeIndexes map[string]*IndexInfo, rootInfo *ModelInfo) {
	part = strings.TrimSpace(part)
	switch {
	case part == "index":
		rootInfo.Indexes = append(rootInfo.Indexes, IndexInfo{
			Name:    "",
			Columns: []string{columnName},
			Unique:  false,
		})
	case part == "unique":
		rootInfo.UniqueIndexes = append(rootInfo.UniqueIndexes, IndexInfo{
			Name:    "",
			Columns: []string{columnName},
			Unique:  true,
		})
	case strings.HasPrefix(part, "index:"):
		indexName := strings.TrimPrefix(part, "index:")
		if indexName == "" {
			log.Printf("Warning: Empty index name found for column %s. Skipping.", columnName)
			return
		}
		if existing, ok := compositeIndexes[indexName]; ok {
			if existing.Unique {
				log.Printf("Error: Index '%s' defined as both unique and non-unique. Skipping column %s.", indexName, columnName)
				return
			}
			existing.Columns = append(existing.Columns, columnName)
		} else {
			compositeIndexes[indexName] = &IndexInfo{
				Name:    indexName,
				Columns: []string{columnName},
				Unique:  false,
			}
		}
	case strings.HasPrefix(part, "unique:"):
		indexName := strings.TrimPrefix(part, "unique:")
		if indexName == "" {
			log.Printf("Warning: Empty unique index name found for column %s. Skipping.", columnName)
			return
		}
		if existing, ok := compositeIndexes[indexName]; ok {
			if !existing.Unique {
				log.Printf("Error: Index '%s' defined as both unique and non-unique. Skipping column %s.", indexName, columnName)
				return
			}
			existing.Columns = append(existing.Columns, columnName)
		} else {
			compositeIndexes[indexName] = &IndexInfo{
				Name:    indexName,
				Columns: []string{columnName},
				Unique:  true,
			}
		}
	}
}
