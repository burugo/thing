package thing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log" // For placeholder logging
	"thing/internal/cache"

	// Added for environment variables
	"os"
	"reflect" // Added for parsing env var
	"strings"
	"sync"
	"time"
)

// --- Constants ---

const (
	// Represents a non-existent entry in the cache, similar to PHP's NoneResult
	NoneResult = "NoneResult"
	// Lock duration
	LockDuration   = 5 * time.Second
	LockRetryDelay = 50 * time.Millisecond
	LockMaxRetries = 5
)

// --- Global Configuration ---

var (
	globalDB     DBAdapter   // Set via Configure
	globalCache  CacheClient // Set via Configure
	isConfigured bool
	configMutex  sync.RWMutex
	// Global cache TTL, determined at startup
	globalCacheTTL time.Duration
)

// Configure sets up the package-level database and cache clients, and the global cache TTL.
// This MUST be called once during application initialization before using Use[T].
// It accepts an optional time.Duration argument to set the global cache TTL.
// If no TTL is provided, it defaults to 8 hours.
func Configure(db DBAdapter, cache CacheClient, ttl ...time.Duration) error { // Added optional ttl parameter
	configMutex.Lock()
	defer configMutex.Unlock()

	defaultTTL := 8 * time.Hour // Define default TTL

	// --- Configure Global TTL ---
	if len(ttl) > 0 {
		// Use the first provided TTL value if it's positive
		if ttl[0] > 0 {
			globalCacheTTL = ttl[0]
			log.Printf("Configuring global cache TTL: %s (from argument)", globalCacheTTL)
		} else {
			globalCacheTTL = defaultTTL
			log.Printf("Warning: Provided TTL is not positive (%s). Using default: %s", ttl[0], globalCacheTTL)
		}
	} else {
		// No TTL provided, use the default
		globalCacheTTL = defaultTTL
		log.Printf("Configuring global cache TTL: %s (default)", globalCacheTTL)
	}
	// --- End Configure Global TTL ---
	// if isConfigured {
	// 	log.Println("Thing package already configured. Re-configuring...")
	// }

	if db == nil {
		return errors.New("DBAdapter must be non-nil")
	}
	if cache == nil {
		return errors.New("CacheClient must be non-nil")
	}
	globalDB = db
	globalCache = cache
	isConfigured = true
	log.Println("Thing package configured globally with DB and Cache adapters.")

	return nil
}

// --- Thing Core Struct ---

// Thing is the central access point for ORM operations, analogous to gorm.DB.
// It holds database/cache clients and the context for operations.
type Thing[T any] struct {
	db    DBAdapter
	cache CacheClient
	ctx   context.Context
	info  *ModelInfo // Pre-computed metadata for type T
}

// --- Thing Constructors & Accessors ---

// New creates a new Thing instance with default context.Background().
// Made generic: requires type parameter T when called, e.g., New[MyModel](...).
func New[T any](db DBAdapter, cache CacheClient) (*Thing[T], error) {
	log.Println("DEBUG: Entering New[T]") // Added log
	if db == nil {
		log.Println("DEBUG: New[T] - DB is nil") // Added log
		return nil, errors.New("DBAdapter must be non-nil")
	}
	if cache == nil {
		log.Println("DEBUG: New[T] - Cache is nil") // Added log
		return nil, errors.New("CacheClient must be non-nil")
	}
	log.Println("New Thing instance created.")

	// Pre-compute model info for T
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	log.Printf("DEBUG: New[T] - Getting model info for type: %s", modelType.Name()) // Added log
	info, err := GetCachedModelInfo(modelType)                                      // Renamed: getCachedModelInfo -> GetCachedModelInfo
	if err != nil {
		log.Printf("DEBUG: New[T] - Error getting model info: %v", err) // Added log
		return nil, fmt.Errorf("failed to get model info for type %s: %w", modelType.Name(), err)
	}
	log.Printf("DEBUG: New[T] - Got model info: %+v", info) // Added log
	// TableName is now populated within GetCachedModelInfo
	// info.TableName = getTableNameFromType(modelType) // Removed redundant call
	if info.TableName == "" {
		log.Printf("Warning: Could not determine table name for type %s during New. Relying on instance method?", modelType.Name())
	}

	log.Println("DEBUG: New[T] - Creating Thing struct") // Added log
	t := &Thing[T]{
		db:    db,
		cache: cache,
		ctx:   context.Background(), // Default context
		info:  info,                 // Store pre-computed info
	}
	log.Println("DEBUG: New[T] - Returning new Thing instance") // Added log
	return t, nil
}

// Use returns a Thing instance for the specified type T, using the globally
// configured DBAdapter and CacheClient.
// The package MUST be configured using Configure() before calling Use[T].
func Use[T any]() (*Thing[T], error) {
	configMutex.RLock()
	defer configMutex.RUnlock()
	if !isConfigured {
		return nil, errors.New("thing.Use[T] called before thing.Configure()")
	}
	// Create a new Thing instance using the global adapters
	return New[T](globalDB, globalCache)
}

// --- Thing Public Methods ---

// WithContext returns a shallow copy of Thing with the context replaced.
// This is used to set the context for a specific chain of operations.
func (t *Thing[T]) WithContext(ctx context.Context) *Thing[T] { // Returns *Thing
	if ctx == nil {
		log.Println("Warning: nil context passed to WithContext, using context.Background()")
		ctx = context.Background()
	}
	// Create a shallow copy and replace the context
	newThing := *t     // Copy struct values (dbAdapter, cacheClient, old ctx)
	newThing.ctx = ctx // Set the new context
	return &newThing   // Return pointer to the copy
}

// --- BaseModel Struct --- // REMOVED - Moved to model.go

// --- Core Internal CRUD & Fetching Logic --- // MOVED to crud_internal.go

// --- Event System --- // MOVED to hooks.go

// --- Model Metadata Cache --- (Moved Down)

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
			diffTag := field.Tag.Get("diff") // Optional diff tag

			// If field is embedded struct, process its fields recursively
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				// For BaseModel, process fields directly since they're important
				if field.Type == reflect.TypeOf(BaseModel{}) {
					baseModelType := field.Type
					for j := 0; j < baseModelType.NumField(); j++ {
						baseField := baseModelType.Field(j)
						baseDbTag := baseField.Tag.Get("db")
						baseDiffTag := baseField.Tag.Get("diff") // Optional diff tag for BaseModel fields

						if baseDbTag == "-" || !baseField.IsExported() {
							continue
						}

						colName := baseDbTag
						if colName == "" {
							colName = strings.ToLower(baseField.Name)
						}

						// Build the field index by appending to the parent index
						fieldIndex := append(append([]int{}, parentIndex...), i, j)

						info.Columns = append(info.Columns, colName) // Using renamed field
						info.Fields = append(info.Fields, baseField.Name)
						info.FieldToColumnMap[baseField.Name] = colName
						info.ColumnToFieldMap[colName] = baseField.Name

						// Skip adding PK to comparable fields
						if colName == "id" {
							pkDbName = colName
							continue
						}

						// Skip fields marked to be ignored in diff
						if baseDiffTag == "-" {
							continue
						}

						// Add to compareFields if not explicitly ignored
						cfInfo := ComparableFieldInfo{
							GoName:       baseField.Name,
							DBColumn:     colName,
							Index:        fieldIndex,
							Kind:         baseField.Type.Kind(),
							Type:         baseField.Type,
							IsEmbedded:   true,
							IgnoreInDiff: false,
						}

						// Add specialized zero-value checker based on field type
						cfInfo.IsZero = getZeroChecker(baseField.Type)

						info.CompareFields = append(info.CompareFields, cfInfo)
					}
				} else {
					// Process other embedded structs recursively
					newIndex := append(append([]int{}, parentIndex...), i)
					processFields(field.Type, newIndex, true)
				}
				continue
			}

			// Skip unexported fields and fields marked with db:"-"
			if dbTag == "-" || !field.IsExported() {
				continue
			}

			colName := dbTag
			if colName == "" {
				// Skip fields without db tag
				continue
			}

			// Build the field index by appending to the parent index
			fieldIndex := append(append([]int{}, parentIndex...), i)

			info.Columns = append(info.Columns, colName) // Using renamed field
			info.Fields = append(info.Fields, field.Name)
			info.FieldToColumnMap[field.Name] = colName
			info.ColumnToFieldMap[colName] = field.Name

			if colName == "id" && pkDbName == "" {
				pkDbName = colName
				continue // Skip adding PK to comparable fields
			}

			// Skip fields marked to be ignored in diff
			if diffTag == "-" {
				continue
			}

			// Add to compareFields if not explicitly ignored
			cfInfo := ComparableFieldInfo{
				GoName:       field.Name,
				DBColumn:     colName,
				Index:        fieldIndex,
				Kind:         field.Type.Kind(),
				Type:         field.Type,
				IsEmbedded:   isEmbedded,
				IgnoreInDiff: false,
			}

			// Add specialized zero-value checker based on field type
			cfInfo.IsZero = getZeroChecker(field.Type)

			info.CompareFields = append(info.CompareFields, cfInfo)
		}
	}

	processFields(modelType, []int{}, false)

	if pkDbName == "" {
		return nil, fmt.Errorf("primary key column (assumed 'id') not found via 'db:\"id\"' tag in struct %s or its embedded BaseModel", modelType.Name())
	}
	info.PkName = pkDbName
	info.TableName = getTableNameFromType(modelType)
	if info.TableName == "" {
		log.Printf("Warning: Could not determine table name for type %s in GetCachedModelInfo.", modelType.Name())
	}

	actualInfo, _ := ModelCache.LoadOrStore(modelType, &info)
	return actualInfo.(*ModelInfo), nil
}

// getZeroChecker returns a function that checks if a value is the zero value for its type
func getZeroChecker(t reflect.Type) func(v reflect.Value) bool {
	switch t.Kind() {
	case reflect.Bool:
		return func(v reflect.Value) bool { return !v.Bool() }
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return func(v reflect.Value) bool { return v.Int() == 0 }
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return func(v reflect.Value) bool { return v.Uint() == 0 }
	case reflect.Float32, reflect.Float64:
		return func(v reflect.Value) bool { return v.Float() == 0 }
	case reflect.Complex64, reflect.Complex128:
		return func(v reflect.Value) bool { return v.Complex() == complex(0, 0) }
	case reflect.Array:
		return func(v reflect.Value) bool {
			// For arrays, check if all elements are zero
			for i := 0; i < v.Len(); i++ {
				if !reflect.DeepEqual(v.Index(i).Interface(), reflect.Zero(v.Index(i).Type()).Interface()) {
					return false
				}
			}
			return true
		}
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return func(v reflect.Value) bool { return v.IsNil() }
	case reflect.String:
		return func(v reflect.Value) bool { return v.String() == "" }
	case reflect.Struct:
		// Special case for time.Time
		if t == reflect.TypeOf(time.Time{}) {
			zeroTime := time.Time{}
			return func(v reflect.Value) bool {
				// Check if this is a zero time, which can be tricky due to location/monotonic data
				t := v.Interface().(time.Time)
				return t.IsZero() || t.Equal(zeroTime)
			}
		}
		// For other structs, use reflection to check if all fields are zero
		return func(v reflect.Value) bool {
			return reflect.DeepEqual(v.Interface(), reflect.Zero(t).Interface())
		}
	default:
		return func(v reflect.Value) bool {
			return reflect.DeepEqual(v.Interface(), reflect.Zero(t).Interface())
		}
	}
}

// --- Change Detection --- (Moved Down)

// findChangedFields compares two structs and returns a map of column names to changed values.
// It will always skip private fields, primary key fields, and fields tagged with `db:"-"`.
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
// DEPRECATED in favor of findChangedFieldsAdvanced once implemented and tested.
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
	if col, ok := info.FieldToColumnMap[updatedAtGoFieldName]; ok {
		updatedAtDBColName = col
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

// findChangedFieldsAdvanced compares two structs (original and updated) using cached metadata
// and optimized comparison logic. Returns a map of column names to the changed values.
// DEPRECATED: Use findChangedFieldsSimple instead, which is more efficient for typical database models.
func buildSelectSQL(info *ModelInfo) string {
	if info.TableName == "" || len(info.Columns) == 0 {
		log.Printf("Error: buildSelectSQL called with incomplete modelInfo: %+v", info)
		return ""
	}
	quotedColumns := make([]string, len(info.Columns))
	for i, col := range info.Columns {
		quotedColumns[i] = fmt.Sprintf("\"%s\"", col)
	}
	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(quotedColumns, ", "), info.TableName)
}

// buildSelectIDsSQL constructs a SELECT statement to fetch only primary key IDs.
func buildSelectIDsSQL(info *ModelInfo, params cache.QueryParams) (string, []interface{}) {
	var query strings.Builder
	args := []interface{}{}
	if info.TableName == "" || info.PkName == "" {
		log.Printf("Error: buildSelectIDsSQL called with incomplete modelInfo: %+v", info)
		return "", nil
	}
	query.WriteString(fmt.Sprintf("SELECT \"%s\" FROM %s", info.PkName, info.TableName))
	if params.Where != "" {
		query.WriteString(" WHERE ")
		query.WriteString(params.Where)
		args = append(args, params.Args...)
	}
	if params.Order != "" {
		query.WriteString(" ORDER BY ")
		query.WriteString(params.Order)
	}
	return query.String(), args
}

// --- Reflection & Value Helpers --- (Moved Down)

// getTableNameFromType determines table name from reflect.Type.
func getTableNameFromType(modelType reflect.Type) string {
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return ""
	}
	instPtr := reflect.New(modelType)
	meth := instPtr.MethodByName("TableName")
	if meth.IsValid() && meth.Type().NumIn() == 0 && meth.Type().NumOut() == 1 && meth.Type().Out(0) == reflect.TypeOf("") {
		results := meth.Call(nil)
		if len(results) > 0 {
			if name := results[0].String(); name != "" {
				return name
			}
		}
	}
	instVal := instPtr.Elem()
	methVal := instVal.MethodByName("TableName")
	if methVal.IsValid() && methVal.Type().NumIn() == 0 && methVal.Type().NumOut() == 1 && methVal.Type().Out(0) == reflect.TypeOf("") {
		results := methVal.Call(nil)
		if len(results) > 0 {
			if name := results[0].String(); name != "" {
				return name
			}
		}
	}
	return strings.ToLower(modelType.Name()) + "s" // Pluralize convention
}

// getBaseModelPtr returns a pointer to the embedded BaseModel if it exists and is addressable.
func getBaseModelPtr(value interface{}) *BaseModel {
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() == reflect.Struct {
		bmField := val.FieldByName("BaseModel")
		if bmField.IsValid() && bmField.Type() == reflect.TypeOf(BaseModel{}) && bmField.CanAddr() {
			return bmField.Addr().Interface().(*BaseModel)
		}
	}
	return nil
}

// setNewRecordFlagIfBaseModel sets the flag if the value embeds BaseModel.
func setNewRecordFlagIfBaseModel(value interface{}, isNew bool) {
	if bmPtr := getBaseModelPtr(value); bmPtr != nil {
		bmPtr.SetNewRecordFlag(isNew)
	}
}

// setCreatedAtTimestamp sets the CreatedAt field if it exists.
func setCreatedAtTimestamp(value interface{}, t time.Time) {
	if bmPtr := getBaseModelPtr(value); bmPtr != nil {
		bmPtr.CreatedAt = t
		return
	}
	val := reflect.ValueOf(value).Elem()
	field := val.FieldByName("CreatedAt")
	if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
		field.Set(reflect.ValueOf(t))
	}
}

// setUpdatedAtTimestamp sets the UpdatedAt field if it exists.
func setUpdatedAtTimestamp(value interface{}, t time.Time) {
	if bmPtr := getBaseModelPtr(value); bmPtr != nil {
		bmPtr.UpdatedAt = t
		return
	}
	val := reflect.ValueOf(value).Elem()
	field := val.FieldByName("UpdatedAt")
	if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
		field.Set(reflect.ValueOf(t))
	}
}

// --- Cache Helpers --- // MOVED to cache.go

// // generateCacheKey creates a standard cache key string for a single model.
// func generateCacheKey(tableName string, id int64) string {
// 	// Format: {tableName}:{id}
// 	return fmt.Sprintf("%s:%d", tableName, id)
// }

// // withLock acquires a lock, executes the action, and releases the lock.
// func withLock(ctx context.Context, cache CacheClient, lockKey string, action func(ctx context.Context) error) error {
// ... (rest of withLock code omitted)
// }

// --- Querying Structs --- // MOVED to query.go

// // QueryParams defines parameters for database queries.
// type QueryParams struct {
// 	Where string        // Raw WHERE clause (e.g., "status = ? AND name LIKE ?")
// 	Args  []interface{} // Arguments for the WHERE clause placeholders
// 	Order string        // Raw ORDER BY clause (e.g., "created_at DESC")
// 	// Start    int           // Offset (for pagination) - REMOVED
// 	// Limit    int           // Limit (for pagination) - REMOVED
// 	Preloads []string // List of relationship names to eager-load (e.g., ["Author", "Comments"])
// }

// --- NEW Relationship Preloading Functions --- (Added at the end)

// RelationshipOpts holds parsed options from the 'thing' tag for relationships.
type RelationshipOpts struct {
	RelationType string // "belongsTo", "hasMany"
	ForeignKey   string // FK field name in the *owning* struct (for belongsTo) or *related* struct (for hasMany)
	LocalKey     string // PK field name in the *owning* struct (defaults to info.pkName)
	RelatedModel string // Optional: Specify related model name if different from field type
}

// parseRelationTag parses the `thing` tag for relationship definitions.
// Example: `thing:"rel=belongsTo;fk=AuthorID"`
// Example: `thing:"rel=hasMany;fk=PostID"`
func parseRelationTag(tag string) (opts RelationshipOpts, err error) {
	parts := strings.Split(tag, ";")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue // Ignore invalid parts
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		// --- DEBUG LOGGING ---
		log.Printf("DEBUG parseRelationTag: Part='%s', Key='%s', Value='%s'", part, key, value)
		// --- END DEBUG LOGGING ---

		switch strings.ToLower(key) {
		case "rel":
			opts.RelationType = strings.ToLower(value) // Ensure value is lowercased
			// --- DEBUG LOGGING ---
			log.Printf("DEBUG parseRelationTag: Set opts.RelationType to '%s'", opts.RelationType)
			// --- END DEBUG LOGGING ---
		case "fk":
			opts.ForeignKey = value
		case "localkey":
			opts.LocalKey = value
		case "model":
			opts.RelatedModel = value // Less common, might be needed for interface fields?
		}
	}

	if opts.RelationType != "belongs_to" && opts.RelationType != "has_many" {
		err = fmt.Errorf("invalid or missing 'rel' type in tag: %s", tag)
		return
	}
	// Basic validation (more could be added)
	if opts.ForeignKey == "" {
		// FK is usually required, derivation logic is complex, enforce for now.
		err = fmt.Errorf("missing required 'fk' (foreign key) in relation tag: %s", tag)
		return
	}

	return opts, nil
}

// preloadRelations dispatches preloading work based on relationship type.
func (t *Thing[T]) preloadRelations(ctx context.Context, results []*T, preloadName string) error {
	if len(results) == 0 {
		return nil // Nothing to preload
	}
	// 1. Get reflect.Type and reflect.Value of the results slice
	resultsVal := reflect.ValueOf(results)
	modelType := reflect.TypeOf(results[0]).Elem() // Get type T from the first element

	// 2. Get the StructField for preloadName
	field, found := modelType.FieldByName(preloadName)
	if !found {
		return fmt.Errorf("relation field '%s' not found in model type %s", preloadName, modelType.Name())
	}

	// 3. Parse the `thing` tag
	tag := field.Tag.Get("thing")
	if tag == "" {
		return fmt.Errorf("missing 'thing' tag on relation field '%s' in model type %s", preloadName, modelType.Name())
	}
	opts, err := parseRelationTag(tag)
	if err != nil {
		return fmt.Errorf("error parsing 'thing' tag for field '%s': %w", preloadName, err)
	}

	// Ensure LocalKey is set (defaults to primary key of T)
	if opts.LocalKey == "" {
		opts.LocalKey = t.info.PkName
		if opts.LocalKey == "" {
			return fmt.Errorf("cannot determine local key (primary key) for model type %s", modelType.Name())
		}
	}

	// 4. Based on tag, call appropriate helper
	log.Printf("Dispatching preload for '%s.%s' (Type: %s, FK: %s, LocalKey: %s)", modelType.Name(), preloadName, opts.RelationType, opts.ForeignKey, opts.LocalKey)
	switch opts.RelationType {
	case "belongs_to":
		return t.preloadBelongsTo(ctx, resultsVal, field, opts)
	case "has_many":
		return t.preloadHasMany(ctx, resultsVal, field, opts)
	default:
		// Should be caught by parseRelationTag, but defensive check
		return fmt.Errorf("unsupported relation type '%s' for field '%s'", opts.RelationType, preloadName)
	}
}

// preloadBelongsTo handles eager loading for BelongsTo relationships.
func (t *Thing[T]) preloadBelongsTo(ctx context.Context, resultsVal reflect.Value, field reflect.StructField, opts RelationshipOpts) error {
	// T = Owning Model (e.g., Post)
	// R = Related Model (e.g., User)

	// --- Type checking ---
	relatedFieldType := field.Type // Type of the field (e.g., *User)
	if relatedFieldType.Kind() != reflect.Ptr || relatedFieldType.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("belongsTo field '%s' must be a pointer to a struct, but got %s", field.Name, relatedFieldType.String())
	}
	relatedModelType := relatedFieldType.Elem() // Type R (e.g., User)
	log.Printf("Preloading BelongsTo: Field %s (*%s), FK in %s: %s", field.Name, relatedModelType.Name(), t.info.TableName, opts.ForeignKey)

	// --- Get Foreign Key Info from Owning Model T ---
	owningModelType := t.info.ColumnToFieldMap // Use exported name
	fkFieldName, fkFieldFound := owningModelType[opts.ForeignKey]
	if !fkFieldFound {
		if _, directFieldFound := reflect.TypeOf(resultsVal.Index(0).Interface()).Elem().FieldByName(opts.ForeignKey); directFieldFound {
			fkFieldName = opts.ForeignKey
			fkFieldFound = true
		} else {
			return fmt.Errorf("foreign key field '%s' (from tag 'fk') not found in owning model %s", opts.ForeignKey, resultsVal.Type().Elem().Elem().Name())
		}
	}
	log.Printf("Foreign Key Field in Owning Model (%s): %s", resultsVal.Type().Elem().Elem().Name(), fkFieldName)

	// --- Collect Unique Foreign Key Values from results ---
	fkValuesMap := make(map[int64]bool) // Use int64 specifically for IDs
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelElem := resultsVal.Index(i).Elem()
		fkFieldVal := owningModelElem.FieldByName(fkFieldName)
		if fkFieldVal.IsValid() {
			// Attempt to convert FK to int64
			var key int64
			if fkFieldVal.Type().ConvertibleTo(reflect.TypeOf(key)) {
				key = fkFieldVal.Convert(reflect.TypeOf(key)).Int()
				if key != 0 { // Only collect non-zero keys
					fkValuesMap[key] = true
				}
			} else {
				log.Printf("WARN: FK field '%s' value (%v) on element %d is not convertible to int64", fkFieldName, fkFieldVal.Interface(), i)
			}
		} else {
			log.Printf("WARN: FK field '%s' not valid on element %d during belongsTo preload", fkFieldName, i)
		}
	}

	if len(fkValuesMap) == 0 {
		log.Println("No valid non-zero foreign keys found for belongsTo preload.")
		return nil // No related models to load
	}

	uniqueFkList := make([]int64, 0, len(fkValuesMap))
	for k := range fkValuesMap {
		uniqueFkList = append(uniqueFkList, k)
	}
	log.Printf("Collected %d unique foreign keys for %s: %v", len(uniqueFkList), field.Name, uniqueFkList)

	// --- Fetch Related Models (Type R) using the internal helper ---
	relatedInfo, err := GetCachedModelInfo(relatedModelType)
	if err != nil {
		return fmt.Errorf("failed to get model info for related type %s: %w", relatedModelType.Name(), err)
	}

	// Call the internal helper, passing the concrete relatedModelType
	relatedMap, err := fetchModelsByIDsInternal(ctx, t.cache, t.db, relatedInfo, relatedModelType, uniqueFkList)
	if err != nil {
		return fmt.Errorf("failed to fetch related %s models using internal helper: %w", relatedModelType.Name(), err)
	}

	// --- Map Related Models back to original results --- (Using reflect.Value map)
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelPtr := resultsVal.Index(i)                  // *T
		owningModelElem := owningModelPtr.Elem()               // T
		fkFieldVal := owningModelElem.FieldByName(fkFieldName) // Get FK field value

		if fkFieldVal.IsValid() {
			var fkValueInt64 int64
			if fkFieldVal.Type().ConvertibleTo(reflect.TypeOf(fkValueInt64)) {
				fkValueInt64 = fkFieldVal.Convert(reflect.TypeOf(fkValueInt64)).Int()
				if relatedModelPtr, found := relatedMap[fkValueInt64]; found {
					relationField := owningModelElem.FieldByName(field.Name) // Get the *User field
					if relationField.IsValid() && relationField.CanSet() {
						log.Printf("DEBUG Preload Set: Setting %s.%s (FK: %d) to %v", owningModelElem.Type().Name(), field.Name, fkValueInt64, relatedModelPtr.Interface()) // DEBUG LOG
						relationField.Set(relatedModelPtr)                                                                                                                  // Set post.Author = userPtr (*R)
					} else {
						log.Printf("WARN Preload Set: Relation field %s.%s is not valid or settable", owningModelElem.Type().Name(), field.Name) // DEBUG LOG
					}
				} else {
					log.Printf("DEBUG Preload Set: Related model for FK %d not found in map", fkValueInt64) // DEBUG LOG
				}
			} // else: FK was not convertible or was zero, do nothing
		}
	}

	log.Printf("Successfully preloaded BelongsTo relation '%s'", field.Name)
	return nil
}

// preloadHasMany handles eager loading for HasMany relationships.
func (t *Thing[T]) preloadHasMany(ctx context.Context, resultsVal reflect.Value, field reflect.StructField, opts RelationshipOpts) error {
	// T = Owning Model (e.g., Post)
	// R = Related Model (e.g., Comment)

	// --- Type checking ---
	relatedFieldType := field.Type // Type of the field (e.g., []Comment or []*Comment)
	if relatedFieldType.Kind() != reflect.Slice {
		return fmt.Errorf("hasMany field '%s' must be a slice, but got %s", field.Name, relatedFieldType.String())
	}
	relatedElemType := relatedFieldType.Elem() // Type of slice elements (e.g., Comment or *Comment)
	var relatedModelType reflect.Type
	var relatedIsSliceOfPtr bool
	if relatedElemType.Kind() == reflect.Ptr && relatedElemType.Elem().Kind() == reflect.Struct {
		relatedModelType = relatedElemType.Elem() // Type R (e.g., Comment)
		relatedIsSliceOfPtr = true
	} else if relatedElemType.Kind() == reflect.Struct {
		relatedModelType = relatedElemType // Type R (e.g., Comment)
		relatedIsSliceOfPtr = false
	} else {
		return fmt.Errorf("hasMany field '%s' must be a slice of structs or pointers to structs, got slice of %s", field.Name, relatedElemType.String())
	}
	log.Printf("Preloading HasMany: Field %s (%s), FK in %s: %s", field.Name, relatedFieldType.String(), relatedModelType.Name(), opts.ForeignKey)

	// --- Get Local Key Info from Owning Model T ---
	localKeyColName := opts.LocalKey // e.g., "id"
	localKeyGoFieldName, ok := t.info.ColumnToFieldMap[localKeyColName]
	if !ok {
		return fmt.Errorf("local key column '%s' not found in model %s info", localKeyColName, resultsVal.Type().Elem().Elem().Name())
	}
	log.Printf("Local Key Field in Owning Model (%s): %s (DB: %s)", resultsVal.Type().Elem().Elem().Name(), localKeyGoFieldName, localKeyColName)

	// --- Collect Local Key Values from results ---
	localKeyValues := make(map[interface{}]bool)
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelElem := resultsVal.Index(i).Elem()                  // Get underlying struct T
		lkFieldVal := owningModelElem.FieldByName(localKeyGoFieldName) // Get local key field (e.g., ID)
		if lkFieldVal.IsValid() {
			key := lkFieldVal.Interface() // Get the value (e.g., int64 ID)
			localKeyValues[key] = true
		} else {
			log.Printf("WARN: Local key field '%s' not valid on element %d during hasMany preload", localKeyGoFieldName, i)
		}
	}

	if len(localKeyValues) == 0 {
		log.Println("No valid local keys found for hasMany preload.")
		// Ensure the relation slice is initialized to empty on the owning models
		for i := 0; i < resultsVal.Len(); i++ {
			owningModelElem := resultsVal.Index(i).Elem()
			relationField := owningModelElem.FieldByName(field.Name)
			if relationField.IsValid() && relationField.CanSet() {
				relationField.Set(reflect.MakeSlice(relatedFieldType, 0, 0))
			}
		}
		return nil // No related models to load
	}

	uniqueLkList := make([]interface{}, 0, len(localKeyValues))
	for k := range localKeyValues {
		uniqueLkList = append(uniqueLkList, k)
	}
	log.Printf("Collected %d unique local keys for %s: %v", len(uniqueLkList), field.Name, uniqueLkList)

	// --- Step 1: Get Related Model IDs ---
	relatedInfo, err := GetCachedModelInfo(relatedModelType)
	if err != nil {
		return fmt.Errorf("failed to get model info for related type %s: %w", relatedModelType.Name(), err)
	}
	relatedFkColName := opts.ForeignKey // FK column name in the related table R

	// --- Get Related Model FK Go field name ---
	relatedFkGoFieldName, fkFieldFound := relatedInfo.ColumnToFieldMap[relatedFkColName]
	if !fkFieldFound {
		// Fallback check if FK name matches Go field name directly
		if _, directFieldFound := relatedModelType.FieldByName(opts.ForeignKey); directFieldFound {
			relatedFkGoFieldName = opts.ForeignKey
			fkFieldFound = true
		} else {
			return fmt.Errorf("foreign key column '%s' (from tag 'fk') not found in related model %s info or as a direct field name", relatedFkColName, relatedModelType.Name())
		}
	}
	log.Printf("Foreign Key Field in Related Model (%s): %s (DB: %s)", relatedModelType.Name(), relatedFkGoFieldName, relatedFkColName)

	// Prepare query params for fetching related model IDs
	placeholders := strings.Repeat("?,", len(uniqueLkList))[:len(uniqueLkList)*2-1]
	relatedIDParams := cache.QueryParams{
		Where: fmt.Sprintf("\"%s\" IN (%s)", relatedFkColName, placeholders), // Ensure FK column is quoted
		Args:  uniqueLkList,
		// Potentially add Order from tag later?
	}

	var relatedIDs []int64
	listCacheKey := ""
	cacheHit := false // Flag to indicate if we got a definitive result (IDs or NoneResult) from cache

	if t.cache != nil {
		// 1. Generate Cache Key (Error handling below)
		keyGenParams := relatedIDParams
		normalizedArgs := make([]interface{}, len(keyGenParams.Args))
		for i, arg := range keyGenParams.Args {
			normalizedArgs[i] = fmt.Sprintf("%v", arg)
		}
		keyGenParams.Args = normalizedArgs
		paramsBytes, jsonErr := json.Marshal(keyGenParams)

		if jsonErr == nil {
			hasher := sha256.New()
			hasher.Write([]byte(relatedInfo.TableName))
			hasher.Write(paramsBytes)
			hash := hex.EncodeToString(hasher.Sum(nil))
			listCacheKey = fmt.Sprintf("list:%s:%s", relatedInfo.TableName, hash)

			// 2. Try GetQueryIDs directly (handles NoneResult internally now)
			cachedIDs, queryIDsErr := t.cache.GetQueryIDs(ctx, listCacheKey)

			if queryIDsErr == nil {
				// Cache hit with actual IDs
				log.Printf("CACHE HIT (Query IDs): Found %d related IDs for key %s", len(cachedIDs), listCacheKey)
				relatedIDs = cachedIDs
				cacheHit = true // Got the IDs from cache
			} else if errors.Is(queryIDsErr, ErrNotFound) {
				// Cache miss
				log.Printf("CACHE MISS (Query IDs): Key %s not found.", listCacheKey)
				// cacheHit remains false
			} else {
				// Other cache error
				log.Printf("WARN: Cache GetQueryIDs error for key %s: %v. Treating as cache miss.", listCacheKey, queryIDsErr)
				// cacheHit remains false
			}

		} else {
			log.Printf("WARN: Failed to marshal params for list cache key generation: %v", jsonErr)
			// cacheHit remains false, proceed to DB query
		}
	} // End if t.cache != nil

	// Fetch IDs from DB if cache was not hit (cacheHit is false)
	if !cacheHit {
		// Build query to select only the primary key of the related model
		idQuery := fmt.Sprintf("SELECT \"%s\" FROM %s WHERE %s",
			relatedInfo.PkName,    // Use exported name
			relatedInfo.TableName, // Related table R
			relatedIDParams.Where, // WHERE clause (e.g., "\"user_id\" IN (?,?,?)")
		)
		// TODO: Add Order By if needed

		log.Printf("Executing query for related IDs: %s [%v]", idQuery, relatedIDParams.Args)

		// Execute the query - db.Select expects a slice destination
		// Create a slice of the appropriate type for the PK (assuming int64 for now)
		idSliceDest := make([]int64, 0)
		err = t.db.Select(ctx, &idSliceDest, idQuery, relatedIDParams.Args...)

		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to fetch related %s IDs: %w", relatedModelType.Name(), err)
		}
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("DB HIT (Zero IDs): No related %s IDs found for local keys %v", relatedModelType.Name(), uniqueLkList)
		} else {
			log.Printf("DB HIT (IDs): Fetched %d related %s IDs from database.", len(idSliceDest), relatedModelType.Name())
		}

		relatedIDs = idSliceDest

		// Cache the fetched IDs (or NoneResult if empty)
		if t.cache != nil && listCacheKey != "" {
			if len(relatedIDs) > 0 {
				log.Printf("Caching %d fetched IDs for query key %s", len(relatedIDs), listCacheKey)
				if qcErr := t.cache.SetQueryIDs(ctx, listCacheKey, relatedIDs, globalCacheTTL); qcErr != nil {
					log.Printf("WARN: Failed to cache query IDs for key %s: %v", listCacheKey, qcErr)
				}
			} else {
				log.Printf("Caching NoneResult for query key %s", listCacheKey)
				if qcErr := t.cache.Set(ctx, listCacheKey, NoneResult, globalCacheTTL); qcErr != nil {
					log.Printf("WARN: Failed to cache NoneResult for query key %s: %v", listCacheKey, qcErr)
				}
			}
		}
	}

	// --- Step 2: Fetch Related Models using IDs ---
	var relatedModelsMap map[int64]reflect.Value
	if len(relatedIDs) > 0 {
		// Use the internal helper which checks object cache
		log.Printf("Fetching %d related %s models using fetchModelsByIDsInternal", len(relatedIDs), relatedModelType.Name())
		relatedModelsMap, err = fetchModelsByIDsInternal(ctx, t.cache, t.db, relatedInfo, relatedModelType, relatedIDs)
		if err != nil {
			// Log error but proceed to map any models that might have been fetched from cache before the error
			log.Printf("WARN: Error during fetchModelsByIDsInternal for %s: %v. Proceeding with potentially partial results.", relatedModelType.Name(), err)
			// return fmt.Errorf("failed to fetch related %s models using internal helper: %w", relatedModelType.Name(), err)
		}
		log.Printf("Successfully fetched/retrieved %d related %s models from internal helper.", len(relatedModelsMap), relatedModelType.Name())
	} else {
		// If no IDs were found (either from cache or DB), initialize an empty map
		relatedModelsMap = make(map[int64]reflect.Value)
		log.Printf("No related %s IDs found, skipping fetchModelsByIDsInternal.", relatedModelType.Name())
	}

	// --- Step 3: Map Related Models back to results ---
	// Group related models by their foreign key value
	groupedRelatedMap := make(map[interface{}][]reflect.Value) // Map FK value -> Slice of *R or R

	for _, relatedModelPtrVal := range relatedModelsMap { // Iterate over map[id]reflect.Value(*R)
		relatedModelElem := relatedModelPtrVal.Elem()                    // Get R from *R
		fkValField := relatedModelElem.FieldByName(relatedFkGoFieldName) // Get the FK field (e.g., UserID)
		if !fkValField.IsValid() {
			log.Printf("WARN: FK field '%s' not valid on fetched related model %s during mapping", relatedFkGoFieldName, relatedModelType.Name())
			continue
		}
		fkValue := fkValField.Interface() // Get the FK value (e.g., the owning User's ID)

		var modelToAppend reflect.Value
		if relatedIsSliceOfPtr {
			modelToAppend = relatedModelPtrVal // Append *R
		} else {
			modelToAppend = relatedModelElem // Append R
		}
		groupedRelatedMap[fkValue] = append(groupedRelatedMap[fkValue], modelToAppend)
	}

	// Iterate through original results (owning models) and set the relationship field
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelPtr := resultsVal.Index(i)                          // *T
		owningModelElem := owningModelPtr.Elem()                       // T
		lkFieldVal := owningModelElem.FieldByName(localKeyGoFieldName) // Get local key field (e.g., ID)

		relationField := owningModelElem.FieldByName(field.Name) // Get the []R or []*R field
		if !relationField.IsValid() || !relationField.CanSet() {
			log.Printf("WARN: Cannot set hasMany field '%s' on owning model %s at index %d", field.Name, resultsVal.Type().Elem().Elem().Name(), i)
			continue
		}

		if lkFieldVal.IsValid() {
			lkValue := lkFieldVal.Interface() // Get the local key value

			// Find the slice of related models for this local key value
			if relatedSliceValues, found := groupedRelatedMap[lkValue]; found {
				// Create a new slice of the correct type ([]R or []*R) and append results
				finalSlice := reflect.MakeSlice(relatedFieldType, 0, len(relatedSliceValues))
				for _, relatedVal := range relatedSliceValues {
					finalSlice = reflect.Append(finalSlice, relatedVal)
				}
				relationField.Set(finalSlice)
			} else {
				// No related models found for this owner, set empty slice
				relationField.Set(reflect.MakeSlice(relatedFieldType, 0, 0))
			}
		} else {
			// Local key was invalid, set empty slice
			relationField.Set(reflect.MakeSlice(relatedFieldType, 0, 0))
		}
	}

	log.Printf("Successfully preloaded HasMany relation '%s'", field.Name)
	return nil
}

// loadInternal explicitly loads relationships for a given model instance using the provided context.
// This is the internal implementation called by the public Load method.
// model must be a pointer to a struct of type T.
// relations are the string names of the fields representing the relationships to load.
func (t *Thing[T]) loadInternal(ctx context.Context, model *T, relations ...string) error {
	if model == nil {
		return errors.New("model cannot be nil")
	}
	if len(relations) == 0 {
		return nil // Nothing to load
	}
	if t.info == nil { // Add check for safety
		return errors.New("loadInternal: model info not available on Thing instance")
	}

	// Wrap the single model in a slice to reuse the preloadRelations helper
	modelSlice := []*T{model}

	for _, relationName := range relations {
		// Use the provided context (ctx) when calling preloadRelations
		if err := t.preloadRelations(ctx, modelSlice, relationName); err != nil {
			// Stop on the first error
			return fmt.Errorf("failed to load relation '%s': %w", relationName, err)
		}
	}

	return nil
}

// ByIDs retrieves multiple records by their primary keys and optionally preloads relations.
func (t *Thing[T]) ByIDs(ids []int64, preloads ...string) (map[int64]*T, error) {
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	// REMOVED TTLs from call
	resultsMapReflect, err := fetchModelsByIDsInternal(t.ctx, t.cache, t.db, t.info, modelType, ids)
	if err != nil {
		return nil, fmt.Errorf("ByIDs failed during internal fetch: %w", err)
	}

	// Convert map[int64]reflect.Value (containing *T) to map[int64]*T
	resultsMapTyped := make(map[int64]*T, len(resultsMapReflect))
	// Also collect results in a slice for preloading
	resultsSliceForPreload := make([]*T, 0, len(resultsMapReflect))
	for id, modelVal := range resultsMapReflect {
		if typedModel, ok := modelVal.Interface().(*T); ok {
			resultsMapTyped[id] = typedModel
			resultsSliceForPreload = append(resultsSliceForPreload, typedModel)
		} else {
			log.Printf("WARN: ByIDs: Could not assert type for ID %d", id)
		}
	}

	// Apply preloads if requested
	if len(preloads) > 0 && len(resultsSliceForPreload) > 0 {
		for _, preloadName := range preloads {
			if preloadErr := t.preloadRelations(t.ctx, resultsSliceForPreload, preloadName); preloadErr != nil {
				// Log error but return results obtained so far
				log.Printf("WARN: ByIDs: failed to apply preload '%s': %v", preloadName, preloadErr)
				// Optionally return error: return nil, fmt.Errorf("failed to apply preload '%s': %w", preloadName, preloadErr)
			}
		}
	}

	return resultsMapTyped, nil
}

// ByID retrieves a single record of type T by its primary key.
func (t *Thing[T]) ByID(id int64) (*T, error) {
	instance := new(T)
	err := t.byIDInternal(t.ctx, id, instance) // Calls internal method
	if err != nil {
		return nil, err
	}
	return instance, nil
}

// Save updates an existing record or creates a new one if its primary key is zero
// or it's explicitly marked as new. Also updates timestamps.
func (t *Thing[T]) Save(value *T) error {
	// Delegate directly to the internal save logic
	return t.saveInternal(t.ctx, value) // Calls internal method
}

// Delete removes a record of type T.
func (t *Thing[T]) Delete(value *T) error {
	return t.deleteInternal(t.ctx, value) // Calls internal method
}

// SoftDelete marks a record as deleted by setting the Deleted flag and updating
// the UpdatedAt timestamp. It then saves the changes.
func (t *Thing[T]) SoftDelete(model *T) error {
	if model == nil {
		return errors.New("cannot soft delete a nil model")
	}

	baseModelPtr := getBaseModelPtr(model)
	if baseModelPtr == nil {
		// This case might be less likely if model is *T, but good practice
		return errors.New("SoftDelete: model must embed BaseModel")
	}

	// Set Deleted flag and update timestamp
	baseModelPtr.Deleted = true
	baseModelPtr.UpdatedAt = time.Now()

	// Use Save to persist changes and handle cache invalidation
	// Note: saveInternal triggers BeforeSave/AfterSave but not Before/AfterDelete
	if err := t.saveInternal(t.ctx, model); err != nil {
		// If save fails, attempt to revert the flags in memory?
		// baseModelPtr.Deleted = false // Maybe? Or rely on caller to retry?
		return fmt.Errorf("SoftDelete failed during save: %w", err)
	}

	return nil
}

// Query prepares a query based on QueryParams and returns a *CachedResult[T] for lazy execution.
// MOVED to query.go
// func (t *Thing[T]) Query(params QueryParams) (*CachedResult[T], error) {
// 	// TODO: Add validation for params if necessary?
// 	return &CachedResult[T]{
// 		thing:  t,
// 		params: params,
// 		// cachedIDs, cachedCount, hasLoadedIDs, hasLoadedCount, hasLoadedAll, all initialized to zero values
// 	}, nil
// }

// Load explicitly loads relationships for a given model instance using the Thing's context.
// model must be a pointer to a struct of type T.
// relations are the string names of the fields representing the relationships to load.
func (t *Thing[T]) Load(model *T, relations ...string) error {
	// Use the context stored in the Thing instance
	return t.loadInternal(t.ctx, model, relations...)
}

// findChangedFieldsSimple is a simplified version of field comparison that focuses on
// common database column types rather than handling complex structures.
func findChangedFieldsSimple[T any](original, updated *T, info *ModelInfo) (map[string]interface{}, error) {
	changed := make(map[string]interface{})
	originalVal := reflect.ValueOf(original).Elem()
	updatedVal := reflect.ValueOf(updated).Elem()

	if originalVal.Type() != updatedVal.Type() {
		return nil, errors.New("original and updated values must be of the same type")
	}

	// Get the UpdatedAt field name for special handling later
	var updatedAtDBColName string
	updatedAtGoFieldName := "UpdatedAt"
	if col, ok := info.FieldToColumnMap[updatedAtGoFieldName]; ok {
		updatedAtDBColName = col
	}

	// Only enable detailed debug logging if DEBUG_DIFF environment variable is set
	debugEnabled := os.Getenv("DEBUG_DIFF") != ""

	// Use compareFields from cached metadata for efficient field comparison
	for _, field := range info.CompareFields {
		// Use the pre-computed field index for fast access
		originalFieldVal := originalVal.FieldByIndex(field.Index)
		updatedFieldVal := updatedVal.FieldByIndex(field.Index)

		if !originalFieldVal.IsValid() || !updatedFieldVal.IsValid() {
			if debugEnabled {
				log.Printf("DEBUG: Field not valid, skipping: %s", field.GoName)
			}
			continue
		}

		// Simple comparison based on field type
		var fieldsEqual bool

		// Special handling for time.Time - this is common in DB models
		if field.Type == reflect.TypeOf(time.Time{}) {
			if !originalFieldVal.IsZero() && !updatedFieldVal.IsZero() {
				oTime := originalFieldVal.Interface().(time.Time)
				uTime := updatedFieldVal.Interface().(time.Time)
				fieldsEqual = oTime.Equal(uTime)
			} else {
				fieldsEqual = originalFieldVal.IsZero() == updatedFieldVal.IsZero()
			}
		} else {
			// For basic types, use simple equality check
			// This works for strings, numbers, booleans and other basic types
			fieldsEqual = originalFieldVal.Interface() == updatedFieldVal.Interface()
		}

		if !fieldsEqual {
			if debugEnabled {
				log.Printf("DEBUG: Change detected in %s: %v -> %v",
					field.GoName, originalFieldVal.Interface(), updatedFieldVal.Interface())
			}
			// Add the changed field to the result using its DB column name
			changed[field.DBColumn] = updatedFieldVal.Interface()
		}
	}

	// Handle UpdatedAt field specially
	if updatedAtDBColName != "" && len(changed) > 0 {
		if len(changed) == 1 {
			if _, isOnlyChange := changed[updatedAtDBColName]; isOnlyChange {
				if debugEnabled {
					log.Printf("DEBUG: Removing '%s' from changed map as it's the only change.", updatedAtDBColName)
				}
				delete(changed, updatedAtDBColName)
			}
		} else {
			// If UpdatedAt changed alongside other fields, ensure it's using the latest timestamp
			if _, ok := changed[updatedAtDBColName]; ok {
				// If UpdatedAt was detected as changed, ensure we use the *actual* updated value
				updatedFieldVal := updatedVal.FieldByName(updatedAtGoFieldName)
				if updatedFieldVal.IsValid() {
					changed[updatedAtDBColName] = updatedFieldVal.Interface()
					if debugEnabled {
						log.Printf("DEBUG: Ensuring '%s' uses the latest timestamp value in changed map.", updatedAtDBColName)
					}
				}
			}
		}
	}

	return changed, nil
}

// --- Incremental Cache Update Methods --- // MOVED to cache.go

// // updateAffectedQueryCaches is called after a Save operation
// // to incrementally update relevant list and count caches.
// // It's now a method of Thing[T].
// func (t *Thing[T]) updateAffectedQueryCaches(ctx context.Context, model *T, originalModel *T, isCreate bool) {
// ... (rest of updateAffectedQueryCaches code omitted)
// }

// // handleDeleteInQueryCaches is called after a Delete operation
// // to incrementally update relevant list and count caches by removing the deleted item.
// // It's now a method of Thing[T].
// func (t *Thing[T]) handleDeleteInQueryCaches(ctx context.Context, model *T) {
// ... (rest of handleDeleteInQueryCaches code omitted)
// }

// --- Helper functions for list/count cache --- // MOSTLY MOVED to cache.go

// // containsID checks if an ID exists in a slice of int64.
// // ... (rest of containsID)
// func containsID(ids []int64, id int64) bool {
// 	for _, currentID := range ids {
// 		if currentID == id {
// 			return true
// 		}
// 	}
// 	return false
// }

// // checkModelMatchAgainstQuery checks if a model matches query params.
// // ... (rest of checkModelMatchAgainstQuery)
// func (t *Thing[T]) checkModelMatchAgainstQuery(model *T, originalModel *T, params QueryParams, isCreate bool) (bool, bool, error) {
// 	var matchesCurrent, matchesOriginal bool
// 	var matchErr error
//
// 	// Check current model state
// 	matchesCurrent, matchErr = t.CheckQueryMatch(model, params) // CheckQueryMatch is defined in query_match.go
// 	if matchErr != nil {
// 		return false, false, fmt.Errorf("error checking query match for current model: %w", matchErr)
// 	}
//
// 	// Check original model state (only relevant for updates)
// 	if !isCreate && originalModel != nil {
// 		matchesOriginal, matchErr = t.CheckQueryMatch(originalModel, params) // CheckQueryMatch is defined in query_match.go
// 		if matchErr != nil {
// 			return false, false, fmt.Errorf("error checking query match for original model: %w", matchErr)
// 		}
// 	}
// 	return matchesCurrent, matchesOriginal, nil
// }

// // determineCacheAction determines cache add/remove actions.
// // ... (rest of determineCacheAction)
// func determineCacheAction(isCreate, matchesOriginal, matchesCurrent bool, isKept bool) (bool, bool) {
// 	needsAdd := false
// 	needsRemove := false
//
// 	if !isKept {
// 		// If item is soft-deleted, it always needs removal (if it was previously matching)
// 		// and never needs adding.
// 		log.Printf("DEBUG Determine Action: Model is soft-deleted (KeepItem=false). Ensuring removal.")
// 		needsRemove = true // Ensure removal attempt
// 		needsAdd = false
// 	} else if isCreate {
// 		if matchesCurrent {
// 			needsAdd = true
// 			log.Printf("DEBUG Determine Action: Create matches query. Needs Add.")
// 		}
// 	} else { // Update
// 		if matchesCurrent && !matchesOriginal {
// 			needsAdd = true
// 			log.Printf("DEBUG Determine Action: Update now matches query (didn't before). Needs Add.")
// 		} else if !matchesCurrent && matchesOriginal {
// 			needsRemove = true
// 			log.Printf("DEBUG Determine Action: Update no longer matches query (did before). Needs Remove.")
// 		}
// 		// If match status didn't change (both true or both false), neither add nor remove is needed.
// 	}
//
// 	// If it needs adding, it cannot simultaneously need removing based on match status change.
// 	if needsAdd {
// 		needsRemove = false
// 	}
//
// 	return needsAdd, needsRemove
// }

// --- Cache Helpers --- // MOVED to cache.go

// // updateAffectedQueryCaches is called after a Save operation
// // to incrementally update relevant list and count caches.
// // It's now a method of Thing[T].
// func (t *Thing[T]) updateAffectedQueryCaches(ctx context.Context, model *T, originalModel *T, isCreate bool) {
// ... (rest of updateAffectedQueryCaches code omitted)
// }

// // handleDeleteInQueryCaches is called after a Delete operation
// // to incrementally update relevant list and count caches by removing the deleted item.
// // It's now a method of Thing[T].
// func (t *Thing[T]) handleDeleteInQueryCaches(ctx context.Context, model *T) {
// ... (rest of handleDeleteInQueryCaches code omitted)
// }

// --- ClearCacheByID MOVED to cache.go ---
// func (t *Thing[T]) ClearCacheByID(ctx context.Context, id int64) error {
// ... (rest of ClearCacheByID code omitted)
// }
