# thing

## Flexible JSON Serialization (Key Feature)

- **Supports Go struct tag**  
  Use standard `json:"name,omitempty"`, `json:"-"` tags for default serialization rules, fully compatible with `encoding/json`.
- **Dynamic field control (DSL/WithFields)**  
  Specify included/excluded fields, nested relationships, and output order at runtime using a simple DSL string or `WithFields` API.
- **Clear priority**  
  **Struct tag rules (e.g., `json:"-"`, `json:"name"`) always take precedence over DSL/WithFields.** If a struct tag excludes a field (e.g., `json:"-"`), it will never be output, even if the DSL includes it. DSL/WithFields dynamic rules control output order and inclusion/exclusion for all fields allowed by struct tags. Struct tags provide the ultimate allow/deny list; DSL/WithFields provides dynamic, ordered, and nested control within those constraints.
- **Order guaranteed**  
  Output JSON field order strictly follows the DSL, meeting frontend/API spec requirements.
- **Recursive nested support**  
  Control nested objects/relationship fields recursively, including their output order and content.
- **No struct tag required**  
  Even without any `json` struct tags, you can serialize to JSON with full field control and ordering. Struct tags are optional and only needed for default rules or special cases.

### Example

// Model definition (with struct tags, optional)
type User struct {
    ID   int    `json:"id"`
    Name string `json:"name,omitempty"`
    // ...
}

// Model definition (no struct tags)
type SimpleUser struct {
    ID   int
    Name string
    // ...
}

// Default serialization follows struct tag (if present)
json.Marshal(user)

// Flexible, ordered, nested output (works with or without struct tags)
// Note: ToJSON is called on a Thing[*User] or Thing[*SimpleUser] instance (e.g., userThing)
userThing.ToJSON(WithFieldsDSL("name,profile{avatar},-id"))
simpleUserThing.ToJSON(WithFieldsDSL("name,-id"))

## Method-based Virtual Properties (Advanced JSON Serialization)

You can define computed (virtual) fields on your model by adding exported, zero-argument, single-return-value methods. These methods will only be included in the JSON output if you explicitly reference their corresponding field name in the DSL string passed to `ToJSON`.

- **Method Naming:** Use Go's exported method naming (e.g., `FullName`). The field name in the DSL should be the snake_case version (e.g., `full_name`).
- **How it works:**
    - If the DSL includes a field name that matches a method (converted to snake_case), the method will be called and its return value included in the output.
    - If the DSL does not mention the virtual field, it will not be output.

**Example:**

```go
// Model definition
 type User struct {
     FirstName string
     LastName  string
 }

 // Virtual property method
 func (u *User) FullName() string {
     return u.FirstName + " " + u.LastName
 }

 user := &User{FirstName: "Alice", LastName: "Smith"}
 jsonBytes, _ := thing.ToJSON(user, thing.WithFields("first_name,full_name"))
 fmt.Println(string(jsonBytes))
 // Output: {"first_name":"Alice","full_name":"Alice Smith"}
```

- If you omit `full_name` from the DSL, the `FullName()` method will not be called or included in the output.

This approach gives you full control over which computed fields are exposed, and ensures only explicitly requested virtuals are included in the JSON output.

## Simple Field Inclusion: `Include(fields ...string)`

For most use cases, you can use the `Include` function to specify exactly which fields to output, in order, using plain Go string arguments:

```go
thing.ToJSON(user, thing.Include("id", "name", "full_name"))
```

- This is equivalent to `WithFields("id,name,full_name")` but is more Go-idiomatic and avoids DSL syntax for simple cases.
- You can use this to output method-based virtuals as well:

```go
thing.ToJSON(user, thing.Include("first_name", "full_name"))
```

- If you need nested, exclude, or advanced DSL features, use `WithFields`:

```go
thing.ToJSON(user, thing.WithFields("name,profile{avatar},-id"))
```

- Both `Include` and `WithFields` are fully compatible with struct tags and method-based virtuals as described above.



