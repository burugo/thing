package dbscan

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
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

func TestPrepareScanDestScansSQLiteRowsWithNullableAndBasicValues(t *testing.T) {
	type rowModel struct {
		ID        int64     `db:"id"`
		CreatedAt time.Time `db:"created_at"`
		Payload   []byte    `db:"payload"`
		Nickname  *string   `db:"nickname"`
		Empty     *string   `db:"empty_value"`
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE scan_rows (
			id INTEGER PRIMARY KEY,
			created_at DATETIME,
			payload BLOB,
			nickname TEXT,
			empty_value TEXT
		)
	`)
	if err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}

	now := time.Date(2026, 6, 11, 12, 30, 0, 0, time.UTC)
	_, err = db.Exec(
		`INSERT INTO scan_rows (id, created_at, payload, nickname, empty_value) VALUES (?, ?, ?, ?, ?)`,
		int64(7),
		now,
		[]byte("payload"),
		"neo",
		nil,
	)
	if err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}

	rows, err := db.Query(`SELECT id, created_at, payload, nickname, empty_value, 'discarded' AS unknown_column FROM scan_rows`)
	if err != nil {
		t.Fatalf("SELECT returned error: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("Rows.Columns returned error: %v", err)
	}
	if !rows.Next() {
		t.Fatal("expected one row")
	}

	var model rowModel
	dest, err := PrepareScanDest(reflect.ValueOf(&model).Elem(), cols)
	if err != nil {
		t.Fatalf("PrepareScanDest returned error: %v", err)
	}
	if err := rows.Scan(dest...); err != nil {
		t.Fatalf("Rows.Scan returned error: %v", err)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Rows.Err returned error: %v", err)
	}

	if model.ID != 7 {
		t.Fatalf("expected ID 7, got %d", model.ID)
	}
	if model.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be scanned")
	}
	if string(model.Payload) != "payload" {
		t.Fatalf("expected payload bytes, got %q", string(model.Payload))
	}
	if model.Nickname == nil || *model.Nickname != "neo" {
		t.Fatalf("expected Nickname to be scanned, got %#v", model.Nickname)
	}
	if model.Empty != nil {
		t.Fatalf("expected NULL string pointer to remain nil, got %#v", model.Empty)
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
