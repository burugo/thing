package schema

import (
	"reflect"
	"testing"
)

// testBaseModel mimics thing.BaseModel for detection tests: it provides a
// promoted KeepItem method via embedding.
type testBaseModel struct {
	ID      int64
	Deleted bool
}

func (b testBaseModel) KeepItem() bool           { return !b.Deleted }
func (b testBaseModel) KeepItemFields() []string { return nil }
func (b testBaseModel) GetID() int64             { return b.ID }

// defaultModel embeds testBaseModel and does NOT override KeepItem.
type defaultModel struct {
	testBaseModel
	Name string
}

// valueOverrideModel overrides KeepItem with a value receiver.
type valueOverrideModel struct {
	testBaseModel
	Hidden bool
}

func (m valueOverrideModel) KeepItem() bool           { return !m.Deleted && !m.Hidden }
func (m valueOverrideModel) KeepItemFields() []string { return []string{"hidden"} }

// ptrOverrideModel overrides KeepItem with a pointer receiver.
type ptrOverrideModel struct {
	testBaseModel
	Spam bool
}

func (m *ptrOverrideModel) KeepItem() bool           { return !m.Deleted && !m.Spam }
func (m *ptrOverrideModel) KeepItemFields() []string { return []string{"spam"} }

// midModel / deepModel exercise two-level embedding (still default).
type midModel struct {
	testBaseModel
}
type deepModel struct {
	midModel
	X int
}

// ptrEmbedModel embeds *testBaseModel (default via pointer embedding).
type ptrEmbedModel struct {
	*testBaseModel
	Name string
}

// standaloneModel declares KeepItem itself without embedding.
type standaloneModel struct {
	ID int64
}

func (m standaloneModel) KeepItem() bool { return true }

// forgotFieldsModel overrides KeepItem but inherits KeepItemFields from the
// embedded base (i.e. the user forgot to declare the dependency fields).
type forgotFieldsModel struct {
	testBaseModel
	Hidden bool
}

func (m forgotFieldsModel) KeepItem() bool { return !m.Deleted && !m.Hidden }

func TestHasCustomKeepItem(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
		want bool
	}{
		{"default value embed", reflect.TypeOf(defaultModel{}), false},
		{"two-level embed", reflect.TypeOf(deepModel{}), false},
		{"pointer embed default", reflect.TypeOf(ptrEmbedModel{}), false},
		{"value receiver override", reflect.TypeOf(valueOverrideModel{}), true},
		{"pointer receiver override", reflect.TypeOf(ptrOverrideModel{}), true},
		{"standalone own method", reflect.TypeOf(standaloneModel{}), true},
		{"override keepitem only", reflect.TypeOf(forgotFieldsModel{}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HasCustomKeepItem(tc.typ)
			if got != tc.want {
				t.Errorf("HasCustomKeepItem(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestHasCustomKeepItemFields(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
		want bool
	}{
		{"default value embed", reflect.TypeOf(defaultModel{}), false},
		{"two-level embed", reflect.TypeOf(deepModel{}), false},
		{"pointer embed default", reflect.TypeOf(ptrEmbedModel{}), false},
		{"value receiver override", reflect.TypeOf(valueOverrideModel{}), true},
		{"pointer receiver override", reflect.TypeOf(ptrOverrideModel{}), true},
		{"override keepitem only (forgot fields)", reflect.TypeOf(forgotFieldsModel{}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HasCustomKeepItemFields(tc.typ)
			if got != tc.want {
				t.Errorf("HasCustomKeepItemFields(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
