package schema

import (
	"reflect"
	"testing"
)

// mockModelInfo returns a minimal *ModelInfo for testing
func mockModelInfo() *ModelInfo {
	return &ModelInfo{
		TableName: "users",
		PkName:    "id",
		CompareFields: []ComparableFieldInfo{
			{GoName: "ID", DBColumn: "id", Kind: reflect.Int64, Type: reflect.TypeOf(int64(0))},
			{GoName: "Name", DBColumn: "name", Kind: reflect.String, Type: reflect.TypeOf("")},
			{GoName: "Email", DBColumn: "email", Kind: reflect.String, Type: reflect.TypeOf("")},
		},
		UniqueIndexes: []IndexInfo{{Columns: []string{"email"}, Unique: true}},
	}
}

func mockTableInfo() *TableInfo {
	return &TableInfo{
		Name: "users",
		Columns: []ColumnInfo{
			{Name: "id", DataType: "INTEGER", IsPrimary: true},
			{Name: "username", DataType: "TEXT"},               // will be dropped
			{Name: "email", DataType: "TEXT", IsUnique: false}, // will become unique
		},
		Indexes:    []IndexInfo{},
		PrimaryKey: "id",
	}
}

func TestGenerateAlterTableSQL(t *testing.T) {
	modelInfo := mockModelInfo()
	tableInfo := mockTableInfo()

	sqls, err := GenerateAlterTableSQL(modelInfo, tableInfo, "sqlite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundAdd := false
	foundDrop := false
	foundUnique := false
	for _, sql := range sqls {
		if sql == "ALTER TABLE users ADD COLUMN name TEXT" {
			foundAdd = true
		}
		if sql == "-- [manual] DROP COLUMN username from users (SQLite needs table rebuild)" {
			foundDrop = true
		}
		if sql == "CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_email ON users (email)" {
			foundUnique = true
		}
	}
	if !foundAdd {
		t.Error("expected add column for 'name'")
	}
	if !foundDrop {
		t.Error("expected drop column for 'username'")
	}
	if !foundUnique {
		t.Error("expected unique index for 'email'")
	}
}

func TestGenerateAlterTableSQL_MySQL(t *testing.T) {
	modelInfo := mockModelInfo()
	tableInfo := mockTableInfo()

	sqls, err := GenerateAlterTableSQL(modelInfo, tableInfo, "mysql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundAdd := false
	foundDrop := false
	foundUnique := false
	for _, sql := range sqls {
		if sql == "ALTER TABLE users ADD COLUMN name VARCHAR(255)" {
			foundAdd = true
		}
		if sql == "ALTER TABLE users DROP COLUMN username" {
			foundDrop = true
		}
		if sql == "CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_email ON users (email)" {
			foundUnique = true
		}
	}
	if !foundAdd {
		t.Error("expected add column for 'name' (MySQL)")
	}
	if !foundDrop {
		t.Error("expected drop column for 'username' (MySQL)")
	}
	if !foundUnique {
		t.Error("expected unique index for 'email' (MySQL)")
	}
}

func TestGenerateAlterTableSQL_Postgres(t *testing.T) {
	modelInfo := mockModelInfo()
	tableInfo := mockTableInfo()

	sqls, err := GenerateAlterTableSQL(modelInfo, tableInfo, "postgres")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundAdd := false
	foundDrop := false
	foundUnique := false
	for _, sql := range sqls {
		if sql == "ALTER TABLE users ADD COLUMN name VARCHAR(255)" {
			foundAdd = true
		}
		if sql == "ALTER TABLE users DROP COLUMN username" {
			foundDrop = true
		}
		if sql == "CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_email ON users (email)" {
			foundUnique = true
		}
	}
	if !foundAdd {
		t.Error("expected add column for 'name' (Postgres)")
	}
	if !foundDrop {
		t.Error("expected drop column for 'username' (Postgres)")
	}
	if !foundUnique {
		t.Error("expected unique index for 'email' (Postgres)")
	}
}
