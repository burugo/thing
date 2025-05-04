// Standalone example: Basic CRUD operations with Thing ORM
// Note: Only one main.go can be run at a time in the examples directory.
// To run this example, rename this file to main.go or run with:
// go run examples/01_basic_crud.go
package main

import (
	"fmt"
	"log"

	"github.com/burugo/thing"
)

// User model definition
// Only basic fields for demonstration
// No relationships

type User struct {
	thing.BaseModel
	Name string `db:"name"`
	Age  int    `db:"age"`
}

func main() {

	// Configure Thing ORM (in-memory DB and cache for demo)
	thing.Configure()

	// Auto-migrate User table
	if err := thing.AutoMigrate(&User{}); err != nil {
		log.Fatal(err)
	}

	// Get the User ORM object
	users, err := thing.Use[*User]()

	// Create
	u := &User{Name: "Alice", Age: 30}
	if err := users.Save(u); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Created:", u)

	// ByID
	found, err := users.ByID(u.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("ByID:", found)

	// Update
	found.Age = 31
	if err := users.Save(found); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Updated:", found)

	// Query all
	result, err := users.Query(thing.QueryParams{})
	if err != nil {
		log.Fatal(err)
	}
	all, err := result.All()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("All users:", all)

	// Delete
	if err := users.Delete(u); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Deleted user.")
}
