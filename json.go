package thing

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/burugo/thing/internal/utils"
)

// Helper function to check if a string slice contains a string (case-insensitive)
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, item) { // Use EqualFold for case-insensitive check
			return true
		}
	}
	return false
}

// Helper: snake_case/小写转导出方法名
func exportedName(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// --- JSON Serialization Options ---

// FieldRule represents a single included field and its potential nested options.
type FieldRule struct {
	Name   string
	Nested *JSONOptions // Nested options for this specific field
}

// JSONOptions holds the options for JSON serialization.
type JSONOptions struct {
	OrderedInclude []*FieldRule            // Ordered list of fields to include (with possible nested rules)
	OrderedExclude []string                // Ordered list of fields to exclude
	NestedRules    map[string]*JSONOptions // Stores nested rules for any field, regardless of include/exclude status
}

// newJSONOptions creates a default empty options struct.
func newJSONOptions() *JSONOptions {
	return &JSONOptions{
		OrderedInclude: make([]*FieldRule, 0),
		OrderedExclude: make([]string, 0),
		NestedRules:    make(map[string]*JSONOptions),
	}
}

// JSONOption defines the function signature for JSON serialization options.
type JSONOption func(*JSONOptions)

// Include specifies fields to include in the JSON output (now supports nested DSL via WithFields logic).
func Include(fields ...string) JSONOption {
	return func(opts *JSONOptions) {
		if len(fields) == 0 {
			return
		}
		// Build DSL string: e.g., "books{title},name"
		dsl := strings.Join(fields, ",")
		parsedOpts, err := ParseFieldsDSL(dsl)
		if err != nil {
			return
		}
		mergeJSONOptions(opts, parsedOpts)
	}
}

// Exclude specifies fields to exclude from the JSON output (now supports nested DSL via WithFields logic).
func Exclude(fields ...string) JSONOption {
	return func(opts *JSONOptions) {
		if len(fields) == 0 {
			return
		}
		// Build DSL string: e.g., "-books{id},-email"
		dslFields := make([]string, len(fields))
		for i, f := range fields {
			// If already starts with '-', don't double it
			if strings.HasPrefix(f, "-") {
				dslFields[i] = f
			} else {
				dslFields[i] = "-" + f
			}
		}
		dsl := strings.Join(dslFields, ",")
		parsedOpts, err := ParseFieldsDSL(dsl)
		if err != nil {
			return
		}
		mergeJSONOptions(opts, parsedOpts)
	}
}

// --- JSON Serialization Methods ---

// ToJSON serializes the provided model instance (single or collection) to JSON based on the given options.
func (t *Thing[T]) ToJSON(model interface{}, opts ...JSONOption) ([]byte, error) {
	if model == nil {
		return nil, errors.New("cannot serialize nil model")
	}

	options := newJSONOptions()
	for _, opt := range opts {
		opt(options)
	}

	val := reflect.ValueOf(model)
	kind := val.Kind()

	// Handle pointer to single model
	if kind == reflect.Ptr {
		if val.IsNil() {
			return nil, errors.New("cannot serialize nil pointer")
		}
		val = val.Elem()
		kind = val.Kind()
	}

	// Handle slice/array of models
	if kind == reflect.Slice || kind == reflect.Array {
		result := make([]interface{}, val.Len())
		for i := 0; i < val.Len(); i++ {
			itemVal := val.Index(i)
			// If item is pointer, dereference
			if itemVal.Kind() == reflect.Ptr && !itemVal.IsNil() {
				itemVal = itemVal.Elem()
			}
			result[i] = t.serializeValue(itemVal, options)
		}
		return json.Marshal(result)
	}

	// Handle single struct
	if kind == reflect.Struct {
		jsonOutput := t.serializeValue(val, options)
		if om, ok := jsonOutput.(*utils.OrderedMap); ok {
			return json.Marshal(interface{}(om))
		}
		return json.Marshal(jsonOutput)
	}

	return nil, errors.New("ToJSON: unsupported type (must be struct, pointer to struct, or slice/array of struct/pointer)")
}

// serializeValue recursively serializes a reflect.Value based on provided jsonOptions.
func (t *Thing[T]) serializeValue(val reflect.Value, options *JSONOptions) interface{} {
	kind := val.Kind()

	// --- Handle Pointers ---
	if kind == reflect.Ptr {
		if val.IsNil() {
			return nil // Handle nil pointers
		}
		val = val.Elem() // Dereference the pointer
		kind = val.Kind()
	}

	// --- Handle Non-Containers ---
	// Handle primitive types and non-struct/slice/array directly
	if kind != reflect.Struct && kind != reflect.Slice && kind != reflect.Array {
		if val.IsValid() {
			return val.Interface()
		}
		return nil // Return nil for invalid primitive values
	}

	// --- Handle Slices and Arrays ---
	if kind == reflect.Slice || kind == reflect.Array {
		// Serialize a slice or array of nested models
		nestedArray := make([]interface{}, val.Len())
		// Use the current 'options' for all elements in the slice/array.
		// This 'options' parameter already holds the correct nested rules
		// determined by the caller (struct serialization logic).
		for k := 0; k < val.Len(); k++ {
			elemVal := val.Index(k)
			// Pass the 'options' received by this function call
			nestedArray[k] = t.serializeValue(elemVal, options)
		}
		return nestedArray
	}

	// --- Handle Structs ---
	if kind == reflect.Struct {
		// Special case: time.Time should be marshaled as string
		if val.Type().PkgPath() == "time" && val.Type().Name() == "Time" {
			return val.Interface()
		}

		typ := val.Type()
		var fieldsToProcess []*FieldRule

		// 判断 OrderedInclude 是否只包含 id
		useInclude := false
		onlyID := false
		if options != nil && len(options.OrderedInclude) > 0 {
			useInclude = false
			onlyID = true
			for _, rule := range options.OrderedInclude {
				if !strings.EqualFold(rule.Name, "id") {
					useInclude = true
					onlyID = false
					break
				}
			}
		}
		if useInclude || (onlyID && len(options.OrderedExclude) == 0) {
			// --- Include Mode 或极简 id-only 模式 ---
			fieldsToProcess = options.OrderedInclude
		} else {
			// --- Fallback Mode (No explicit includes, or only id present) ---
			fieldsToProcess = make([]*FieldRule, 0, typ.NumField())
			processedFieldNames := make(map[string]bool) // Track processed Go field names

			var processStructFields func(currentVal reflect.Value, currentTyp reflect.Type)
			processStructFields = func(currentVal reflect.Value, currentTyp reflect.Type) {
				for i := 0; i < currentTyp.NumField(); i++ {
					fieldTyp := currentTyp.Field(i)
					fieldName := fieldTyp.Name // Go field name

					// Handle embedded structs recursively first
					if fieldTyp.Anonymous && fieldTyp.Type.Kind() == reflect.Struct {
						if currentVal.IsValid() && currentVal.Kind() == reflect.Struct {
							processStructFields(currentVal.Field(i), fieldTyp.Type)
						}
						continue
					}

					if processedFieldNames[fieldName] {
						continue // Already processed (likely from embedded struct)
					}

					// Get JSON tag info
					jsonTag := fieldTyp.Tag.Get("json")
					jsonTagParts := strings.Split(jsonTag, ",")
					jsonFieldName := jsonTagParts[0]
					if jsonFieldName == "" {
						jsonFieldName = fieldName // Use Go name if json tag name is empty
					}

					// 1. Skip if excluded by json:"-" tag
					if jsonFieldName == "-" {
						processedFieldNames[fieldName] = true
						continue
					}

					// 2. Skip if explicitly excluded by top-level Exclude option (using JSON name)
					if options != nil && contains(options.OrderedExclude, jsonFieldName) {
						processedFieldNames[fieldName] = true
						continue
					}

					// 3. Add the field (since it wasn't excluded). Nested rules applied later in processing loop.
					fieldsToProcess = append(fieldsToProcess, &FieldRule{Name: fieldName, Nested: nil})
					processedFieldNames[fieldName] = true
				}
			}
			processStructFields(val, typ) // Start processing from the top-level type
		}

		// --- Process fields in the determined order ---
		ordered := utils.NewOrderedMap()

		// Helper to find field recursively
		var findField func(currentVal reflect.Value, currentTyp reflect.Type, targetName string) (reflect.StructField, reflect.Value, bool)
		findField = func(currentVal reflect.Value, currentTyp reflect.Type, targetName string) (reflect.StructField, reflect.Value, bool) {
			if !currentVal.IsValid() { // Safety check for invalid values (e.g., nil embedded pointers)
				return reflect.StructField{}, reflect.Value{}, false
			}
			snakeName := utils.ToSnakeCase(targetName)
			camelName := exportedName(snakeName) // snake_case -> CamelCase
			for i := 0; i < currentTyp.NumField(); i++ {
				structField := currentTyp.Field(i)
				// 先用 json tag 匹配
				jsonTag := structField.Tag.Get("json")
				jsonTagName := strings.Split(jsonTag, ",")[0]
				if jsonTagName == "" {
					jsonTagName = structField.Name
				}
				if strings.EqualFold(jsonTagName, targetName) || strings.EqualFold(structField.Name, targetName) || strings.EqualFold(structField.Name, camelName) {
					// Ensure the containing struct value is valid before accessing the field
					if currentVal.Kind() == reflect.Struct {
						return structField, currentVal.Field(i), true
					}
					return reflect.StructField{}, reflect.Value{}, false // Cannot access field on non-struct
				}
				// Check anonymous embedded structs recursively
				if structField.Anonymous && structField.Type.Kind() == reflect.Struct {
					// Ensure the containing struct value is valid before accessing the embedded field
					if currentVal.Kind() == reflect.Struct {
						fField, fVal, fFound := findField(currentVal.Field(i), structField.Type, targetName)
						if fFound {
							return fField, fVal, true
						}
					}
				}
			}
			return reflect.StructField{}, reflect.Value{}, false
		}

		for _, rule := range fieldsToProcess {
			fieldName := rule.Name // DSL/Include 里的名字（snake_case）
			outKey := utils.ToSnakeCase(fieldName)

			// 优先查字段（json tag/snake_case/go字段名）
			field, fieldVal, found := findField(val, typ, fieldName)
			if found && field.IsExported() {
				// 字段存在，直接输出字段值（即使为零值）
				jsonTag := field.Tag.Get("json")
				if jsonTag != "" {
					tagParts := strings.Split(jsonTag, ",")
					if tagParts[0] != "" && tagParts[0] != "-" {
						outKey = tagParts[0]
					}
				}
				var optionsToPassDown *JSONOptions
				if rule.Nested != nil {
					optionsToPassDown = rule.Nested
				} else {
					var nestedOpts *JSONOptions
					definedNestedRules := false
					if options != nil && options.NestedRules != nil {
						var ok bool
						nestedOpts, ok = options.NestedRules[outKey]
						definedNestedRules = ok
					}
					optionsToPassDown = nestedOpts
					if !definedNestedRules && options != nil && options.NestedRules != nil {
						if nested, ok := options.NestedRules[fieldName]; ok {
							optionsToPassDown = nested
						}
					}
					if optionsToPassDown == nil {
						if options != nil && len(options.OrderedInclude) == 0 {
							optionsToPassDown = options
						} else {
							optionsToPassDown = newJSONOptions()
						}
					}
					// 合成 exclude 规则（如 books{id}）
					if options != nil && options.NestedRules != nil {
						if nestedRule, ok := options.NestedRules[outKey]; ok && nestedRule != nil {
							for _, ex := range nestedRule.OrderedExclude {
								if !contains(optionsToPassDown.OrderedExclude, ex) {
									optionsToPassDown.OrderedExclude = append(optionsToPassDown.OrderedExclude, ex)
								}
							}
						}
						if nestedRule, ok := options.NestedRules[fieldName]; ok && nestedRule != nil {
							for _, ex := range nestedRule.OrderedExclude {
								if !contains(optionsToPassDown.OrderedExclude, ex) {
									optionsToPassDown.OrderedExclude = append(optionsToPassDown.OrderedExclude, ex)
								}
							}
						}
					}
				}
				switch fieldVal.Kind() {
				case reflect.Struct, reflect.Ptr:
					valToSet := t.serializeValue(fieldVal, optionsToPassDown)
					ordered.Set(outKey, valToSet)
				case reflect.Slice, reflect.Array:
					nestedArray := make([]interface{}, fieldVal.Len())
					for k := 0; k < fieldVal.Len(); k++ {
						elemVal := fieldVal.Index(k)
						nestedArray[k] = t.serializeValue(elemVal, optionsToPassDown)
					}
					ordered.Set(outKey, nestedArray)
				default:
					ordered.Set(outKey, fieldVal.Interface())
				}
				continue // 字段已处理，跳过虚拟方法
			}
			// 字段不存在，查虚拟方法
			var method reflect.Value
			if val.CanAddr() {
				method = val.Addr().MethodByName(exportedName(fieldName))
			} else {
				method = val.MethodByName(exportedName(fieldName))
			}
			if method.IsValid() && method.Type().NumIn() == 0 && method.Type().NumOut() == 1 {
				result := method.Call(nil)[0].Interface()
				ordered.Set(outKey, result)
			} else {
				ordered.Set(outKey, nil)
			}
		}

		return ordered
	} // end if kind == reflect.Struct

	return nil // Should not reach here for supported types
}

// --- FieldRuleTree (REMOVED) ---
// // FieldRuleTree represents the include/exclude/nested rules for JSON field control.
// type FieldRuleTree struct {
// 	OrderedInclude []*FieldRule // Ordered list of fields to include (with possible nested rules)
// 	OrderedExclude []string     // Ordered list of fields to exclude
// }

// ParseFieldsDSL parses a DSL string and returns populated jsonOptions representing the rules.
func ParseFieldsDSL(dsl string) (*JSONOptions, error) {
	// Enhanced parser: handle top-level and nested include/exclude fields

	var parse func(input string) (*JSONOptions, error)
	parse = func(input string) (*JSONOptions, error) {
		currentOpts := newJSONOptions()
		var i int
		for i < len(input) {
			// Skip whitespace and leading comma
			for i < len(input) && (input[i] == ' ' || input[i] == ',') {
				i++
			}
			if i >= len(input) { // Handle trailing comma
				break
			}
			// Parse field name (may start with '-')
			start := i
			for i < len(input) && input[i] != ',' && input[i] != '{' && input[i] != '}' {
				i++
			}
			field := strings.TrimSpace(input[start:i])

			if field == "" { // Should not happen with leading comma/whitespace skip, but for safety
				continue
			}

			// Check for nested
			switch {
			case i < len(input) && input[i] == '{':
				// Find matching '}'
				braceCount := 1
				nestStart := i + 1
				nestEnd := nestStart
				for nestEnd < len(input) && braceCount > 0 {
					if input[nestEnd] == '{' {
						braceCount++
					} else if input[nestEnd] == '}' {
						braceCount--
					}
					nestEnd++
				}

				if braceCount != 0 {
					return nil, errors.New("unmatched '{' in DSL")
				}

				nestedContent := input[nestStart : nestEnd-1]
				// Recursively parse nested content to get nested jsonOptions
				nestedOpts, err := parse(nestedContent)
				if err != nil {
					return nil, err
				}

				// Determine the name (remove leading '-' if present)
				name := field
				isExclude := false
				if strings.HasPrefix(name, "-") {
					name = strings.TrimPrefix(name, "-")
					isExclude = true
				}

				// Store nested rules regardless of include/exclude status
				if name != "" {
					if _, exists := currentOpts.NestedRules[name]; !exists {
						currentOpts.NestedRules[name] = nestedOpts
					} else {
						// DEBUG: 跳过重复嵌套
					}
				}
				if isExclude {
					if name != "" && !contains(currentOpts.OrderedExclude, name) {
						currentOpts.OrderedExclude = append(currentOpts.OrderedExclude, name)
					} else {
						// Do NOT add to OrderedInclude for top-level excluded fields with nested rules
					}
				} else {
					// If the field is included (e.g., books{...}), add to include list with nested rules
					found := false
					for _, existingRule := range currentOpts.OrderedInclude {
						if existingRule.Name == name {
							found = true
							break
						}
					}
					if !found {
						currentOpts.OrderedInclude = append(currentOpts.OrderedInclude, &FieldRule{
							Name:   name,
							Nested: nestedOpts, // Assign the parsed nested options
						})
					}
				}
				i = nestEnd // Move past '}'
			default:
				if strings.HasPrefix(field, "-") {
					name := strings.TrimPrefix(field, "-")
					if name != "" && !contains(currentOpts.OrderedExclude, name) {
						currentOpts.OrderedExclude = append(currentOpts.OrderedExclude, name)
					} else {
						// DEBUG: 跳过重复 exclude
					}
				} else {
					found := false
					for _, existingRule := range currentOpts.OrderedInclude {
						if existingRule.Name == field {
							found = true
							break
						}
					}
					if !found {
						currentOpts.OrderedInclude = append(currentOpts.OrderedInclude, &FieldRule{
							Name:   field,
							Nested: nil, // No nested rules for simple fields
						})
					}
				}
			}

			// Skip trailing comma after processing field or nested block
			for i < len(input) && (input[i] == ' ' || input[i] == ',') {
				i++
			}
		}
		return currentOpts, nil
	}

	// Top-level parsing
	rootOpts, err := parse(dsl)
	if err != nil {
		return nil, err
	}

	return rootOpts, nil
}

// mergeJSONOptions merges src options into dst options recursively.
// Note: This merge logic might need refinement based on desired behavior for overlapping rules.
// Current: Appends includes if name doesn't exist, merges nested if name exists. Appends unique excludes.
func mergeJSONOptions(dst, src *JSONOptions) {
	if dst == nil || src == nil {
		return
	}

	// Merge Includes (no nested merge: keep first occurrence only)
	dstIncludeMap := make(map[string]*FieldRule)
	for _, rule := range dst.OrderedInclude {
		dstIncludeMap[rule.Name] = rule
	}

	for _, srcRule := range src.OrderedInclude {
		if _, ok := dstIncludeMap[srcRule.Name]; ok {
			// Name exists, skip (do not merge nested, do not override)
			continue
		} else {
			// Name does not exist, append
			dst.OrderedInclude = append(dst.OrderedInclude, srcRule)
			dstIncludeMap[srcRule.Name] = srcRule
		}
	}

	// Remove duplicate id in OrderedInclude, keep only the first one (should always be at the front)
	idSeen := false
	newOrdered := make([]*FieldRule, 0, len(dst.OrderedInclude))
	seen := make(map[string]bool)
	for _, rule := range dst.OrderedInclude {
		if strings.EqualFold(rule.Name, "id") {
			if !idSeen {
				newOrdered = append(newOrdered, rule)
				idSeen = true
				seen[strings.ToLower(rule.Name)] = true
			}
		} else {
			if !seen[strings.ToLower(rule.Name)] {
				newOrdered = append(newOrdered, rule)
				seen[strings.ToLower(rule.Name)] = true
			}
		}
	}
	dst.OrderedInclude = newOrdered

	// Merge Excludes
	dstExcludeMap := make(map[string]bool)
	for _, excludedName := range dst.OrderedExclude {
		dstExcludeMap[excludedName] = true
	}

	for _, srcExcludedName := range src.OrderedExclude {
		if _, ok := dstExcludeMap[srcExcludedName]; !ok {
			dst.OrderedExclude = append(dst.OrderedExclude, srcExcludedName)
			dstExcludeMap[srcExcludedName] = true
		}
	}
}

// WithFields specifies fields to include/exclude using a DSL string (supports nested, exclude, etc.).
func WithFields(dsl string) JSONOption {
	return withFieldsDSL(dsl)
}

// withFieldsDSL is the internal implementation for parsing the DSL string.
func withFieldsDSL(dsl string) JSONOption {
	return func(opts *JSONOptions) {
		parsedOpts, err := ParseFieldsDSL(dsl)
		if err != nil {
			// If DSL is invalid, do nothing (or could panic/log)
			return
		}
		// Merge parsed DSL options into existing options
		mergeJSONOptions(opts, parsedOpts)
	}
}
