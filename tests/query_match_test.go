package thing_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	// Import the package we are testing
	// Adjust the import path if your module structure is different
	"thing"
	"thing/internal/cache" // Import internal cache
)

// Use a model defined within the main thing package or define one here if needed.
// Reusing setup_test.go models if possible is good practice.

// Mock Model for testing CheckQueryMatch
type MatchTestModel struct {
	thing.BaseModel
	Name   string  `db:"name"`
	Status int     `db:"status"`
	Count  int64   `db:"count"`
	Amount float64 `db:"amount"`
	Code   *string `db:"code"`  // Pointer type
	Flags  []int   `db:"flags"` // Slice type
}

func (m MatchTestModel) TableName() string {
	return "match_test_models"
}

func TestCheckQueryMatch(t *testing.T) {
	// Get ModelInfo for the test model
	modelInfo, err := thing.GetCachedModelInfo(reflect.TypeOf(MatchTestModel{}))
	require.NoError(t, err)
	require.NotNil(t, modelInfo)

	strPtr := func(s string) *string { return &s }

	model := MatchTestModel{
		Name:   "Test Name",
		Status: 1,
		Count:  100,
		Amount: 99.99,
		Code:   strPtr("ABC"),
		Flags:  []int{10, 20, 30},
	}

	// Define test cases
	tests := []struct {
		name        string
		params      cache.QueryParams // Use internal type
		expected    bool
		expectError bool
	}{
		{
			name: "Simple Equality Match",
			params: cache.QueryParams{
				Where: "status = ?",
				Args:  []interface{}{1},
			},
			expected: true,
		},
		{
			name: "Simple Equality Mismatch",
			params: cache.QueryParams{
				Where: "status = ?",
				Args:  []interface{}{2},
			},
			expected: false,
		},
		{
			name: "Equality Match String",
			params: cache.QueryParams{
				Where: "name = ?",
				Args:  []interface{}{"Test Name"},
			},
			expected: true,
		},
		{
			name: "Equality Mismatch String",
			params: cache.QueryParams{
				Where: "name = ?",
				Args:  []interface{}{"Wrong Name"},
			},
			expected: false,
		},
		{
			name: "Multiple AND Conditions Match",
			params: cache.QueryParams{
				Where: "status = ? AND name = ?",
				Args:  []interface{}{1, "Test Name"},
			},
			expected: true,
		},
		{
			name: "Multiple AND Conditions Mismatch (First)",
			params: cache.QueryParams{
				Where: "status = ? AND name = ?",
				Args:  []interface{}{0, "Test Name"},
			},
			expected: false,
		},
		{
			name: "Multiple AND Conditions Mismatch (Second)",
			params: cache.QueryParams{
				Where: "status = ? AND name = ?",
				Args:  []interface{}{1, "Wrong Name"},
			},
			expected: false,
		},
		{
			name: "LIKE Match End Wildcard",
			params: cache.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"Test %"},
			},
			expected: true,
		},
		{
			name: "LIKE Match Start Wildcard",
			params: cache.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"% Name"},
			},
			expected: true,
		},
		{
			name: "LIKE Match Both Wildcards",
			params: cache.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"% Na%"},
			},
			expected: true,
		},
		{
			name: "LIKE Match No Wildcards (Exact)",
			params: cache.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"Test Name"},
			},
			expected: true,
		},
		{
			name: "LIKE Mismatch",
			params: cache.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"Non%"},
			},
			expected: false,
		},
		{
			name: "LIKE Case Sensitive Mismatch",
			params: cache.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"test %"},
			},
			expected: false,
		},
		{
			name: "LIKE With Non-String Field (Error)",
			params: cache.QueryParams{
				Where: "status LIKE ?",
				Args:  []interface{}{"1%"},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Greater Than Match",
			params: cache.QueryParams{
				Where: "count > ?",
				Args:  []interface{}{50},
			},
			expected: true,
		},
		{
			name: "Greater Than Mismatch (Equal)",
			params: cache.QueryParams{
				Where: "count > ?",
				Args:  []interface{}{100},
			},
			expected: false,
		},
		{
			name: "Greater Than Mismatch (Less)",
			params: cache.QueryParams{
				Where: "count > ?",
				Args:  []interface{}{150},
			},
			expected: false,
		},
		{
			name: "Greater Than or Equal Match (Equal)",
			params: cache.QueryParams{
				Where: "count >= ?",
				Args:  []interface{}{100},
			},
			expected: true,
		},
		{
			name: "Greater Than or Equal Match (Greater)",
			params: cache.QueryParams{
				Where: "count >= ?",
				Args:  []interface{}{99},
			},
			expected: true,
		},
		{
			name: "Greater Than or Equal Mismatch",
			params: cache.QueryParams{
				Where: "count >= ?",
				Args:  []interface{}{101},
			},
			expected: false,
		},
		{
			name: "Less Than Match",
			params: cache.QueryParams{
				Where: "amount < ?",
				Args:  []interface{}{100.0},
			},
			expected: true,
		},
		{
			name: "Less Than Mismatch (Equal)",
			params: cache.QueryParams{
				Where: "amount < ?",
				Args:  []interface{}{99.99},
			},
			expected: false,
		},
		{
			name: "Less Than or Equal Match",
			params: cache.QueryParams{
				Where: "amount <= ?",
				Args:  []interface{}{99.99},
			},
			expected: true,
		},
		{
			name: "IN Match Integer",
			params: cache.QueryParams{
				Where: "status IN (?)",
				Args:  []interface{}{[]int{1, 2, 3}},
			},
			expected: true,
		},
		{
			name: "IN Match String",
			params: cache.QueryParams{
				Where: "name IN (?)",
				Args:  []interface{}{[]string{"Another Name", "Test Name"}},
			},
			expected: true,
		},
		{
			name: "IN Mismatch",
			params: cache.QueryParams{
				Where: "status IN (?)",
				Args:  []interface{}{[]int{5, 6, 7}},
			},
			expected: false,
		},
		{
			name: "IN Empty Slice Mismatch",
			params: cache.QueryParams{
				Where: "status IN (?)",
				Args:  []interface{}{[]int{}},
			},
			expected: false,
		},
		{
			name: "IN Invalid Arg Type (Error)",
			params: cache.QueryParams{
				Where: "status IN (?)",
				Args:  []interface{}{123},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "IN Incompatible Slice Type (Error)",
			params: cache.QueryParams{
				Where: "status IN (?)",
				Args:  []interface{}{[]string{"a", "b"}},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Unsupported Operator (Error)",
			params: cache.QueryParams{
				Where: "status BETWEEN ? AND ?",
				Args:  []interface{}{0, 5},
			},
			expected:    false,
			expectError: true, // Expect error due to unsupported operator/format
		},
		{
			name: "Pointer Field Equality Match",
			params: cache.QueryParams{
				Where: "code = ?",
				Args:  []interface{}{"ABC"},
			},
			expected: true,
		},
		{
			name: "Pointer Field Equality Mismatch",
			params: cache.QueryParams{
				Where: "code = ?",
				Args:  []interface{}{"DEF"},
			},
			expected: false,
		},
		{
			name: "Pointer Field Equality Match (Nil Arg)",
			params: cache.QueryParams{
				Where: "code = ?",
				Args:  []interface{}{nil},
			},
			expected: false, // model.Code is not nil
		},
		// --- Add tests for !=, <>, NOT LIKE, NOT IN ---
		{
			name: "Not Equal (!=) Match",
			params: cache.QueryParams{
				Where: "status != ?",
				Args:  []interface{}{2},
			},
			expected: true,
		},
		{
			name: "Not Equal (!=) Mismatch",
			params: cache.QueryParams{
				Where: "status != ?",
				Args:  []interface{}{1},
			},
			expected: false,
		},
		{
			name: "Not Equal (<>) Match String",
			params: cache.QueryParams{
				Where: "name <> ?",
				Args:  []interface{}{"Wrong Name"},
			},
			expected: true,
		},
		{
			name: "Not Equal (<>) Mismatch String",
			params: cache.QueryParams{
				Where: "name <> ?",
				Args:  []interface{}{"Test Name"},
			},
			expected: false,
		},
		{
			name: "NOT LIKE Match",
			params: cache.QueryParams{
				Where: "name NOT LIKE ?",
				Args:  []interface{}{"Non%"},
			},
			expected: true,
		},
		{
			name: "NOT LIKE Mismatch (End Wildcard)",
			params: cache.QueryParams{
				Where: "name NOT LIKE ?",
				Args:  []interface{}{"Test %"},
			},
			expected: false,
		},
		{
			name: "NOT LIKE Mismatch (Exact)",
			params: cache.QueryParams{
				Where: "name NOT LIKE ?",
				Args:  []interface{}{"Test Name"},
			},
			expected: false,
		},
		{
			name: "NOT LIKE With Non-String Field (Error)",
			params: cache.QueryParams{
				Where: "status NOT LIKE ?",
				Args:  []interface{}{"1%"},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "NOT IN Match Integer",
			params: cache.QueryParams{
				Where: "status NOT IN (?)",
				Args:  []interface{}{[]int{5, 6, 7}},
			},
			expected: true,
		},
		{
			name: "NOT IN Match String",
			params: cache.QueryParams{
				Where: "name NOT IN (?)",
				Args:  []interface{}{[]string{"Another Name", "Wrong Name"}},
			},
			expected: true,
		},
		{
			name: "NOT IN Mismatch",
			params: cache.QueryParams{
				Where: "status NOT IN (?)",
				Args:  []interface{}{[]int{1, 2, 3}},
			},
			expected: false,
		},
		{
			name: "NOT IN Empty Slice Match", // Value is not in empty slice
			params: cache.QueryParams{
				Where: "status NOT IN (?)",
				Args:  []interface{}{[]int{}},
			},
			expected: true,
		},
		{
			name: "NOT IN Invalid Arg Type (Error)",
			params: cache.QueryParams{
				Where: "status NOT IN (?)",
				Args:  []interface{}{123},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "NOT IN Incompatible Slice Type (Error)",
			params: cache.QueryParams{
				Where: "status NOT IN (?)",
				Args:  []interface{}{[]string{"a", "b"}},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Unsupported Operator (!~)",
			params: cache.QueryParams{
				Where: "status !~ ?",
				Args:  []interface{}{5},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Invalid WHERE Clause Format",
			params: cache.QueryParams{
				Where: "status = 1", // No placeholder
				Args:  []interface{}{},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Mismatched Args/Placeholders",
			params: cache.QueryParams{
				Where: "status = ? AND name = ?",
				Args:  []interface{}{1}, // Only one arg
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Column Not Found",
			params: cache.QueryParams{
				Where: "nonexistent_col = ?",
				Args:  []interface{}{1},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Empty WHERE Clause",
			params: cache.QueryParams{
				Where: "",
				Args:  []interface{}{},
			},
			expected: true, // Empty WHERE matches everything
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert root ModelInfo to internal ModelInfo for the call
			infoInternal := &cache.ModelInfo{
				TableName:        modelInfo.TableName,
				ColumnToFieldMap: modelInfo.ColumnToFieldMap,
				// Add other fields if needed by CheckQueryMatch
			}

			// Call the function from internal/cache directly
			// Pass model, internal modelInfo, and params
			match, err := cache.CheckQueryMatch(&model, infoInternal, tt.params)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, match)
			}
		})
	}
}
