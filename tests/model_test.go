package thing_test

import (
	"encoding/json"
	"testing"
	"thing"
	"time"

	"github.com/stretchr/testify/require"
)

// --- Test Models ---

// User represents a user for testing.
type User struct {
	thing.BaseModel         // Use embedding again
	Name            string  `db:"name"`
	Email           string  `db:"email"`
	Books           []*Book `thing:"hasMany;fk:user_id;model:Book" db:"-"`
}

// Change TableName to pointer receiver to satisfy Model[T] interface directly
func (u *User) TableName() string {
	return "users"
}

// Book represents a book for testing.
type Book struct {
	thing.BaseModel
	Title  string `db:"title"`
	UserID int64  `db:"user_id"`
	User   *User  `thing:"belongsTo;fk:user_id"`
}

// Change TableName to pointer receiver
func (b *Book) TableName() string {
	return "books"
}

func TestBaseModel_ToJSONWithOptions(t *testing.T) {
	// Use a model with more fields for testing options
	type TestJSONModel struct {
		thing.BaseModel
		Name   string `json:"name"`
		Email  string `json:"email,omitempty"`
		Status int    `json:"status"`
		Hidden string `json:"-"` // Should always be excluded by tag
	}

	// Setup Thing instance (using test DB and cache)
	db, cache, cleanup := setupTestDB(t)
	defer cleanup()
	thingInstance, err := thing.New[*TestJSONModel](db, cache)
	require.NoError(t, err)

	now := time.Now()
	m := &TestJSONModel{
		BaseModel: thing.BaseModel{
			ID:        123,
			CreatedAt: now,
			UpdatedAt: now,
		},
		Name:   "Test User",
		Email:  "test@example.com",
		Status: 1,
		Hidden: "should not see",
	}

	tests := []struct {
		name       string
		opts       []thing.JSONOption
		expected   map[string]interface{}
		unexpected []string // Fields that should NOT be in the output
	}{
		{
			name: "No Options (Defaults)",
			opts: nil,
			expected: map[string]interface{}{
				"id":         float64(123), // JSON numbers are float64 by default
				"created_at": now.Format(time.RFC3339Nano),
				"updated_at": now.Format(time.RFC3339Nano),
				"deleted":    false,
				"name":       "Test User",
				"email":      "test@example.com",
				"status":     float64(1),
			},
			unexpected: []string{"Hidden", "isNewRecord"},
		},
		{
			name: "Include Specific Fields",
			opts: []thing.JSONOption{thing.Include("name", "status")},
			expected: map[string]interface{}{
				"id":     float64(123), // ID included by default
				"name":   "Test User",
				"status": float64(1),
			},
			unexpected: []string{"created_at", "updated_at", "deleted", "email", "Hidden"},
		},
		{
			name: "Exclude Specific Fields",
			opts: []thing.JSONOption{thing.Exclude("created_at", "updated_at", "email")},
			expected: map[string]interface{}{
				"id":      float64(123),
				"deleted": false,
				"name":    "Test User",
				"status":  float64(1),
			},
			unexpected: []string{"created_at", "updated_at", "email", "Hidden"},
		},
		{
			name: "Exclude ID",
			opts: []thing.JSONOption{thing.Exclude("id")},
			expected: map[string]interface{}{
				"created_at": now.Format(time.RFC3339Nano),
				"updated_at": now.Format(time.RFC3339Nano),
				"deleted":    false,
				"name":       "Test User",
				"email":      "test@example.com",
				"status":     float64(1),
			},
			unexpected: []string{"id", "Hidden"},
		},
		{
			name: "Include Specific Fields (Exclude ID)",
			opts: []thing.JSONOption{thing.Include("name", "status"), thing.Exclude("id")},
			expected: map[string]interface{}{
				"name":   "Test User",
				"status": float64(1),
			},
			unexpected: []string{"id", "created_at", "updated_at", "deleted", "email", "Hidden"},
		},
		{
			name: "Exclude All Base Fields",
			opts: []thing.JSONOption{thing.Exclude("id", "created_at", "updated_at", "deleted")},
			expected: map[string]interface{}{
				"name":   "Test User",
				"email":  "test@example.com",
				"status": float64(1),
			},
			unexpected: []string{"id", "created_at", "updated_at", "deleted", "Hidden"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call ToJSON on the Thing instance, passing the model
			jsonBytes, err := thingInstance.ToJSON(m, tt.opts...)
			if err != nil {
				t.Fatalf("ToJSON failed: %v", err)
			}

			var outputMap map[string]interface{}
			err = json.Unmarshal(jsonBytes, &outputMap)
			if err != nil {
				t.Fatalf("Failed to unmarshal result JSON: %v\nJSON: %s", err, string(jsonBytes))
			}

			// Check expected fields
			for key, expectedValue := range tt.expected {
				actualValue, ok := outputMap[key]
				if !ok {
					t.Errorf("Expected field '%s' missing from JSON output", key)
					continue
				}
				// Special handling for time comparison
				if expectedTimeStr, ok := expectedValue.(string); ok {
					if actualTimeStr, ok := actualValue.(string); ok {
						expectedTime, _ := time.Parse(time.RFC3339Nano, expectedTimeStr)
						actualTime, _ := time.Parse(time.RFC3339Nano, actualTimeStr)
						if !expectedTime.Equal(actualTime) {
							t.Errorf("Field '%s': expected time %v, got %v", key, expectedTime, actualTime)
						}
						continue // Skip next comparison for time
					}
				}

				if actualValue != expectedValue {
					t.Errorf("Field '%s': expected %v (%T), got %v (%T)", key, expectedValue, expectedValue, actualValue, actualValue)
				}
			}

			// Check unexpected fields
			for _, key := range tt.unexpected {
				if _, ok := outputMap[key]; ok {
					t.Errorf("Unexpected field '%s' found in JSON output", key)
				}
			}
		})
	}
}
