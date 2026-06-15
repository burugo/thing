// Package types defines internal data structures shared between thing's
// sub-packages to avoid import cycles.
package types

type QueryParams struct {
	Where          string
	Args           []interface{}
	Order          string
	Preloads       []string
	IncludeDeleted bool
}
