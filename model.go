package thing

import (
	"time"
)

// --- BaseModel Struct ---

// BaseModel provides common fields and functionality for database models.
// It should be embedded into specific model structs.
type BaseModel struct {
	ID        int64     `json:"id" db:"id"`                 // Primary key
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
func (b *BaseModel) GetID() int64 {
	return b.ID
}

// SetID sets the primary key value.
func (b *BaseModel) SetID(id int64) {
	b.ID = id
}

// TableName returns the database table name for the model.
// Default implementation returns empty string, relying on getTableNameFromType.
// Override this method in your specific model struct for custom table names.
func (b *BaseModel) TableName() string {
	// Default implementation, getTableNameFromType will be used if this returns ""
	return ""
}

// IsNewRecord returns whether this is a new record.
func (b *BaseModel) IsNewRecord() bool {
	return b.isNewRecord
}

// KeepItem checks if the record is considered active (not soft-deleted).
func (b *BaseModel) KeepItem() bool {
	return !b.Deleted
}

// SetNewRecordFlag sets the internal isNewRecord flag.
func (b *BaseModel) SetNewRecordFlag(isNew bool) {
	b.isNewRecord = isNew
}
