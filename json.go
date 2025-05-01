package thing

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"thing/internal/utils"
)

// --- JSON Serialization Options ---

// jsonOptions holds the configuration for JSON serialization.
// Uses maps for efficient lookups.
type jsonOptions struct {
	include map[string]bool         // Fields explicitly included (if non-empty, acts as whitelist)
	exclude map[string]bool         // Fields explicitly excluded
	nested  map[string]*jsonOptions // Options for nested/relationship fields (Key: field name)
	// TODO: Add support for static struct tag rules
}

// newJsonOptions creates a default options struct.
func newJsonOptions() *jsonOptions {
	return &jsonOptions{
		include: make(map[string]bool),
		exclude: make(map[string]bool),
		nested:  make(map[string]*jsonOptions),
	}
}

// JSONOption defines the function signature for JSON serialization options.
type JSONOption func(*jsonOptions)

// Include specifies fields to include in the JSON output.
// If any field is explicitly included, only those fields (plus ID by default unless excluded) will be present.
func Include(fields ...string) JSONOption {
	return func(opts *jsonOptions) {
		for _, field := range fields {
			opts.include[field] = true
		}
	}
}

// Exclude specifies fields to exclude from the JSON output.
func Exclude(fields ...string) JSONOption {
	return func(opts *jsonOptions) {
		for _, field := range fields {
			opts.exclude[field] = true
		}
	}
}

// TODO: Implement IncludeNested and ExcludeNested for relationship serialization control

// shouldIncludeField determines if a field should be included based on options.
// Helper function for the ToJSON and serializeField methods.
func shouldIncludeField(goName, jsonName string, useWhitelist bool, options *jsonOptions) bool {
	include := false
	if useWhitelist {
		// Whitelist mode: only include if explicitly mentioned
		if options.include[goName] || options.include[jsonName] {
			include = true
		}
		// Always include ID by default in whitelist mode, unless explicitly excluded
		// This logic might need adjustment depending on how embedded BaseModel fields are handled
		if (goName == "ID" || jsonName == "id") && !options.exclude[goName] && !options.exclude[jsonName] {
			include = true
		}
	} else {
		// Blacklist mode: include by default
		include = true
	}

	// Check blacklist exclusions
	if options.exclude[goName] || options.exclude[jsonName] {
		include = false
	}
	return include
}

// --- JSON Serialization Methods ---

// ToJSON serializes the provided model instance to JSON based on the given options.
func (t *Thing[T]) ToJSON(model T, opts ...JSONOption) ([]byte, error) {
	if reflect.ValueOf(model).IsNil() {
		return nil, errors.New("cannot serialize nil model")
	}

	options := newJsonOptions()
	for _, opt := range opts {
		opt(options)
	}

	val := reflect.ValueOf(model).Elem() // model is *T, Elem() gives T
	// Pass the initial options to the recursive helper
	jsonOutput := t.serializeValue(val, options)
	return json.Marshal(jsonOutput)
}

// serializeValue recursively serializes a reflect.Value based on provided jsonOptions.
func (t *Thing[T]) serializeValue(val reflect.Value, options *jsonOptions) interface{} {
	kind := val.Kind()

	if kind == reflect.Ptr {
		if val.IsNil() {
			return nil // Handle nil pointers
		}
		val = val.Elem() // Dereference the pointer
		kind = val.Kind()
	}

	// Handle primitive types and non-structs directly
	if kind != reflect.Struct && kind != reflect.Slice && kind != reflect.Array {
		return val.Interface()
	}

	if kind == reflect.Struct {
		outputMap := make(map[string]interface{})
		typ := val.Type()
		useWhitelist := len(options.include) > 0

		for i := 0; i < typ.NumField(); i++ {
			fieldTyp := typ.Field(i)
			fieldVal := val.Field(i)
			fieldName := fieldTyp.Name // Go field name

			// Handle JSON tag and field name
			jsonTag := fieldTyp.Tag.Get("json")
			jsonFieldName := fieldName // Default to Go field name
			omitEmpty := false

			if jsonTag != "" {
				tagParts := strings.Split(jsonTag, ",")
				if tagParts[0] == "-" {
					continue // Skip json:"-"
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

			// Check if the field should be included based on options
			// Special handling for BaseModel fields if it's an embedded struct
			isBaseModel := fieldTyp.Type == reflect.TypeOf(BaseModel{}) && fieldTyp.Anonymous

			if isBaseModel {
				// For embedded BaseModel, iterate through its fields and apply rules
				baseModelVal := fieldVal
				baseModelTyp := baseModelVal.Type()
				for j := 0; j < baseModelVal.NumField(); j++ {
					bmFieldVal := baseModelVal.Field(j)
					bmFieldTyp := baseModelTyp.Field(j)
					bmFieldName := bmFieldTyp.Name // Go name of field within BaseModel
					bmJSONTag := bmFieldTyp.Tag.Get("json")
					bmJSONFieldName := bmFieldName // Default
					bmOmitEmpty := false

					if bmJSONTag != "" {
						bmTagParts := strings.Split(bmJSONTag, ",")
						if bmTagParts[0] == "-" {
							continue // Skip json:"-" in BaseModel
						}
						if bmTagParts[0] != "" {
							bmJSONFieldName = bmTagParts[0]
						}
						for _, part := range bmTagParts[1:] {
							if part == "omitempty" {
								bmOmitEmpty = true
								break
							}
						}
					}

					// Apply include/exclude rules to BaseModel fields
					if shouldIncludeField(bmFieldName, bmJSONFieldName, useWhitelist, options) {
						// Handle omitempty for BaseModel fields
						if bmOmitEmpty && utils.IsZero(bmFieldVal) {
							continue
						}
						outputMap[bmJSONFieldName] = bmFieldVal.Interface()
					}
				}
				continue // Done with embedded BaseModel, move to next field of outer struct
			}

			// Handle omitempty for non-BaseModel fields
			if omitEmpty && utils.IsZero(fieldVal) {
				continue
			}

			// Apply include/exclude rules to main struct fields
			if shouldIncludeField(fieldName, jsonFieldName, useWhitelist, options) {
				// --- Handle nested options/relationships here ---
				nestedOptions, ok := options.nested[fieldName]
				if ok && nestedOptions != nil && (len(nestedOptions.include) > 0 || len(nestedOptions.exclude) > 0) {
					// This field has nested options, recursively serialize it
					outputMap[jsonFieldName] = t.serializeValue(fieldVal, nestedOptions)
				} else {
					// No nested options, include the field normally
					outputMap[jsonFieldName] = fieldVal.Interface()
				}

			}
		}
		return outputMap

	} else if kind == reflect.Slice || kind == reflect.Array {
		// Serialize a slice or array of nested models
		nestedArray := make([]interface{}, val.Len())
		for k := 0; k < val.Len(); k++ {
			elemVal := val.Index(k)
			// For slice/array elements, use the *current* options, not nested options, unless the slice/array field itself has nested options defined for its elements.
			// The logic here needs to be careful about applying the correct options to the elements.
			// If nestedOptions exists for the slice/array field, those options should apply to the *elements*.
			nestedItem := t.serializeValue(elemVal, options.nested[val.Type().Name()]) // Use nested options for elements
			nestedArray[k] = nestedItem
		}
		return nestedArray
	}

	return nil // Should not reach here for supported types
}

// applyRuleTreeToOptions populates jsonOptions from a FieldRuleTree (recursive)
func applyRuleTreeToOptions(tree *FieldRuleTree, opts *jsonOptions) {
	if tree == nil || opts == nil {
		return
	}
	for k, v := range tree.Include {
		opts.include[k] = true
		if v != nil && (len(v.Include) > 0 || len(v.Exclude) > 0) {
			if opts.nested[k] == nil {
				opts.nested[k] = newJsonOptions()
			}
			applyRuleTreeToOptions(v, opts.nested[k])
		}
	}
	for k := range tree.Exclude {
		opts.exclude[k] = true
	}
}

// FieldRuleTree represents the include/exclude/nested rules for JSON field control.
type FieldRuleTree struct {
	Include map[string]*FieldRuleTree // Fields to include (with possible nested rules)
	Exclude map[string]bool           // Fields to exclude
}

// ParseFieldsDSL parses a DSL string and returns a FieldRuleTree representing the rules.
// Example: "name,book{title,-publish_at},-deleted"
func ParseFieldsDSL(dsl string) (*FieldRuleTree, error) {
	// Enhanced parser: handle top-level and nested include/exclude fields
	tree := &FieldRuleTree{
		Include: make(map[string]*FieldRuleTree),
		Exclude: make(map[string]bool),
	}
	if strings.TrimSpace(dsl) == "" {
		return tree, nil
	}

	var parse func(input string) (map[string]*FieldRuleTree, map[string]bool, error)
	parse = func(input string) (map[string]*FieldRuleTree, map[string]bool, error) {
		include := make(map[string]*FieldRuleTree)
		exclude := make(map[string]bool)
		var i int
		for i < len(input) {
			// Skip whitespace
			for i < len(input) && (input[i] == ' ' || input[i] == ',') {
				i++
			}
			if i >= len(input) {
				break
			}
			// Parse field name (may start with '-')
			start := i
			for i < len(input) && input[i] != ',' && input[i] != '{' && input[i] != '}' {
				i++
			}
			field := strings.TrimSpace(input[start:i])
			if field == "" {
				continue
			}
			// Check for nested
			if i < len(input) && input[i] == '{' {
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
					return nil, nil, errors.New("unmatched '{' in DSL")
				}
				nestedContent := input[nestStart : nestEnd-1]
				nestedTree, nestedExclude, err := parse(nestedContent)
				if err != nil {
					return nil, nil, err
				}
				// Remove leading '-' for nested exclude
				name := field
				if strings.HasPrefix(name, "-") {
					name = strings.TrimPrefix(name, "-")
				}
				// Assign both Include and Exclude from nestedTree (which is the result of parsing inside braces)
				if existing, ok := include[name]; ok && existing != nil {
					// Merge includes
					for k, v := range nestedTree {
						if v2, ok2 := existing.Include[k]; ok2 && v2 != nil && v != nil {
							// Recursively merge
							mergeFieldRuleTree(v2, v)
						} else {
							existing.Include[k] = v
						}
					}
					// Merge excludes
					for ex, exVal := range nestedExclude {
						existing.Exclude[ex] = exVal
					}
				} else {
					include[name] = &FieldRuleTree{
						Include: nestedTree,
						Exclude: make(map[string]bool),
					}
					for ex, exVal := range nestedExclude {
						include[name].Exclude[ex] = exVal
					}
				}
				i = nestEnd
			} else if strings.HasPrefix(field, "-") {
				name := strings.TrimPrefix(field, "-")
				if name != "" {
					exclude[name] = true
				}
			} else {
				include[field] = &FieldRuleTree{
					Include: make(map[string]*FieldRuleTree),
					Exclude: make(map[string]bool),
				}
			}
			// Skip trailing ','
			for i < len(input) && (input[i] == ' ' || input[i] == ',') {
				i++
			}
		}
		return include, exclude, nil
	}

	inc, exc, err := parse(dsl)
	if err != nil {
		return nil, err
	}
	tree.Include = inc
	tree.Exclude = exc
	return tree, nil
}

// mergeFieldRuleTree merges src into dst recursively (for repeated fields in DSL)
func mergeFieldRuleTree(dst, src *FieldRuleTree) {
	if dst == nil || src == nil {
		return
	}
	// Merge includes
	for k, v := range src.Include {
		if v2, ok2 := dst.Include[k]; ok2 && v2 != nil && v != nil {
			mergeFieldRuleTree(v2, v)
		} else {
			dst.Include[k] = v
		}
	}
	// Merge excludes
	for ex, exVal := range src.Exclude {
		dst.Exclude[ex] = exVal
	}
}

// WithFields is an alias for WithFieldsDSL, allowing users to specify flexible field control using a simple string DSL.
// Example: user.ToJSON(WithFields("name,book{title,-publish_at}"))
func WithFields(dsl string) JSONOption {
	return WithFieldsDSL(dsl)
}

// WithFieldsDSL parses a DSL string and returns a JSONOption for flexible field control.
// Example: user.ToJSON(WithFieldsDSL("name,book{title,-publish_at}"))
func WithFieldsDSL(dsl string) JSONOption {
	return func(opts *jsonOptions) {
		ruleTree, err := ParseFieldsDSL(dsl)
		if err != nil {
			// If DSL is invalid, do nothing (or could panic/log)
			return
		}
		applyRuleTreeToOptions(ruleTree, opts)
	}
}
