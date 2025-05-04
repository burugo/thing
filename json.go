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

// snakeToCamel converts snake_case to CamelCase (e.g., full_name -> FullName)
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
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
// It is intentionally not exported as it's an internal detail.
// Use functional options (e.g., WithFields, Include, Exclude) to configure JSON output.
// type jsonOptions struct {
// 	// The FieldRuleTree represents the parsed DSL and explicit includes/excludes.
// 	// This structure allows for recursive definition of rules for nested fields.
// 	RuleTree *FieldRuleTree
// 	// Include list from WithInclude (takes precedence over RuleTree includes)
// 	IncludeFields []string
// 	// Exclude list from WithExclude (takes precedence over RuleTree excludes)
// 	ExcludeFields []string
// }

// JSONOptions holds the options for JSON serialization.
type JSONOptions struct {
	OrderedInclude []*FieldRule // Ordered list of fields to include (with possible nested rules)
	OrderedExclude []string     // Ordered list of fields to exclude
}

// newJSONOptions creates a default empty options struct.
func newJSONOptions() *JSONOptions {
	return &JSONOptions{
		OrderedInclude: make([]*FieldRule, 0),
		OrderedExclude: make([]string, 0),
	}
}

// JSONOption defines the function signature for JSON serialization options.
type JSONOption func(*JSONOptions)

// Include specifies fields to include in the JSON output (simple, flat field list; no DSL features).
func Include(fields ...string) JSONOption {
	return func(opts *JSONOptions) {
		for _, field := range fields {
			// Avoid duplicates
			found := false
			for _, existingRule := range opts.OrderedInclude {
				if existingRule.Name == field {
					found = true
					break
				}
			}
			if !found {
				opts.OrderedInclude = append(opts.OrderedInclude, &FieldRule{Name: field, Nested: nil})
			}
		}
	}
}

// Exclude specifies fields to exclude from the JSON output.
func Exclude(fields ...string) JSONOption {
	return func(opts *JSONOptions) {
		for _, field := range fields {
			// Avoid duplicates
			if !contains(opts.OrderedExclude, field) {
				opts.OrderedExclude = append(opts.OrderedExclude, field)
			}
		}
	}
}

// --- JSON Serialization Methods ---

// ToJSON serializes the provided model instance to JSON based on the given options.
func (t *Thing[T]) ToJSON(model T, opts ...JSONOption) ([]byte, error) {
	if reflect.ValueOf(model).IsNil() {
		return nil, errors.New("cannot serialize nil model")
	}

	options := newJSONOptions()
	for _, opt := range opts {
		opt(options)
	}

	val := reflect.ValueOf(model).Elem() // model is *T, Elem() gives T
	// Pass the initial options to the recursive helper, indicating it's the top level
	jsonOutput := t.serializeValue(val, options)
	return json.Marshal(jsonOutput)
}

// serializeValue recursively serializes a reflect.Value based on provided jsonOptions and top-level status.
func (t *Thing[T]) serializeValue(val reflect.Value, options *JSONOptions) interface{} {
	kind := val.Kind()

	if kind == reflect.Ptr {
		if val.IsNil() {
			return nil // Handle nil pointers
		}
		val = val.Elem() // Dereference the pointer
		kind = val.Kind()
	}

	// Handle primitive types and non-struct/slice/array directly
	if kind != reflect.Struct && kind != reflect.Slice && kind != reflect.Array {
		return val.Interface()
	}

	if kind == reflect.Struct {
		// Special case: time.Time should be marshaled as string
		if val.Type().PkgPath() == "time" && val.Type().Name() == "Time" {
			return val.Interface()
		}
		ordered := utils.NewOrderedMap()
		typ := val.Type()

		// Determine fields to iterate: DSL order if options.orderedInclude exists, else all struct fields.
		var fieldsToProcess []*FieldRule // Change type to []*FieldRule
		if options != nil && len(options.OrderedInclude) > 0 {
			fieldsToProcess = options.OrderedInclude
			// Ensure id is prepended if not excluded and not already included
			idExcluded := options != nil && contains(options.OrderedExclude, "id")
			idIncluded := false
			for _, rule := range fieldsToProcess {
				if strings.EqualFold(rule.Name, "id") {
					idIncluded = true
					break
				}
			}
			if !idExcluded && !idIncluded {
				fieldsToProcess = append([]*FieldRule{{Name: "id", Nested: nil}}, fieldsToProcess...)
			}
		} else {
			// Fallback: Iterate all struct fields if no includes specified
			fieldsToProcess = make([]*FieldRule, 0, typ.NumField())
			for i := 0; i < typ.NumField(); i++ {
				fieldTyp := typ.Field(i)
				// Check if excluded by struct tag "-"
				jsonTag := fieldTyp.Tag.Get("json")
				if strings.Split(jsonTag, ",")[0] == "-" {
					continue
				}
				// Skip if explicitly excluded
				if options != nil && contains(options.OrderedExclude, fieldTyp.Name) {
					continue
				}
				// Handle anonymous embedded BaseModel fields
				if fieldTyp.Type == reflect.TypeOf(BaseModel{}) && fieldTyp.Anonymous {
					baseModelTyp := fieldTyp.Type
					for j := 0; j < baseModelTyp.NumField(); j++ {
						bmFieldTyp := baseModelTyp.Field(j)
						bmJsonTag := bmFieldTyp.Tag.Get("json")
						jsonFieldName := bmFieldTyp.Name
						if bmJsonTag != "" {
							tagParts := strings.Split(bmJsonTag, ",")
							if tagParts[0] != "" && tagParts[0] != "-" {
								jsonFieldName = tagParts[0]
							}
						}
						if strings.Split(bmJsonTag, ",")[0] != "-" {
							// Skip if explicitly excluded (by JSON field name)
							if options != nil && contains(options.OrderedExclude, jsonFieldName) {
								continue
							}
							fieldsToProcess = append(fieldsToProcess, &FieldRule{Name: bmFieldTyp.Name, Nested: nil})
						}
					}
				} else {
					fieldsToProcess = append(fieldsToProcess, &FieldRule{Name: fieldTyp.Name, Nested: nil})
				}
			}
			// Default include ID if not excluded and no other includes specified (only in fallback mode)
			if options != nil && !contains(options.OrderedExclude, "id") {
				idFound := false
				for _, rule := range fieldsToProcess {
					if strings.EqualFold(rule.Name, "id") {
						idFound = true
						break
					}
				}
				if !idFound {
					fieldsToProcess = append([]*FieldRule{{Name: "id", Nested: nil}}, fieldsToProcess...)
				}
			}
		}

		// Process fields in the determined order
		for _, rule := range fieldsToProcess {
			fieldName := rule.Name

			// Check if explicitly excluded (case-insensitive)
			if options != nil && contains(options.OrderedExclude, fieldName) {
				continue
			}

			// Find the corresponding struct field by name (case-insensitive and snake_case to CamelCase)
			var field reflect.StructField
			var fieldVal reflect.Value
			found := false
			for i := 0; i < typ.NumField(); i++ {
				structField := typ.Field(i)
				if strings.EqualFold(structField.Name, fieldName) {
					field = structField
					fieldVal = val.Field(i)
					found = true
					break
				} else if structField.Type == reflect.TypeOf(BaseModel{}) && structField.Anonymous {
					baseModelTyp := structField.Type
					baseModelVal := val.Field(i)
					for j := 0; j < baseModelTyp.NumField(); j++ {
						bmField := baseModelTyp.Field(j)
						if strings.EqualFold(bmField.Name, fieldName) {
							field = bmField
							fieldVal = baseModelVal.Field(j)
							found = true
							break
						}
					}
					if found {
						break
					}
				}
			}
			// If not found, try snake_case to CamelCase mapping
			if !found {
				camelName := snakeToCamel(fieldName)
				for i := 0; i < typ.NumField(); i++ {
					structField := typ.Field(i)
					if structField.Name == camelName {
						field = structField
						fieldVal = val.Field(i)
						found = true
						break
					}
					// Check embedded BaseModel
					if structField.Type == reflect.TypeOf(BaseModel{}) && structField.Anonymous {
						baseModelTyp := structField.Type
						baseModelVal := val.Field(i)
						for j := 0; j < baseModelTyp.NumField(); j++ {
							bmField := baseModelTyp.Field(j)
							if bmField.Name == camelName {
								field = bmField
								fieldVal = baseModelVal.Field(j)
								found = true
								break
							}
						}
						if found {
							break
						}
					}
				}
			}

			if found {
				jsonTag := field.Tag.Get("json")
				jsonFieldName := fieldName
				omitEmpty := false
				if jsonTag != "" {
					tagParts := strings.Split(jsonTag, ",")
					if tagParts[0] == "-" {
						continue // Should have been caught by exclusion list or fallback generation, but double check
					}
					if tagParts[0] != "" {
						jsonFieldName = tagParts[0]
					}
					for _, part := range tagParts[1:] {
						if part == "omitempty" {
							omitEmpty = true
							break
						}
					}
				}

				// Handle omitempty
				if omitEmpty && utils.IsZero(fieldVal) {
					continue
				}

				// Recursively serialize nested fields using the nested options from the rule
				nestedOptions := rule.Nested // Pass nested options if they exist in the rule
				if nestedOptions == nil {
					nestedOptions = newJSONOptions() // Use default empty options if no specific nested rules
				}

				// Check if the field is exportable. Unexported fields cannot be accessed via Interface().
				if !field.IsExported() {
					continue // Skip unexported fields
				}

				switch fieldVal.Kind() {
				case reflect.Struct, reflect.Ptr:
					ordered.Set(jsonFieldName, t.serializeValue(fieldVal, nestedOptions))
				case reflect.Slice, reflect.Array:
					// For slices/arrays, pass the correct nestedOptions to each element
					nestedArray := make([]interface{}, fieldVal.Len())
					for k := 0; k < fieldVal.Len(); k++ {
						elemVal := fieldVal.Index(k)
						nestedArray[k] = t.serializeValue(elemVal, nestedOptions)
					}
					ordered.Set(jsonFieldName, nestedArray)
				default:
					ordered.Set(jsonFieldName, fieldVal.Interface())
				}
				continue // Done with this field
			}

			// --- Method-based virtual property support ---
			// If not found as a struct field, try to find an exported, zero-arg, single-return method matching the field name (snake_case to CamelCase)
			methodName := snakeToCamel(fieldName)
			method := val.Addr().MethodByName(methodName)
			if method.IsValid() && method.Type().NumIn() == 0 && method.Type().NumOut() == 1 {
				// Only call exported, zero-arg, single-return methods
				result := method.Call(nil)
				if len(result) == 1 {
					ordered.Set(fieldName, result[0].Interface())
				}
			}
		}
		return ordered

	} else if kind == reflect.Slice || kind == reflect.Array {
		// Serialize a slice or array of nested models
		nestedArray := make([]interface{}, val.Len())
		// Use the current options for all elements in the slice/array.
		// If DSL needs per-element control, the structure would need further changes.
		for k := 0; k < val.Len(); k++ {
			elemVal := val.Index(k)
			// Pass the same options (which contain the nested rules for this field)
			nestedArray[k] = t.serializeValue(elemVal, options)
		}
		return nestedArray
	}

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
	rootOpts := newJSONOptions()

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

				// Remove leading '-' for nested field name if present (though unusual)
				name := field
				if strings.HasPrefix(name, "-") {
					name = strings.TrimPrefix(name, "-")
				}

				// Add to ordered include list with nested options
				currentOpts.OrderedInclude = append(currentOpts.OrderedInclude, &FieldRule{
					Name:   name,
					Nested: nestedOpts, // Assign the parsed nested options
				})

				i = nestEnd // Move past '}'

			default:
				if strings.HasPrefix(field, "-") {
					name := strings.TrimPrefix(field, "-")
					if name != "" && !contains(currentOpts.OrderedExclude, name) {
						currentOpts.OrderedExclude = append(currentOpts.OrderedExclude, name)
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
	parsedOpts, err := parse(dsl)
	if err != nil {
		return nil, err
	}
	rootOpts = parsedOpts

	// Recursively apply the id default rule to all JSONOptions (including nested)
	var applyIDDefaultRule func(opts *JSONOptions)
	applyIDDefaultRule = func(opts *JSONOptions) {
		if opts == nil {
			return
		}
		idExcluded := containsExclude(opts.OrderedExclude, "id")
		idExplicitlyIncluded := false
		for _, rule := range opts.OrderedInclude {
			if strings.EqualFold(rule.Name, "id") {
				idExplicitlyIncluded = true
				break
			}
		}
		if (len(opts.OrderedInclude) == 0 || !idExplicitlyIncluded) && !idExcluded {
			// Prepend id to the include list if not excluded and not already included
			opts.OrderedInclude = append([]*FieldRule{{Name: "id", Nested: nil}}, opts.OrderedInclude...)
		}
		// Recurse into nested options
		for _, rule := range opts.OrderedInclude {
			if rule.Nested != nil {
				applyIDDefaultRule(rule.Nested)
			}
		}
	}
	applyIDDefaultRule(rootOpts)

	return rootOpts, nil
}

// containsExclude is a helper like contains but specifically for the exclude list check
func containsExclude(slice []string, item string) bool {
	return contains(slice, item)
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

	// Recursively apply id default rule to all nested JSONOptions after merge
	for _, rule := range dst.OrderedInclude {
		if rule.Nested != nil {
			idExcluded := containsExclude(rule.Nested.OrderedExclude, "id")
			idExplicitlyIncluded := false
			for _, r := range rule.Nested.OrderedInclude {
				if strings.EqualFold(r.Name, "id") {
					idExplicitlyIncluded = true
					break
				}
			}
			if (len(rule.Nested.OrderedInclude) == 0 || !idExplicitlyIncluded) && !idExcluded {
				// Prepend id to the include list if not excluded and not already included
				rule.Nested.OrderedInclude = append([]*FieldRule{{Name: "id", Nested: nil}}, rule.Nested.OrderedInclude...)
			}
			// 不再递归合并嵌套，仅保留第一个出现的嵌套规则
		}
	}

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
