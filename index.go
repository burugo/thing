package thing

// Index describes a model-declared database index.
type Index struct {
	Name    string
	Columns []string
	Unique  bool
	Where   string
}

// IndexProvider is implemented by models that declare programmatic indexes.
type IndexProvider interface {
	Indexes() []Index
}
