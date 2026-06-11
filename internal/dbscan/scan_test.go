package dbscan

import (
	"database/sql"
	"reflect"
	"testing"
	"time"
)

type ScanBaseModel struct {
	ID        int64     `db:"id"`
	CreatedAt time.Time `db:"created_at"`
}

type scanModel struct {
	ScanBaseModel
	Name    string `db:"name"`
	Count   int
	Ignored string `db:"-"`
}

func TestPrepareScanDestMapsColumnsToStructFields(t *testing.T) {
	var model scanModel

	dest, err := PrepareScanDest(reflect.ValueOf(&model).Elem(), []string{"id", "name", "count", "missing"})
	if err != nil {
		t.Fatalf("PrepareScanDest returned error: %v", err)
	}
	if len(dest) != 4 {
		t.Fatalf("expected 4 destinations, got %d", len(dest))
	}

	*dest[0].(*int64) = 42
	*dest[1].(*string) = "alice"
	*dest[2].(*int) = 7
	if _, ok := dest[3].(*sql.RawBytes); !ok {
		t.Fatalf("expected unknown column to scan into *sql.RawBytes, got %T", dest[3])
	}

	if model.ID != 42 || model.Name != "alice" || model.Count != 7 {
		t.Fatalf("scan destinations wrote unexpected model values: %+v", model)
	}
}

func TestPrepareScanDestPrefersOuterFieldOverEmbeddedField(t *testing.T) {
	type Embedded struct {
		Name string `db:"name"`
	}
	type model struct {
		Embedded
		Name string `db:"name"`
	}

	var item model
	dest, err := PrepareScanDest(reflect.ValueOf(&item).Elem(), []string{"name"})
	if err != nil {
		t.Fatalf("PrepareScanDest returned error: %v", err)
	}
	*dest[0].(*string) = "outer"

	if item.Name != "outer" {
		t.Fatalf("expected outer field to be set, got %q", item.Name)
	}
	if item.Embedded.Name != "" {
		t.Fatalf("expected embedded field to remain untouched, got %q", item.Embedded.Name)
	}
}

func TestIsBasicType(t *testing.T) {
	if !IsBasicType(reflect.TypeOf(time.Time{})) {
		t.Fatal("expected time.Time to be a basic scan type")
	}
	if !IsBasicType(reflect.TypeOf([]byte{})) {
		t.Fatal("expected []byte to be a basic scan type")
	}
	if !IsBasicType(reflect.TypeOf((*int)(nil))) {
		t.Fatal("expected pointer to basic type to be a basic scan type")
	}
	if IsBasicType(reflect.TypeOf(scanModel{})) {
		t.Fatal("did not expect struct model to be a basic scan type")
	}
}
