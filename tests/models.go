package thing

import (
	thing "github.com/burugo/thing"
)

// User is a sample model for testing purposes within the tests package.
type User struct {
	thing.BaseModel
	Name  string `json:"name" db:"name"`
	Email string `json:"email" db:"email"`
	// Books []Book // Example relation, commented out for now
}

// Ensure User implements the Model interface implicitly via BaseModel
var _ thing.Model = (*User)(nil)

// Book is a sample related model (commented out for simplicity now)
// type Book struct {
// 	thing.BaseModel
// 	Title  string `json:"title" db:"title"`
// 	UserID int64  `json:"-" db:"user_id"` // Foreign key
// }
// var _ thing.Model = (*Book)(nil)
