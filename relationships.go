package thing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/burugo/thing/common"
	"github.com/burugo/thing/internal/cache"
	"github.com/burugo/thing/internal/schema"
	"github.com/burugo/thing/internal/types"
)

// --- Relationship Loading ---

// RelationshipOpts defines the configuration for a relationship based on struct tags.
type RelationshipOpts struct {
	RelationType   string // "belongsTo", "hasMany", "manyToMany"
	ForeignKey     string // FK field name in the *owning* struct (for belongsTo) or *related* struct (for hasMany)
	LocalKey       string // PK field name in the *owning* struct (defaults to info.pkName)
	RelatedModel   string // Optional: Specify related model name if different from field type
	JoinTable      string // For manyToMany: join table name
	JoinLocalKey   string // For manyToMany: join table column for local model
	JoinRelatedKey string // For manyToMany: join table column for related model
}

// parseThingTag parses the `thing:"..."` struct tag.
func parseThingTag(tag string) (opts RelationshipOpts, err error) {
	if tag == "" {
		err = errors.New("empty thing tag")
		return
	}

	parts := strings.Split(tag, ";")
	if len(parts) == 0 {
		err = errors.New("invalid thing tag format")
		return
	}

	opts.RelationType = parts[0]
	if opts.RelationType != "belongsTo" && opts.RelationType != "hasMany" && opts.RelationType != "manyToMany" {
		err = fmt.Errorf("unsupported relation type in thing tag: %s", opts.RelationType)
		return
	}

	for _, part := range parts[1:] {
		keyValue := strings.SplitN(part, ":", 2)
		if len(keyValue) != 2 {
			err = fmt.Errorf("invalid key-value pair in thing tag: %s", part)
			return
		}
		key := strings.TrimSpace(keyValue[0])
		value := strings.TrimSpace(keyValue[1])

		switch key {
		case "fk":
			opts.ForeignKey = value
		case "foreignKey":
			opts.ForeignKey = value
		case "localKey":
			opts.LocalKey = value
		case "model":
			opts.RelatedModel = value
		case "relatedModel":
			opts.RelatedModel = value
		case "joinTable":
			opts.JoinTable = value
		case "joinLocalKey":
			opts.JoinLocalKey = value
		case "joinRelatedKey":
			opts.JoinRelatedKey = value
		default:
			err = fmt.Errorf("unknown key in thing tag: %s", key)
			return
		}
	}

	// Basic validation
	if opts.ForeignKey == "" {
		err = fmt.Errorf("missing 'fk' (foreignKey) in thing tag for type %s", opts.RelationType)
		return
	}

	// For manyToMany, check joinTable, joinLocalKey, joinRelatedKey
	if opts.RelationType == "manyToMany" {
		if opts.JoinTable == "" || opts.JoinLocalKey == "" || opts.JoinRelatedKey == "" {
			err = fmt.Errorf("missing joinTable/joinLocalKey/joinRelatedKey in thing tag for manyToMany relation")
			return
		}
	}

	return
}

// preloadRelations handles the actual preloading logic based on parsed opts.
func (t *Thing[T]) preloadRelations(ctx context.Context, results []T, preloadName string) error {
	if len(results) == 0 {
		return nil // Nothing to preload
	}

	// Always use the struct type (not pointer) for FieldByName to avoid panic
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	field, ok := modelType.FieldByName(preloadName)
	if !ok {
		return fmt.Errorf("field '%s' not found in model %s", preloadName, modelType.Name())
	}

	thingTag := field.Tag.Get("thing")   // Get the 'thing' tag
	opts, err := parseThingTag(thingTag) // Call the renamed parsing function
	if err != nil {
		return fmt.Errorf("failed to parse thing tag for field %s: %w", preloadName, err)
	}

	// Default LocalKey if not specified
	if opts.LocalKey == "" {
		opts.LocalKey = t.info.PkName // Use the cached primary key column name
		log.Printf("DEBUG: Using default local key '%s' for relation '%s'", opts.LocalKey, preloadName)
	}

	// --- Reflection fix: ensure resultsVal is a slice of pointers to struct ---
	resultsVal := reflect.ValueOf(results)
	if resultsVal.Kind() != reflect.Slice {
		return fmt.Errorf("preloadRelations: results is not a slice (got %s)", resultsVal.Kind())
	}
	if resultsVal.Len() == 0 {
		return nil // Nothing to preload
	}
	firstElem := resultsVal.Index(0)
	if firstElem.Kind() != reflect.Ptr || firstElem.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("preloadRelations: elements in results must be pointers to struct, got %s", firstElem.Kind())
	}

	switch opts.RelationType {
	case "belongsTo":
		return t.preloadBelongsTo(ctx, resultsVal, field, opts)
	case "hasMany":
		return t.preloadHasMany(ctx, resultsVal, field, opts)
	case "manyToMany":
		return t.preloadManyToMany(ctx, resultsVal, field, opts)
	default:
		return fmt.Errorf("internal error: unsupported relation type %s in preloadRelations", opts.RelationType)
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
	relatedInfo, err := schema.GetCachedModelInfo(relatedModelType)
	if err != nil {
		return fmt.Errorf("failed to get model info for related type %s: %w", relatedModelType.Name(), err)
	}

	relatedPtrType := relatedModelType
	if relatedPtrType.Kind() != reflect.Ptr {
		relatedPtrType = reflect.PtrTo(relatedModelType)
	}
	relatedMap, err := fetchModelsByIDsInternal(ctx, t.cache, t.db, relatedInfo, relatedPtrType, uniqueFkList)
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
	switch {
	case relatedElemType.Kind() == reflect.Ptr && relatedElemType.Elem().Kind() == reflect.Struct:
		relatedModelType = relatedElemType.Elem() // Type R (e.g., Comment)
		relatedIsSliceOfPtr = true
	case relatedElemType.Kind() == reflect.Struct:
		relatedModelType = relatedElemType // Type R (e.g., Comment)
		relatedIsSliceOfPtr = false
	default:
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
	relatedInfo, err := schema.GetCachedModelInfo(relatedModelType)
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
		} else {
			return fmt.Errorf("foreign key column '%s' (from tag 'fk') not found in related model %s info or as a direct field name", relatedFkColName, relatedModelType.Name())
		}
	}
	log.Printf("Foreign Key Field in Related Model (%s): %s (DB: %s)", relatedModelType.Name(), relatedFkGoFieldName, relatedFkColName)

	// Prepare query params for fetching related model IDs
	placeholders := strings.Repeat("?,", len(uniqueLkList))[:len(uniqueLkList)*2-1]
	relatedIDParams := types.QueryParams{
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

			switch {
			case queryIDsErr == nil:
				// Cache hit with actual IDs
				log.Printf("CACHE HIT (Query IDs): Found %d related IDs for key %s", len(cachedIDs), listCacheKey)
				relatedIDs = cachedIDs
				cacheHit = true // Got the IDs from cache
			case errors.Is(queryIDsErr, common.ErrNotFound):
				// Cache miss
				log.Printf("CACHE MISS (Query IDs): Key %s not found.", listCacheKey)
				// cacheHit remains false
			default:
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
		idQuery := t.db.Builder().BuildSelectSQL(relatedInfo.TableName, []string{relatedInfo.PkName})
		if relatedIDParams.Where != "" {
			idQuery = idQuery + " WHERE " + relatedIDParams.Where
		}

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
				if qcErr := t.cache.Set(ctx, listCacheKey, common.NoneResult, globalCacheTTL); qcErr != nil {
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
		// Always pass pointer type (*R) to fetchModelsByIDsInternal
		relatedPtrType := reflect.PtrTo(relatedModelType)
		if relatedPtrType.Kind() != reflect.Ptr {
			return fmt.Errorf("preloadHasMany: relatedPtrType must be a pointer type, got %s", relatedPtrType.Kind())
		}
		relatedModelsMap, err = fetchModelsByIDsInternal(ctx, t.cache, t.db, relatedInfo, relatedPtrType, relatedIDs)
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

	// 注册值级别索引（如 user_roles:13）
	if cache.GlobalCacheIndex != nil {
		relatedTable := relatedInfo.TableName
		for lk := range localKeyValues {
			params := types.QueryParams{
				Where: fmt.Sprintf("%s = ?", relatedFkColName),
				Args:  []interface{}{lk},
			}
			cacheKey := fmt.Sprintf("%s:%v", relatedTable, lk)
			cache.GlobalCacheIndex.RegisterQuery(relatedTable, cacheKey, params)
		}
	}

	log.Printf("Successfully preloaded HasMany relation '%s'", field.Name)
	return nil
}

// preloadManyToMany handles eager loading for ManyToMany relationships.
func (t *Thing[T]) preloadManyToMany(ctx context.Context, resultsVal reflect.Value, field reflect.StructField, opts RelationshipOpts) error {
	// 1. 类型检查
	relatedFieldType := field.Type // e.g., []Role 或 []*Role
	if relatedFieldType.Kind() != reflect.Slice {
		return fmt.Errorf("manyToMany field '%s' must be a slice, got %s", field.Name, relatedFieldType.String())
	}
	relatedElemType := relatedFieldType.Elem()
	var relatedModelType reflect.Type
	var relatedIsSliceOfPtr bool
	switch {
	case relatedElemType.Kind() == reflect.Ptr && relatedElemType.Elem().Kind() == reflect.Struct:
		relatedModelType = relatedElemType.Elem()
		relatedIsSliceOfPtr = true
	case relatedElemType.Kind() == reflect.Struct:
		relatedModelType = relatedElemType
		relatedIsSliceOfPtr = false
	default:
		return fmt.Errorf("manyToMany field '%s' must be a slice of structs or pointers to structs, got slice of %s", field.Name, relatedElemType.String())
	}

	// 2. 获取本地主键字段名
	localKeyColName := opts.LocalKey
	if localKeyColName == "" {
		localKeyColName = t.info.PkName
	}
	localKeyGoFieldName, ok := t.info.ColumnToFieldMap[localKeyColName]
	if !ok {
		return fmt.Errorf("local key column '%s' not found in model %s info", localKeyColName, resultsVal.Type().Elem().Elem().Name())
	}

	// 3. 收集所有本地ID
	localKeyValues := make([]interface{}, 0, resultsVal.Len())
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelElem := resultsVal.Index(i).Elem()
		lkFieldVal := owningModelElem.FieldByName(localKeyGoFieldName)
		if lkFieldVal.IsValid() {
			localKeyValues = append(localKeyValues, lkFieldVal.Interface())
		}
	}
	if len(localKeyValues) == 0 {
		return nil // 无需加载
	}

	// 4. 查询中间表，获取本地ID -> 关联ID列表
	joinTable := opts.JoinTable
	joinLocalKey := opts.JoinLocalKey
	joinRelatedKey := opts.JoinRelatedKey
	if joinTable == "" || joinLocalKey == "" || joinRelatedKey == "" {
		return fmt.Errorf("manyToMany preload: joinTable/joinLocalKey/joinRelatedKey must be set in tag")
	}

	// 构造缓存 key 前缀
	cacheKeyPrefix := fmt.Sprintf("%s:%s:", joinTable, joinLocalKey)

	relatedIDsMap := make(map[interface{}][]interface{}) // localID -> []relatedID
	allRelatedIDsSet := make(map[interface{}]struct{})
	for _, localID := range localKeyValues {
		cacheKey := fmt.Sprintf("%s%v", cacheKeyPrefix, localID)
		var ids []interface{}
		cacheHit := false
		if t.cache != nil {
			if getter, ok := t.cache.(interface {
				Get(ctx context.Context, key string) (string, error)
			}); ok {
				if val, err := getter.Get(ctx, cacheKey); err == nil {
					var idList []int64
					if jsonErr := json.Unmarshal([]byte(val), &idList); jsonErr == nil {
						for _, id := range idList {
							ids = append(ids, id)
						}
						cacheHit = true
					}
				}
			}
		}
		if !cacheHit {
			// 查 DB
			query := fmt.Sprintf(`SELECT "%s" FROM "%s" WHERE "%s" = ?`, joinRelatedKey, joinTable, joinLocalKey)
			rows, err := t.db.DB().QueryContext(ctx, query, localID)
			if err != nil {
				return err
			}
			var idList []int64
			for rows.Next() {
				var rid int64
				if err := rows.Scan(&rid); err == nil {
					ids = append(ids, rid)
					idList = append(idList, rid)
					allRelatedIDsSet[rid] = struct{}{}
				}
			}
			_ = rows.Close()
			// 写入缓存
			if t.cache != nil {
				if setter, ok := t.cache.(interface {
					Set(ctx context.Context, key string, value string, expiration time.Duration) error
				}); ok {
					if b, err := json.Marshal(idList); err == nil {
						_ = setter.Set(ctx, cacheKey, string(b), 0)
					}
				}
			}
		}
		// 注册值级别索引（如 user_roles:13）
		if cache.GlobalCacheIndex != nil {
			params := types.QueryParams{
				Where: fmt.Sprintf("%s = ?", joinLocalKey),
				Args:  []interface{}{localID},
			}
			cacheKey := fmt.Sprintf("%s:%v", joinTable, localID)
			cache.GlobalCacheIndex.RegisterQuery(joinTable, cacheKey, params)
		}
		relatedIDsMap[localID] = ids
	}

	// 5. 批量加载关联实体
	allRelatedIDs := make([]interface{}, 0, len(allRelatedIDsSet))
	for rid := range allRelatedIDsSet {
		allRelatedIDs = append(allRelatedIDs, rid)
	}
	// 转换为 int64 切片（假设主键为 int64）
	intIDs := make([]int64, 0, len(allRelatedIDs))
	for _, v := range allRelatedIDs {
		if id, ok := v.(int64); ok {
			intIDs = append(intIDs, id)
		} else if id, ok := v.(int); ok {
			intIDs = append(intIDs, int64(id))
		}
	}
	relatedInfo, err := schema.GetCachedModelInfo(relatedModelType)
	if err != nil {
		return fmt.Errorf("failed to get model info for related type %s: %w", relatedModelType.Name(), err)
	}
	relatedPtrType := relatedModelType
	if relatedPtrType.Kind() != reflect.Ptr {
		relatedPtrType = reflect.PtrTo(relatedModelType)
	}
	roleMap, err := fetchModelsByIDsInternal(ctx, t.cache, t.db, relatedInfo, relatedPtrType, intIDs)
	if err != nil {
		return fmt.Errorf("failed to fetch related models: %w", err)
	}

	// 6. 回填到原始对象
	for i := 0; i < resultsVal.Len(); i++ {
		owningModelPtr := resultsVal.Index(i)
		owningModelElem := owningModelPtr.Elem()
		lkFieldVal := owningModelElem.FieldByName(localKeyGoFieldName)
		if !lkFieldVal.IsValid() {
			continue
		}
		localID := lkFieldVal.Interface()
		relatedIDs := relatedIDsMap[localID]
		finalSlice := reflect.MakeSlice(relatedFieldType, 0, len(relatedIDs))
		for _, rid := range relatedIDs {
			var id64 int64
			if id, ok := rid.(int64); ok {
				id64 = id
			} else if id, ok := rid.(int); ok {
				id64 = int64(id)
			} else {
				continue
			}
			if roleVal, ok := roleMap[id64]; ok {
				if relatedIsSliceOfPtr {
					finalSlice = reflect.Append(finalSlice, roleVal)
				} else {
					finalSlice = reflect.Append(finalSlice, roleVal.Elem())
				}
			}
		}
		relationField := owningModelElem.FieldByName(field.Name)
		if relationField.IsValid() && relationField.CanSet() {
			relationField.Set(finalSlice)
		}
	}
	return nil
}

// loadInternal explicitly loads relationships for a given model instance using the provided context.
// This is the internal implementation called by the public Load method.
// model must be a pointer to a struct of type T.
// relations are the string names of the fields representing the relationships to load.
func (t *Thing[T]) loadInternal(ctx context.Context, model T, relations ...string) error {
	if reflect.ValueOf(model).IsNil() {
		return errors.New("model cannot be nil")
	}
	if len(relations) == 0 {
		return nil // Nothing to load
	}
	if t.info == nil { // Add check for safety
		return errors.New("loadInternal: model info not available on Thing instance")
	}

	// Wrap the single model in a slice to reuse the preloadRelations helper
	modelSlice := []T{model}

	for _, relationName := range relations {
		// Use the provided context (ctx) when calling preloadRelations
		if err := t.preloadRelations(ctx, modelSlice, relationName); err != nil {
			// Stop on the first error
			return fmt.Errorf("failed to load relation '%s': %w", relationName, err)
		}
	}

	return nil
}

// Load eagerly loads specified relationships for a given model instance.
func (t *Thing[T]) Load(model T, relations ...string) error {
	// Use the context stored in the Thing instance
	return t.loadInternal(t.ctx, model, relations...)
}

// NewThingByType creates a *Thing for a given model type (reflect.Type)
func NewThingByType(modelType reflect.Type, db DBAdapter, cache CacheClient) (interface{}, error) {
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	if modelType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("NewThingByType: modelType must be struct or *struct, got %s", modelType.Kind())
	}
	// 通过 reflect.New 创建 *Thing[Model]，用 interface{} 返回
	thingType := reflect.TypeOf(Thing[Model]{})
	thingPtr := reflect.New(thingType)
	thingVal := thingPtr.Elem()
	thingVal.FieldByName("db").Set(reflect.ValueOf(db))
	thingVal.FieldByName("cache").Set(reflect.ValueOf(cache))
	thingVal.FieldByName("ctx").Set(reflect.ValueOf(context.Background()))
	info, err := schema.GetCachedModelInfo(modelType)
	if err != nil {
		return nil, err
	}
	thingVal.FieldByName("info").Set(reflect.ValueOf(info))
	return thingPtr.Interface(), nil
}
