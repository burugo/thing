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



