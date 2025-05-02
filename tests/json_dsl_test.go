package thing_test

import (
	"strings"
	"testing"

	"github.com/burugo/thing"
)

// Test with struct that has no json tags
type SimpleBook struct {
	ID     int
	Title  string
	Author string
}
type SimpleUser struct {
	ID    int
	Name  string
	Books []SimpleBook
}

// Implement thing.Model interface
func (u *SimpleUser) GetID() int64   { return int64(u.ID) }
func (u *SimpleUser) KeepItem() bool { return true }

func TestParseFieldsDSL_ToJsonOptions(t *testing.T) {
	tests := []struct {
		dsl      string
		expectFn func(options *thing.JsonOptions) bool
		desc     string
	}{
		{
			dsl: "name,age",
			expectFn: func(options *thing.JsonOptions) bool {
				// Check inclusion and order - Expect id prepended
				if len(options.OrderedInclude) != 3 || options.OrderedInclude[0].Name != "id" || options.OrderedInclude[1].Name != "name" || options.OrderedInclude[2].Name != "age" {
					return false
				}
				// Check exclusion
				if len(options.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "basic include fields with order (id defaulted)",
		},
		{
			dsl: "name,-age",
			expectFn: func(options *thing.JsonOptions) bool {
				// Check inclusion and order - Expect id prepended
				if len(options.OrderedInclude) != 2 || options.OrderedInclude[0].Name != "id" || options.OrderedInclude[1].Name != "name" {
					return false
				}
				// Check exclusion
				if len(options.OrderedExclude) != 1 || options.OrderedExclude[0] != "age" {
					return false
				}
				return true
			},
			desc: "include and exclude fields with order (id defaulted)",
		},
		{
			dsl: "name,book{title,-publish_at},-deleted",
			expectFn: func(options *thing.JsonOptions) bool {
				// Expected: id, name, book (include), deleted (exclude)
				if len(options.OrderedInclude) != 3 || options.OrderedInclude[0].Name != "id" || options.OrderedInclude[1].Name != "name" || options.OrderedInclude[2].Name != "book" {
					return false
				}
				if len(options.OrderedExclude) != 1 || options.OrderedExclude[0] != "deleted" {
					return false
				}
				// Check nested book rules: id should be prepended
				bookRule := options.OrderedInclude[2]
				if bookRule.Nested == nil || len(bookRule.Nested.OrderedInclude) != 2 || bookRule.Nested.OrderedInclude[0].Name != "id" || bookRule.Nested.OrderedInclude[1].Name != "title" || len(bookRule.Nested.OrderedExclude) != 1 || bookRule.Nested.OrderedExclude[0] != "publish_at" {
					return false
				}
				return true
			},
			desc: "nested include/exclude with order (id defaulted in nested)",
		},
		{
			dsl: "user{name,profile{avatar}},book{author{name}}",
			expectFn: func(options *thing.JsonOptions) bool {
				// Expected top level: id, user, book
				if len(options.OrderedInclude) != 3 || options.OrderedInclude[0].Name != "id" || options.OrderedInclude[1].Name != "user" || options.OrderedInclude[2].Name != "book" {
					return false
				}
				// Check user nested: id should be prepended
				userRule := options.OrderedInclude[1]
				if userRule.Nested == nil || len(userRule.Nested.OrderedInclude) != 3 || userRule.Nested.OrderedInclude[0].Name != "id" || userRule.Nested.OrderedInclude[1].Name != "name" || userRule.Nested.OrderedInclude[2].Name != "profile" || len(userRule.Nested.OrderedExclude) != 0 {
					return false
				}
				// Check profile nested under user: id should be prepended
				profileRule := userRule.Nested.OrderedInclude[2]
				if profileRule.Nested == nil || len(profileRule.Nested.OrderedInclude) != 2 || profileRule.Nested.OrderedInclude[0].Name != "id" || profileRule.Nested.OrderedInclude[1].Name != "avatar" || len(profileRule.Nested.OrderedExclude) != 0 {
					return false
				}
				// Check book nested: id should be prepended
				bookRule := options.OrderedInclude[2]
				if bookRule.Nested == nil || len(bookRule.Nested.OrderedInclude) != 2 || bookRule.Nested.OrderedInclude[0].Name != "id" || bookRule.Nested.OrderedInclude[1].Name != "author" || len(bookRule.Nested.OrderedExclude) != 0 {
					return false
				}
				// Check author nested under book: id should be prepended
				authorRule := bookRule.Nested.OrderedInclude[1]
				if authorRule.Nested == nil || len(authorRule.Nested.OrderedInclude) != 2 || authorRule.Nested.OrderedInclude[0].Name != "id" || authorRule.Nested.OrderedInclude[1].Name != "name" || len(authorRule.Nested.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "multi-level nested fields with order (id defaulted in all levels)",
		},
		{
			dsl: "",
			expectFn: func(options *thing.JsonOptions) bool {
				// Default: only 'id' included
				if len(options.OrderedInclude) != 1 || options.OrderedInclude[0].Name != "id" {
					return false
				}
				if len(options.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "empty string defaults to id",
		},
		{
			dsl: "-deleted,-updated_at",
			expectFn: func(options *thing.JsonOptions) bool {
				// Default 'id' included, plus specified excludes
				if len(options.OrderedInclude) != 1 || options.OrderedInclude[0].Name != "id" {
					return false
				}
				// Check excludes (order doesn't matter for excludes list assertion)
				expectedExcludes := map[string]bool{"deleted": true, "updated_at": true}
				if len(options.OrderedExclude) != len(expectedExcludes) {
					return false
				}
				for _, excluded := range options.OrderedExclude {
					if !expectedExcludes[excluded] {
						return false
					}
				}
				return true
			},
			desc: "only excludes, id defaulted",
		},
		{
			dsl: "book{title}",
			expectFn: func(options *thing.JsonOptions) bool {
				// Default 'id' included, plus book nested
				if len(options.OrderedInclude) != 2 || options.OrderedInclude[0].Name != "id" || options.OrderedInclude[1].Name != "book" {
					return false
				}
				// Check nested book rules: id should be prepended
				bookRule := options.OrderedInclude[1]
				if bookRule.Nested == nil || len(bookRule.Nested.OrderedInclude) != 2 || bookRule.Nested.OrderedInclude[0].Name != "id" || bookRule.Nested.OrderedInclude[1].Name != "title" || len(bookRule.Nested.OrderedExclude) != 0 {
					return false
				}
				if len(options.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "only nested include, id defaulted in nested",
		},
		{
			dsl: "book{author{profile{email}}}",
			expectFn: func(options *thing.JsonOptions) bool {
				// Default 'id' included, plus deeply nested
				if len(options.OrderedInclude) != 2 || options.OrderedInclude[0].Name != "id" || options.OrderedInclude[1].Name != "book" {
					return false
				}
				// Check nested book: id should be prepended
				bookRule := options.OrderedInclude[1]
				if bookRule.Nested == nil || len(bookRule.Nested.OrderedInclude) != 2 || bookRule.Nested.OrderedInclude[0].Name != "id" || bookRule.Nested.OrderedInclude[1].Name != "author" || len(bookRule.Nested.OrderedExclude) != 0 {
					return false
				}
				// Check nested author: id should be prepended
				authorRule := bookRule.Nested.OrderedInclude[1]
				if authorRule.Nested == nil || len(authorRule.Nested.OrderedInclude) != 2 || authorRule.Nested.OrderedInclude[0].Name != "id" || authorRule.Nested.OrderedInclude[1].Name != "profile" || len(authorRule.Nested.OrderedExclude) != 0 {
					return false
				}
				// Check nested profile: id should be prepended
				profileRule := authorRule.Nested.OrderedInclude[1]
				if profileRule.Nested == nil || len(profileRule.Nested.OrderedInclude) != 2 || profileRule.Nested.OrderedInclude[0].Name != "id" || profileRule.Nested.OrderedInclude[1].Name != "email" || len(profileRule.Nested.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "deeply nested include with id defaulted in all levels",
		},
		{
			dsl: "   name  ,   -deleted   ",
			expectFn: func(options *thing.JsonOptions) bool {
				// Default 'id' included, plus name, plus deleted exclude
				if len(options.OrderedInclude) != 2 || options.OrderedInclude[0].Name != "id" || options.OrderedInclude[1].Name != "name" {
					return false
				}
				// Check excludes
				if len(options.OrderedExclude) != 1 || options.OrderedExclude[0] != "deleted" {
					return false
				}
				return true
			},
			desc: "whitespace tolerance with id defaulted",
		},
		{
			dsl: "book{title,-publish_at},book{author}",
			expectFn: func(options *thing.JsonOptions) bool {
				// Only the first occurrence of book{} is kept; merging is not supported.
				bookRule := options.OrderedInclude[1]
				if bookRule.Nested == nil {
					return false
				}
				// Should only have id and title (not author)
				if len(bookRule.Nested.OrderedInclude) != 2 || bookRule.Nested.OrderedInclude[0].Name != "id" || bookRule.Nested.OrderedInclude[1].Name != "title" {
					return false
				}
				if len(bookRule.Nested.OrderedExclude) != 1 || bookRule.Nested.OrderedExclude[0] != "publish_at" {
					return false
				}
				if len(options.OrderedExclude) != 0 {
					return false
				}
				return true
				// No merge: author is ignored
			},
			desc: "nested merge not supported: only first book{...} kept (id defaulted in nested)",
		},
		// Add more test cases for edge cases, invalid DSL, etc.
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			options, err := thing.ParseFieldsDSL(tt.dsl)
			if err != nil {
				t.Fatalf("ParseFieldsDSL error for DSL \"%s\": %v", tt.dsl, err)
			}
			if !tt.expectFn(options) {
				t.Errorf("For DSL '%s', expected conditions not met. Got options: %+v", tt.dsl, options)
			}
		})
	}
}

func TestToJSON_WithFieldsDSL(t *testing.T) {
	type Book struct {
		ID        int
		Title     string
		PublishAt string
		Author    string
	}
	type User struct {
		thing.BaseModel
		Name  string
		Age   int
		Books []Book
	}

	user := &User{
		BaseModel: thing.BaseModel{ID: 1},
		Name:      "Alice",
		Age:       30,
		Books: []Book{
			{ID: 1, Title: "Go 101", PublishAt: "2020", Author: "Bob"},
			{ID: 2, Title: "Go Advanced", PublishAt: "2021", Author: "Carol"},
		},
	}

	simpleUser := &SimpleUser{
		ID:   42,
		Name: "Bob",
		Books: []SimpleBook{
			{ID: 1, Title: "Book1", Author: "A"},
			{ID: 2, Title: "Book2", Author: "B"},
		},
	}

	tests := []struct {
		dsl    string
		expect string // expected JSON string with correct order
		desc   string
	}{
		{
			dsl:    "name,books{title}",
			expect: `{"id":1,"name":"Alice","books":[{"id":1,"title":"Go 101"},{"id":2,"title":"Go Advanced"}]}`,
			desc:   "include name and books.title only, with id defaulted",
		},
		{
			dsl:    "name,books{title,-publish_at}",
			expect: `{"id":1,"name":"Alice","books":[{"id":1,"title":"Go 101"},{"id":2,"title":"Go Advanced"}]}`,
			desc:   "exclude books.publish_at, with id defaulted",
		},
		{
			dsl:    "name,books{title,author}",
			expect: `{"id":1,"name":"Alice","books":[{"id":1,"title":"Go 101","author":"Bob"},{"id":2,"title":"Go Advanced","author":"Carol"}]}`,
			desc:   "include books.title and books.author, with id defaulted",
		},
		{
			dsl:    "-age,books{title}",
			expect: `{"id":1,"books":[{"id":1,"title":"Go 101"},{"id":2,"title":"Go Advanced"}]}`,
			desc:   "exclude age, include id by default",
		},
		{
			dsl:    "books{title,author},-name,-id,-created_at,-updated_at,-deleted",
			expect: `{"books":[{"id":1,"title":"Go 101","author":"Bob"},{"id":2,"title":"Go Advanced","author":"Carol"}]}`,
			desc:   "exclude name and base fields including id, include books fields (id present in nested books)",
		},
		{
			dsl:    "age,name,id",
			expect: `{"age":30,"name":"Alice","id":1}`,
			desc:   "explicitly include age, name, id in specified order",
		},
		{
			dsl:    "id,age,name",
			expect: `{"id":1,"age":30,"name":"Alice"}`,
			desc:   "explicitly include id, age, name in specified order",
		},
		{
			dsl:    "",
			expect: `{"id":1}`,
			desc:   "empty dsl defaults to id",
		},
	}

	th := thing.Thing[*User]{}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			jsonBytes, err := th.ToJSON(user, thing.WithFields(tt.dsl))
			if err != nil {
				t.Fatalf("ToJSON error: %v", err)
			}
			jsonStr := string(jsonBytes)
			if jsonStr != tt.expect {
				t.Errorf("ToJSON output mismatch for DSL %q\nGot:  %s\nWant: %s", tt.dsl, jsonStr, tt.expect)
			}
		})
	}

	// New: test no struct tag, only DSL
	simpleTh := thing.Thing[*SimpleUser]{}
	t.Run("no struct tag, dsl controls order and nesting", func(t *testing.T) {
		jsonBytes, err := simpleTh.ToJSON(simpleUser, thing.WithFields("name,books{title}"))
		if err != nil {
			t.Fatalf("ToJSON error: %v", err)
		}
		jsonStr := string(jsonBytes)
		expect := `{"id":42,"name":"Bob","books":[{"id":1,"title":"Book1"},{"id":2,"title":"Book2"}]}`
		if jsonStr != expect {
			t.Errorf("ToJSON output mismatch for no struct tag DSL\nGot:  %s\nWant: %s", jsonStr, expect)
		}
	})
}

// --- Method-based virtuals test types ---
type MethodUser struct {
	ID        int
	FirstName string
	LastName  string
}

// Implement thing.Model interface
func (u *MethodUser) GetID() int64   { return int64(u.ID) }
func (u *MethodUser) KeepItem() bool { return true }

// Method-based virtual
func (u *MethodUser) FullName() string {
	return u.FirstName + " " + u.LastName
}

func TestToJSON_MethodBasedVirtuals(t *testing.T) {
	user := &MethodUser{ID: 1, FirstName: "Alice", LastName: "Smith"}
	userThing := thing.Thing[*MethodUser]{}

	t.Run("Include field and virtual method", func(t *testing.T) {
		jsonBytes, err := userThing.ToJSON(user, thing.Include("first_name", "full_name"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		jsonStr := string(jsonBytes)
		if !(strings.Contains(jsonStr, "first_name") && strings.Contains(jsonStr, "full_name") && strings.Contains(jsonStr, "Alice Smith")) {
			t.Errorf("expected both field and method output, got: %s", jsonStr)
		}
	})

	t.Run("WithFields DSL: field and virtual method", func(t *testing.T) {
		jsonBytes, err := userThing.ToJSON(user, thing.WithFields("first_name,full_name"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		jsonStr := string(jsonBytes)
		if !(strings.Contains(jsonStr, "first_name") && strings.Contains(jsonStr, "full_name") && strings.Contains(jsonStr, "Alice Smith")) {
			t.Errorf("expected both field and method output, got: %s", jsonStr)
		}
	})

	t.Run("Only virtual method", func(t *testing.T) {
		jsonBytes, err := userThing.ToJSON(user, thing.Include("full_name"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		jsonStr := string(jsonBytes)
		if !(strings.Contains(jsonStr, "full_name") && strings.Contains(jsonStr, "Alice Smith")) || strings.Contains(jsonStr, "first_name") {
			t.Errorf("expected only method output, got: %s", jsonStr)
		}
	})

	t.Run("Omit virtual method", func(t *testing.T) {
		jsonBytes, err := userThing.ToJSON(user, thing.Include("first_name"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		jsonStr := string(jsonBytes)
		if strings.Contains(jsonStr, "full_name") || strings.Contains(jsonStr, "Alice Smith") {
			t.Errorf("expected no method output, got: %s", jsonStr)
		}
	})
}
