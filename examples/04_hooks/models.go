package main

import (
	"thing"
)

// User is a simplified model for the hooks example.
type User struct {
	thing.BaseModel        // Embedding BaseModel provides ID, CreatedAt, UpdatedAt, etc.
	Name            string `json:"name" db:"name"`
	Email           string `json:"email" db:"email"`
}

// CreateTableSQL generates the SQL statement to create the users table.
// This is a helper specific to this model for the example.
func (u User) CreateTableSQL() string {
	return `CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at DATETIME,
		updated_at DATETIME,
		deleted BOOLEAN DEFAULT FALSE,
		name TEXT,
		email TEXT
	);`
}

// Ensure User implements the Model interface implicitly via BaseModel
var _ thing.Model = (*User)(nil)
