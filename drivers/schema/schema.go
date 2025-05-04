package schema

import (
	"context"
)

// IndexInfo holds metadata for a single index (normal or unique).
type IndexInfo struct {
	Name    string   // Index name
	Columns []string // Column names
	Unique  bool     // Is unique index
}

// TableInfo holds the actual schema info introspected from the database.
type TableInfo struct {
	Name       string       // Table name
	Columns    []ColumnInfo // All columns
	Indexes    []IndexInfo  // All indexes (including unique)
	PrimaryKey string       // Primary key column name (if any)
}

// ColumnInfo holds metadata for a single column in a table.
type ColumnInfo struct {
	Name       string  // Column name
	DataType   string  // Database type (e.g., INT, VARCHAR(255))
	IsNullable bool    // Whether the column is nullable
	IsPrimary  bool    // Whether this column is the primary key
	IsUnique   bool    // Whether this column has a unique constraint
	Default    *string // Default value (if any)
}

// Introspector defines the interface for database schema introspection.
type Introspector interface {
	// GetTableInfo introspects the given table and returns its schema info.
	GetTableInfo(ctx context.Context, tableName string) (*TableInfo, error)
}
