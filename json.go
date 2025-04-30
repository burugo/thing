package thing

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
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
// Helper function for the ToJSON method.
func shouldIncludeField(goName, jsonName string, useWhitelist bool, options *jsonOptions) bool {
	include := false
	if useWhitelist {
		// Whitelist mode: only include if explicitly mentioned
		if options.include[goName] || options.include[jsonName] {
			include = true
		}
		// Always include ID by default in whitelist mode, unless explicitly excluded
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
func (t *Thing[T]) ToJSON(model *T, opts ...JSONOption) ([]byte, error) {
	if model == nil {
		return nil, errors.New("cannot serialize nil model")
	}

	options := newJsonOptions()
	for _, opt := range opts {
		opt(options)
	}

	outputMap := make(map[string]interface{})
	val := reflect.ValueOf(model).Elem() // model is *T, Elem() gives T
	typ := val.Type()
	useWhitelist := len(options.include) > 0

	for i := 0; i < val.NumField(); i++ {
		fieldVal := val.Field(i)
		fieldTyp := typ.Field(i)
		fieldName := fieldTyp.Name // Go field name

		// --- Handle embedded BaseModel ---
		if fieldTyp.Type == reflect.TypeOf(BaseModel{}) && fieldTyp.Anonymous {
			baseModelVal := fieldVal
			baseModelTyp := baseModelVal.Type()
			for j := 0; j < baseModelVal.NumField(); j++ {
				bmFieldVal := baseModelVal.Field(j)
				bmFieldTyp := baseModelTyp.Field(j)
				bmFieldName := bmFieldTyp.Name // Go name of field within BaseModel
				jsonTag := bmFieldTyp.Tag.Get("json")
				jsonFieldName := bmFieldName // Default

				if jsonTag != "" {
					tagParts := strings.Split(jsonTag, ",")
					if tagParts[0] == "-" {
						continue // Skip json:"-"
					}
					if tagParts[0] != "" {
						jsonFieldName = tagParts[0]
					}
					// TODO: Handle omitempty
				}

				if shouldIncludeField(bmFieldName, jsonFieldName, useWhitelist, options) {
					outputMap[jsonFieldName] = bmFieldVal.Interface()
				}
			}
			continue // Done with BaseModel fields, move to next field of outer struct
		}

		// --- Handle other fields of the main struct T ---
		jsonTag := fieldTyp.Tag.Get("json")
		jsonFieldName := fieldName // Default

		if jsonTag != "" {
			tagParts := strings.Split(jsonTag, ",")
			if tagParts[0] == "-" {
				continue // Skip json:"-"
			}
			if tagParts[0] != "" {
				jsonFieldName = tagParts[0]
			}
			// TODO: Handle omitempty
		}

		if shouldIncludeField(fieldName, jsonFieldName, useWhitelist, options) {
			// TODO: Handle nested options/relationships here (check options.nested)
			outputMap[jsonFieldName] = fieldVal.Interface()
		}
	}

	return json.Marshal(outputMap)
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
		// TODO: Parse DSL string and populate opts.include, opts.exclude, opts.nested, etc.
		// This is a stub for now.
	}
}
