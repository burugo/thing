package types

// --- Local Interfaces/Structs/Vars for internal/cache --- //
// Local QueryParams matching root structure
type QueryParams struct {
	Where          string
	Args           []interface{}
	Order          string
	Preloads       []string
	IncludeDeleted bool
}
