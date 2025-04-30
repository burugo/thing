package thing

import (
	"fmt"
	"reflect"
	"time"
)

// --- BaseModel Struct ---

// BaseModel provides common fields and functionality for database models.
// It should be embedded into specific model structs.
type BaseModel struct {
	ID        int64     `json:"id" db:"id,pk"`              // Primary key (Added pk tag)
	CreatedAt time.Time `json:"created_at" db:"created_at"` // Timestamp for creation
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"` // Timestamp for last update
	Deleted   bool      `json:"deleted" db:"deleted"`       // Soft delete flag

	// --- Internal ORM state ---
	// These fields should be populated by the ORM functions (ByID, Create, etc.).
	// They are NOT saved to the DB or cache.
	isNewRecord bool `json:"-" db:"-"` // Flag to indicate if the record is new
}

// --- BaseModel Methods ---

// GetID returns the primary key value.
func (b BaseModel) GetID() int64 {
	return b.ID
}

// SetID sets the primary key value.
func (b *BaseModel) SetID(id int64) {
	b.ID = id
}

// TableName returns the database table name for the model.
// Default implementation returns empty string, relying on getTableNameFromType.
// Override this method in your specific model struct for custom table names.
func (b BaseModel) TableName() string {
	// Default implementation, getTableNameFromType will be used if this returns ""
	return ""
}

// IsNewRecord returns whether this is a new record.
func (b *BaseModel) IsNewRecord() bool {
	return b.isNewRecord
}

// KeepItem checks if the record is considered active (not soft-deleted).
func (b BaseModel) KeepItem() bool {
	return !b.Deleted
}

// SetNewRecordFlag sets the internal isNewRecord flag.
func (b *BaseModel) SetNewRecordFlag(isNew bool) {
	b.isNewRecord = isNew
}

// --- Helper Functions --- (Moved GetBaseModelPtr back here)

// GetBaseModelPtr returns a pointer to the embedded BaseModel if it exists and is addressable.
func getBaseModelPtr(value interface{}) *BaseModel {
	if value == nil {
		return nil
	}
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr && val.IsNil() {
		return nil
	}
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

// generateCacheKey creates a standard cache key string for a single model.
func generateCacheKey(tableName string, id int64) string {
	// Format: {tableName}:{id}
	return fmt.Sprintf("%s:%d", tableName, id)
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
	// Fallback for structs not embedding BaseModel but having CreatedAt
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() == reflect.Struct {
		field := val.FieldByName("CreatedAt")
		if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
			field.Set(reflect.ValueOf(t))
		}
	}
}

// setUpdatedAtTimestamp sets the UpdatedAt field if it exists.
func setUpdatedAtTimestamp(value interface{}, t time.Time) {
	if bmPtr := getBaseModelPtr(value); bmPtr != nil {
		bmPtr.UpdatedAt = t
		return
	}
	// Fallback for structs not embedding BaseModel but having UpdatedAt
	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() == reflect.Struct {
		field := val.FieldByName("UpdatedAt")
		if field.IsValid() && field.CanSet() && field.Type() == reflect.TypeOf(time.Time{}) {
			field.Set(reflect.ValueOf(t))
		}
	}
}
