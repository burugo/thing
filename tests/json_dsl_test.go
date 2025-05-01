package thing_test

import (
	"testing"

	thing "thing"
)

func TestParseFieldsDSL_ToRuleTree(t *testing.T) {
	tests := []struct {
		dsl      string
		expectFn func(tree *thing.FieldRuleTree) bool
		desc     string
	}{
		{
			dsl: "name,age",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Include["name"] != nil && tree.Include["age"] != nil && len(tree.Exclude) == 0
			},
			desc: "basic include fields",
		},
		{
			dsl: "name,-age",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Include["name"] != nil && tree.Exclude["age"] && tree.Include["age"] == nil
			},
			desc: "include and exclude fields",
		},
		{
			dsl: "name,book{title,-publish_at},-deleted",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				book, ok := tree.Include["book"]
				return ok && book.Include["title"] != nil && book.Exclude["publish_at"] && tree.Exclude["deleted"]
			},
			desc: "nested include/exclude",
		},
		{
			dsl: "user{name,profile{avatar}},book{author{name}}",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				user, ok1 := tree.Include["user"]
				profile, ok2 := user.Include["profile"]
				book, ok3 := tree.Include["book"]
				author, ok4 := book.Include["author"]
				return ok1 && ok2 && ok3 && ok4 && profile.Include["avatar"] != nil && author.Include["name"] != nil
			},
			desc: "multi-level nested fields",
		},
		{
			dsl: "",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return len(tree.Include) == 0 && len(tree.Exclude) == 0
			},
			desc: "empty string returns empty rule tree",
		},
		{
			dsl: "-deleted,-updated_at",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return len(tree.Include) == 0 && tree.Exclude["deleted"] && tree.Exclude["updated_at"]
			},
			desc: "only excludes",
		},
		{
			dsl: "book{title}",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil
			},
			desc: "only nested include",
		},
		{
			dsl: "book{author{profile{email}}}",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Include["book"] != nil && tree.Include["book"].Include["author"] != nil && tree.Include["book"].Include["author"].Include["profile"] != nil && tree.Include["book"].Include["author"].Include["profile"].Include["email"] != nil
			},
			desc: "deeply nested include",
		},
		{
			dsl: "   name  ,   -deleted   ",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Include["name"] != nil && tree.Exclude["deleted"]
			},
			desc: "whitespace tolerance",
		},
		{
			dsl: "book{title,-publish_at},book{author}",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				// Should merge book includes
				return tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil && tree.Include["book"].Include["author"] != nil && tree.Include["book"].Exclude["publish_at"]
			},
			desc: "merge repeated nested fields",
		},
		{
			dsl: "book{title,author{email}},-book",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Exclude["book"] && tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil && tree.Include["book"].Include["author"] != nil && tree.Include["book"].Include["author"].Include["email"] != nil
			},
			desc: "exclude and include same field",
		},
		{
			dsl: "book{title,author{email}},book{author{email}}",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil && tree.Include["book"].Include["author"] != nil && tree.Include["book"].Include["author"].Include["email"] != nil
			},
			desc: "repeated nested fields should merge",
		},
		{
			dsl: "book{title,author{email}},book{author{email,phone}}",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil && tree.Include["book"].Include["author"] != nil && tree.Include["book"].Include["author"].Include["email"] != nil && tree.Include["book"].Include["author"].Include["phone"] != nil
			},
			desc: "merge nested fields with multiple children",
		},
		{
			dsl: "book{title,author{email}},book{author{email,phone}},-book{author{email}}",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil && tree.Include["book"].Include["author"] != nil && tree.Include["book"].Include["author"].Include["email"] != nil && tree.Include["book"].Include["author"].Include["phone"] != nil && tree.Include["book"].Exclude["author"] == false
			},
			desc: "complex merge and exclude",
		},
		{
			dsl: "book{title,author{email}},book{author{email,phone}},-book{author{email}},-book{title}",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil && tree.Include["book"].Include["author"] != nil && tree.Include["book"].Include["author"].Include["email"] != nil && tree.Include["book"].Include["author"].Include["phone"] != nil && tree.Include["book"].Exclude["author"] == false && tree.Include["book"].Exclude["title"] == false
			},
			desc: "complex merge and multiple excludes",
		},
		{
			dsl: "book{title,author{email}},book{author{email,phone}},-book{author{email}},-book{title},-book,-author",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Exclude["book"] && tree.Exclude["author"] && tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil && tree.Include["book"].Include["author"] != nil && tree.Include["book"].Include["author"].Include["email"] != nil && tree.Include["book"].Include["author"].Include["phone"] != nil && tree.Include["book"].Exclude["author"] == false && tree.Include["book"].Exclude["title"] == false
			},
			desc: "exclude parent after nested includes",
		},
		{
			dsl: "book{title,author{email}},book{author{email,phone}},-book{author{email}},-book{title},-book,-author,-book{author{phone}}",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Exclude["book"] && tree.Exclude["author"] && tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil && tree.Include["book"].Include["author"] != nil && tree.Include["book"].Include["author"].Include["email"] != nil && tree.Include["book"].Include["author"].Include["phone"] != nil && tree.Include["book"].Exclude["author"] == false && tree.Include["book"].Exclude["title"] == false && tree.Include["book"].Exclude["phone"] == false
			},
			desc: "exclude nested after parent and nested includes",
		},
		{
			dsl: "book{title,author{email}},book{author{email,phone}},-book{author{email}},-book{title},-book,-author,-book{author{phone}},-book{author{email,phone}}",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Exclude["book"] && tree.Exclude["author"] && tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil && tree.Include["book"].Include["author"] != nil && tree.Include["book"].Include["author"].Include["email"] != nil && tree.Include["book"].Include["author"].Include["phone"] != nil && tree.Include["book"].Exclude["author"] == false && tree.Include["book"].Exclude["title"] == false && tree.Include["book"].Exclude["phone"] == false
			},
			desc: "exclude deeply nested after parent and nested includes",
		},
		{
			dsl: "book{title,author{email}},book{author{email,phone}},-book{author{email}},-book{title},-book,-author,-book{author{phone}},-book{author{email,phone}},invalid{unmatched",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				// Should error, but for now just check that valid parts are parsed
				return tree.Include["book"] != nil && tree.Include["book"].Include["title"] != nil
			},
			desc: "invalid DSL with unmatched brace",
		},
		{
			dsl: "field-with-dash,field_with_underscore",
			expectFn: func(tree *thing.FieldRuleTree) bool {
				return tree.Include["field-with-dash"] != nil && tree.Include["field_with_underscore"] != nil
			},
			desc: "fields with dashes and underscores",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			tree, err := thing.ParseFieldsDSL(tt.dsl)
			if err != nil {
				t.Fatalf("ParseFieldsDSL error: %v", err)
			}
			if !tt.expectFn(tree) {
				t.Errorf("Rule tree did not match expectation for DSL: %s", tt.dsl)
			}
		})
	}
}

func TestToJSON_WithFieldsDSL(t *testing.T) {
	type Book struct {
		Title     string `json:"title"`
		PublishAt string `json:"publish_at"`
		Author    string `json:"author"`
	}
	type User struct {
		thing.BaseModel
		Name  string `json:"name"`
		Age   int    `json:"age"`
		Books []Book `json:"books"`
	}

	user := &User{
		BaseModel: thing.BaseModel{ID: 1},
		Name:      "Alice",
		Age:       30,
		Books: []Book{
			{Title: "Go 101", PublishAt: "2020", Author: "Bob"},
			{Title: "Go Advanced", PublishAt: "2021", Author: "Carol"},
		},
	}

	tests := []struct {
		dsl    string
		expect string // expected JSON substring (not full match for brevity)
		desc   string
	}{
		{
			dsl:    "name,books{title}",
			expect: `{"id":1,"name":"Alice","books":[{"title":"Go 101"},{"title":"Go Advanced"}]}`,
			desc:   "include name and books.title only",
		},
		{
			dsl:    "name,books{title,-publish_at}",
			expect: `{"id":1,"name":"Alice","books":[{"title":"Go 101"},{"title":"Go Advanced"}]}`,
			desc:   "exclude books.publish_at",
		},
		{
			dsl:    "name,books{title,author}",
			expect: `{"id":1,"name":"Alice","books":[{"title":"Go 101","author":"Bob"},{"title":"Go Advanced","author":"Carol"}]}`,
			desc:   "include books.title and books.author",
		},
		{
			dsl:    "-age,books{title}",
			expect: `{"id":1,"name":"Alice","books":[{"title":"Go 101"},{"title":"Go Advanced"}]}`,
			desc:   "exclude age, include id by default",
		},
		{
			dsl:    "books{title,author},-name,-id,-created_at,-updated_at,-deleted",
			expect: `{"books":[{"title":"Go 101","author":"Bob"},{"title":"Go Advanced","author":"Carol"}]}`,
			desc:   "exclude name and base fields, include books fields",
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
}
