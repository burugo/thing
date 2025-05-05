package thing_test

import (
	"testing"

	"github.com/burugo/thing"
	"github.com/stretchr/testify/assert"
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
		expectFn func(options *thing.JSONOptions) bool
		desc     string
	}{
		{
			dsl: "name,age",
			expectFn: func(options *thing.JSONOptions) bool {
				// 只要求 name, age 顺序
				if len(options.OrderedInclude) != 2 || options.OrderedInclude[0].Name != "name" || options.OrderedInclude[1].Name != "age" {
					return false
				}
				if len(options.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "basic include fields with order (no id default)",
		},
		{
			dsl: "name,-age",
			expectFn: func(options *thing.JSONOptions) bool {
				if len(options.OrderedInclude) != 1 || options.OrderedInclude[0].Name != "name" {
					return false
				}
				if len(options.OrderedExclude) != 1 || options.OrderedExclude[0] != "age" {
					return false
				}
				return true
			},
			desc: "include and exclude fields with order (no id default)",
		},
		{
			dsl: "name,book{title,-publish_at},-deleted",
			expectFn: func(options *thing.JSONOptions) bool {
				if len(options.OrderedInclude) != 2 || options.OrderedInclude[0].Name != "name" || options.OrderedInclude[1].Name != "book" {
					return false
				}
				if len(options.OrderedExclude) != 1 || options.OrderedExclude[0] != "deleted" {
					return false
				}
				bookRule := options.OrderedInclude[1]
				if bookRule.Nested == nil || len(bookRule.Nested.OrderedInclude) != 1 || bookRule.Nested.OrderedInclude[0].Name != "title" || len(bookRule.Nested.OrderedExclude) != 1 || bookRule.Nested.OrderedExclude[0] != "publish_at" {
					return false
				}
				return true
			},
			desc: "nested include/exclude with order (no id default)",
		},
		{
			dsl: "user{name,profile{avatar}},book{author{name}}",
			expectFn: func(options *thing.JSONOptions) bool {
				if len(options.OrderedInclude) != 2 || options.OrderedInclude[0].Name != "user" || options.OrderedInclude[1].Name != "book" {
					return false
				}
				userRule := options.OrderedInclude[0]
				if userRule.Nested == nil || len(userRule.Nested.OrderedInclude) != 2 || userRule.Nested.OrderedInclude[0].Name != "name" || userRule.Nested.OrderedInclude[1].Name != "profile" || len(userRule.Nested.OrderedExclude) != 0 {
					return false
				}
				profileRule := userRule.Nested.OrderedInclude[1]
				if profileRule.Nested == nil || len(profileRule.Nested.OrderedInclude) != 1 || profileRule.Nested.OrderedInclude[0].Name != "avatar" || len(profileRule.Nested.OrderedExclude) != 0 {
					return false
				}
				bookRule := options.OrderedInclude[1]
				if bookRule.Nested == nil || len(bookRule.Nested.OrderedInclude) != 1 || bookRule.Nested.OrderedInclude[0].Name != "author" || len(bookRule.Nested.OrderedExclude) != 0 {
					return false
				}
				authorRule := bookRule.Nested.OrderedInclude[0]
				if authorRule.Nested == nil || len(authorRule.Nested.OrderedInclude) != 1 || authorRule.Nested.OrderedInclude[0].Name != "name" || len(authorRule.Nested.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "multi-level nested fields with order (no id default)",
		},
		{
			dsl: "",
			expectFn: func(options *thing.JSONOptions) bool {
				// 空字符串应无 include
				if len(options.OrderedInclude) != 0 {
					return false
				}
				if len(options.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "empty string: no default id",
		},
		{
			dsl: "-deleted,-updated_at",
			expectFn: func(options *thing.JSONOptions) bool {
				if len(options.OrderedInclude) != 0 {
					return false
				}
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
			desc: "only excludes, no id default",
		},
		{
			dsl: "book{title}",
			expectFn: func(options *thing.JSONOptions) bool {
				if len(options.OrderedInclude) != 1 || options.OrderedInclude[0].Name != "book" {
					return false
				}
				bookRule := options.OrderedInclude[0]
				if bookRule.Nested == nil || len(bookRule.Nested.OrderedInclude) != 1 || bookRule.Nested.OrderedInclude[0].Name != "title" || len(bookRule.Nested.OrderedExclude) != 0 {
					return false
				}
				if len(options.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "only nested include, no id default",
		},
		{
			dsl: "book{author{profile{email}}}",
			expectFn: func(options *thing.JSONOptions) bool {
				if len(options.OrderedInclude) != 1 || options.OrderedInclude[0].Name != "book" {
					return false
				}
				bookRule := options.OrderedInclude[0]
				if bookRule.Nested == nil || len(bookRule.Nested.OrderedInclude) != 1 || bookRule.Nested.OrderedInclude[0].Name != "author" || len(bookRule.Nested.OrderedExclude) != 0 {
					return false
				}
				authorRule := bookRule.Nested.OrderedInclude[0]
				if authorRule.Nested == nil || len(authorRule.Nested.OrderedInclude) != 1 || authorRule.Nested.OrderedInclude[0].Name != "profile" || len(authorRule.Nested.OrderedExclude) != 0 {
					return false
				}
				profileRule := authorRule.Nested.OrderedInclude[0]
				if profileRule.Nested == nil || len(profileRule.Nested.OrderedInclude) != 1 || profileRule.Nested.OrderedInclude[0].Name != "email" || len(profileRule.Nested.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "deeply nested include, no id default",
		},
		{
			dsl: "   name  ,   -deleted   ",
			expectFn: func(options *thing.JSONOptions) bool {
				if len(options.OrderedInclude) != 1 || options.OrderedInclude[0].Name != "name" {
					return false
				}
				if len(options.OrderedExclude) != 1 || options.OrderedExclude[0] != "deleted" {
					return false
				}
				return true
			},
			desc: "whitespace tolerance, no id default",
		},
		{
			dsl: "book{title,-publish_at},book{author}",
			expectFn: func(options *thing.JSONOptions) bool {
				bookRule := options.OrderedInclude[0]
				if bookRule.Nested == nil {
					return false
				}
				if len(bookRule.Nested.OrderedInclude) != 1 || bookRule.Nested.OrderedInclude[0].Name != "title" {
					return false
				}
				if len(bookRule.Nested.OrderedExclude) != 1 || bookRule.Nested.OrderedExclude[0] != "publish_at" {
					return false
				}
				if len(options.OrderedExclude) != 0 {
					return false
				}
				return true
			},
			desc: "nested merge not supported: only first book{...} kept (no id default)",
		},
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
			expect: `{"name":"Alice","books":[{"title":"Go 101"},{"title":"Go Advanced"}]}`,
			desc:   "include name and books.title only (no id default)",
		},
		{
			dsl:    "name,books{title,-publish_at}",
			expect: `{"name":"Alice","books":[{"title":"Go 101"},{"title":"Go Advanced"}]}`,
			desc:   "exclude books.publish_at (no id default)",
		},
		{
			dsl:    "name,books{title,author}",
			expect: `{"name":"Alice","books":[{"title":"Go 101","author":"Bob"},{"title":"Go Advanced","author":"Carol"}]}`,
			desc:   "include books.title and books.author (no id default)",
		},
		{
			dsl:    "-age,books{title}",
			expect: `{"books":[{"title":"Go 101"},{"title":"Go Advanced"}]}`,
			desc:   "exclude age (no id default)",
		},
		{
			dsl:    "books{title,author},-name,-id,-created_at,-updated_at,-deleted",
			expect: `{"books":[{"title":"Go 101","author":"Bob"},{"title":"Go Advanced","author":"Carol"}]}`,
			desc:   "exclude name and base fields including id, include books fields (id not present)",
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
			expect: `{"id":1,"created_at":"0001-01-01T00:00:00Z","updated_at":"0001-01-01T00:00:00Z","deleted":false,"name":"Alice","age":30,"books":[{"id":1,"title":"Go 101","publish_at":"2020","author":"Bob"},{"id":2,"title":"Go Advanced","publish_at":"2021","author":"Carol"}]}`,
			desc:   "empty dsl outputs all fields (no id default)",
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
			assert.JSONEq(t, tt.expect, jsonStr)
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
		expect := `{"name":"Bob","books":[{"title":"Book1"},{"title":"Book2"}]}`
		assert.JSONEq(t, expect, jsonStr)
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
		expect := `{"first_name":"Alice","full_name":"Alice Smith"}`
		assert.JSONEq(t, expect, jsonStr)
	})

	t.Run("WithFields DSL: field and virtual method", func(t *testing.T) {
		jsonBytes, err := userThing.ToJSON(user, thing.WithFields("first_name,full_name"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		jsonStr := string(jsonBytes)
		expect := `{"first_name":"Alice","full_name":"Alice Smith"}`
		assert.JSONEq(t, expect, jsonStr)
	})

	t.Run("Only virtual method", func(t *testing.T) {
		jsonBytes, err := userThing.ToJSON(user, thing.Include("full_name"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		jsonStr := string(jsonBytes)
		expect := `{"full_name":"Alice Smith"}`
		assert.JSONEq(t, expect, jsonStr)
	})

	t.Run("Omit virtual method", func(t *testing.T) {
		jsonBytes, err := userThing.ToJSON(user, thing.Include("first_name"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		jsonStr := string(jsonBytes)
		expect := `{"first_name":"Alice"}`
		assert.JSONEq(t, expect, jsonStr)
	})
}
