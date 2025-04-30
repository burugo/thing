package thing_test

import (
	"thing"
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
