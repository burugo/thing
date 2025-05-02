package thing_test

import (
	"reflect"
	"testing"
	"thing/internal/cache"
	"thing/internal/types"

	"github.com/stretchr/testify/require"

	// Import the package we are testing
	// Adjust the import path if your module structure is different
	"thing"
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
		params      types.QueryParams // Use internal type
		expected    bool
		expectError bool
	}{
		{
			name: "Simple Equality Match",
			params: types.QueryParams{
				Where: "status = ?",
				Args:  []interface{}{1},
			},
			expected: true,
		},
		{
			name: "Simple Equality Mismatch",
			params: types.QueryParams{
				Where: "status = ?",
				Args:  []interface{}{2},
			},
			expected: false,
		},
		{
			name: "Equality Match String",
			params: types.QueryParams{
				Where: "name = ?",
				Args:  []interface{}{"Test Name"},
			},
			expected: true,
		},
		{
			name: "Equality Mismatch String",
			params: types.QueryParams{
				Where: "name = ?",
				Args:  []interface{}{"Wrong Name"},
			},
			expected: false,
		},
		{
			name: "Multiple AND Conditions Match",
			params: types.QueryParams{
				Where: "status = ? AND name = ?",
				Args:  []interface{}{1, "Test Name"},
			},
			expected: true,
		},
		{
			name: "Multiple AND Conditions Mismatch (First)",
			params: types.QueryParams{
				Where: "status = ? AND name = ?",
				Args:  []interface{}{0, "Test Name"},
			},
			expected: false,
		},
		{
			name: "Multiple AND Conditions Mismatch (Second)",
			params: types.QueryParams{
				Where: "status = ? AND name = ?",
				Args:  []interface{}{1, "Wrong Name"},
			},
			expected: false,
		},
		{
			name: "LIKE Match End Wildcard",
			params: types.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"Test %"},
			},
			expected: true,
		},
		{
			name: "LIKE Match Start Wildcard",
			params: types.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"% Name"},
			},
			expected: true,
		},
		{
			name: "LIKE Match Both Wildcards",
			params: types.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"% Na%"},
			},
			expected: true,
		},
		{
			name: "LIKE Match No Wildcards (Exact)",
			params: types.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"Test Name"},
			},
			expected: true,
		},
		{
			name: "LIKE Mismatch",
			params: types.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"Non%"},
			},
			expected: false,
		},
		{
			name: "LIKE Case Sensitive Mismatch",
			params: types.QueryParams{
				Where: "name LIKE ?",
				Args:  []interface{}{"test %"},
			},
			expected: false,
		},
		{
			name: "LIKE With Non-String Field (Error)",
			params: types.QueryParams{
				Where: "status LIKE ?",
				Args:  []interface{}{"1%"},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Greater Than Match",
			params: types.QueryParams{
				Where: "count > ?",
				Args:  []interface{}{50},
			},
			expected: true,
		},
		{
			name: "Greater Than Mismatch (Equal)",
			params: types.QueryParams{
				Where: "count > ?",
				Args:  []interface{}{100},
			},
			expected: false,
		},
		{
			name: "Greater Than Mismatch (Less)",
			params: types.QueryParams{
				Where: "count > ?",
				Args:  []interface{}{150},
			},
			expected: false,
		},
		{
			name: "Greater Than or Equal Match (Equal)",
			params: types.QueryParams{
				Where: "count >= ?",
				Args:  []interface{}{100},
			},
			expected: true,
		},
		{
			name: "Greater Than or Equal Match (Greater)",
			params: types.QueryParams{
				Where: "count >= ?",
				Args:  []interface{}{99},
			},
			expected: true,
		},
		{
			name: "Greater Than or Equal Mismatch",
			params: types.QueryParams{
				Where: "count >= ?",
				Args:  []interface{}{101},
			},
			expected: false,
		},
		{
			name: "Less Than Match",
			params: types.QueryParams{
				Where: "amount < ?",
				Args:  []interface{}{100.0},
			},
			expected: true,
		},
		{
			name: "Less Than Mismatch (Equal)",
			params: types.QueryParams{
				Where: "amount < ?",
				Args:  []interface{}{99.99},
			},
			expected: false,
		},
		{
			name: "Less Than or Equal Match",
			params: types.QueryParams{
				Where: "amount <= ?",
				Args:  []interface{}{99.99},
			},
			expected: true,
		},
		{
			name: "IN Match Integer",
			params: types.QueryParams{
				Where: "status IN (?)",
				Args:  []interface{}{[]int{1, 2, 3}},
			},
			expected: true,
		},
		{
			name: "IN Match String",
			params: types.QueryParams{
				Where: "name IN (?)",
				Args:  []interface{}{[]string{"Another Name", "Test Name"}},
			},
			expected: true,
		},
		{
			name: "IN Mismatch",
			params: types.QueryParams{
				Where: "status IN (?)",
				Args:  []interface{}{[]int{5, 6, 7}},
			},
			expected: false,
		},
		{
			name: "IN Empty Slice Mismatch",
			params: types.QueryParams{
				Where: "status IN (?)",
				Args:  []interface{}{[]int{}},
			},
			expected: false,
		},
		{
			name: "IN Invalid Arg Type (Error)",
			params: types.QueryParams{
				Where: "status IN (?)",
				Args:  []interface{}{123},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "IN Incompatible Slice Type (Error)",
			params: types.QueryParams{
				Where: "status IN (?)",
				Args:  []interface{}{[]string{"a", "b"}},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Unsupported Operator (Error)",
			params: types.QueryParams{
				Where: "status BETWEEN ? AND ?",
				Args:  []interface{}{0, 5},
			},
			expected:    false,
			expectError: true, // Expect error due to unsupported operator/format
		},
		{
			name: "Pointer Field Equality Match",
			params: types.QueryParams{
				Where: "code = ?",
				Args:  []interface{}{"ABC"},
			},
			expected: true,
		},
		{
			name: "Pointer Field Equality Mismatch",
			params: types.QueryParams{
				Where: "code = ?",
				Args:  []interface{}{"DEF"},
			},
			expected: false,
		},
		{
			name: "Pointer Field Equality Match (Nil Arg)",
			params: types.QueryParams{
				Where: "code = ?",
				Args:  []interface{}{nil},
			},
			expected: false, // model.Code is not nil
		},
		// --- Add tests for !=, <>, NOT LIKE, NOT IN ---
		{
			name: "Not Equal (!=) Match",
			params: types.QueryParams{
				Where: "status != ?",
				Args:  []interface{}{2},
			},
			expected: true,
		},
		{
			name: "Not Equal (!=) Mismatch",
			params: types.QueryParams{
				Where: "status != ?",
				Args:  []interface{}{1},
			},
			expected: false,
		},
		{
			name: "Not Equal (<>) Match String",
			params: types.QueryParams{
				Where: "name <> ?",
				Args:  []interface{}{"Wrong Name"},
			},
			expected: true,
		},
		{
			name: "Not Equal (<>) Mismatch String",
			params: types.QueryParams{
				Where: "name <> ?",
				Args:  []interface{}{"Test Name"},
			},
			expected: false,
		},
		{
			name: "NOT LIKE Match",
			params: types.QueryParams{
				Where: "name NOT LIKE ?",
				Args:  []interface{}{"Non%"},
			},
			expected: true,
		},
		{
			name: "NOT LIKE Mismatch (End Wildcard)",
			params: types.QueryParams{
				Where: "name NOT LIKE ?",
				Args:  []interface{}{"Test %"},
			},
			expected: false,
		},
		{
			name: "NOT LIKE Mismatch (Exact)",
			params: types.QueryParams{
				Where: "name NOT LIKE ?",
				Args:  []interface{}{"Test Name"},
			},
			expected: false,
		},
		{
			name: "NOT LIKE With Non-String Field (Error)",
			params: types.QueryParams{
				Where: "status NOT LIKE ?",
				Args:  []interface{}{"1%"},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "NOT IN Match Integer",
			params: types.QueryParams{
				Where: "status NOT IN (?)",
				Args:  []interface{}{[]int{5, 6, 7}},
			},
			expected: true,
		},
		{
			name: "NOT IN Match String",
			params: types.QueryParams{
				Where: "name NOT IN (?)",
				Args:  []interface{}{[]string{"Another Name", "Wrong Name"}},
			},
			expected: true,
		},
		{
			name: "NOT IN Mismatch",
			params: types.QueryParams{
				Where: "status NOT IN (?)",
				Args:  []interface{}{[]int{1, 2, 3}},
			},
			expected: false,
		},
		{
			name: "NOT IN Empty Slice Match", // Value is not in empty slice
			params: types.QueryParams{
				Where: "status NOT IN (?)",
				Args:  []interface{}{[]int{}},
			},
			expected: true,
		},
		{
			name: "NOT IN Invalid Arg Type (Error)",
			params: types.QueryParams{
				Where: "status NOT IN (?)",
				Args:  []interface{}{123},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "NOT IN Incompatible Slice Type (Error)",
			params: types.QueryParams{
				Where: "status NOT IN (?)",
				Args:  []interface{}{[]string{"a", "b"}},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Unsupported Operator (!~)",
			params: types.QueryParams{
				Where: "status !~ ?",
				Args:  []interface{}{5},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Invalid WHERE Clause Format",
			params: types.QueryParams{
				Where: "status = 1", // No placeholder
				Args:  []interface{}{},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Mismatched Args/Placeholders",
			params: types.QueryParams{
				Where: "status = ? AND name = ?",
				Args:  []interface{}{1}, // Only one arg
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Column Not Found",
			params: types.QueryParams{
				Where: "nonexistent_col = ?",
				Args:  []interface{}{1},
			},
			expected:    false,
			expectError: true,
		},
		{
			name: "Empty WHERE Clause",
			params: types.QueryParams{
				Where: "",
				Args:  []interface{}{},
			},
			expected: true, // Empty WHERE matches everything
		},
	}

	// Run tests
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Pass the model by pointer as CheckQueryMatch expects an interface{} that can be Elem()'d
			// and pass the required info fields directly
			result, err := cache.CheckQueryMatch(&model, modelInfo.TableName, modelInfo.ColumnToFieldMap, tt.params)

			if tt.expectError {
				require.Error(t, err, "Expected an error but got none")
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}
