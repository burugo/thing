package thing_test

import (
	"reflect"
	"strings"
	"testing"
	"thing/internal/schema" // 导入 internal 包
)

// mockModelInfo returns a minimal *schema.ModelInfo for testing
func mockModelInfo() *schema.ModelInfo {
	return &schema.ModelInfo{
		TableName: "users",
		PkName:    "id",
		CompareFields: []schema.ComparableFieldInfo{
			{GoName: "ID", DBColumn: "id", Kind: reflect.Int64, Type: reflect.TypeOf(int64(0))},
			{GoName: "Name", DBColumn: "name", Kind: reflect.String, Type: reflect.TypeOf("")},
			{GoName: "Email", DBColumn: "email", Kind: reflect.String, Type: reflect.TypeOf("")},
		},
		UniqueIndexes: []schema.IndexInfo{{Columns: []string{"email"}, Unique: true}},
	}
}

func mockTableInfo() *schema.TableInfo {
	return &schema.TableInfo{
		Name: "users",
		Columns: []schema.ColumnInfo{
			{Name: "id", DataType: "INTEGER", IsPrimary: true},
			{Name: "username", DataType: "TEXT"},               // will be dropped
			{Name: "email", DataType: "TEXT", IsUnique: false}, // will become unique
		},
		Indexes:    []schema.IndexInfo{},
		PrimaryKey: "id",
	}
}

func TestGenerateAlterTableSQL(t *testing.T) {
	modelInfo := mockModelInfo()
	tableInfo := mockTableInfo()

	sqls, err := schema.GenerateAlterTableSQL(modelInfo, tableInfo, "sqlite")
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

	sqls, err := schema.GenerateAlterTableSQL(modelInfo, tableInfo, "mysql")
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

	sqls, err := schema.GenerateAlterTableSQL(modelInfo, tableInfo, "postgres")
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

func TestGenerateMigrationsTableSQL(t *testing.T) {
	dialects := []string{"mysql", "postgres", "sqlite"}
	expects := []string{"CREATE TABLE IF NOT EXISTS schema_migrations", "version", "applied_at"}
	for _, d := range dialects {
		sql, err := schema.GenerateMigrationsTableSQL(d)
		if err != nil {
			t.Errorf("unexpected error for %s: %v", d, err)
		}
		for _, expect := range expects {
			if !contains(sql, expect) {
				t.Errorf("%s: expected SQL to contain %q, got: %s", d, expect, sql)
			}
		}
	}
	// 不支持的方言
	_, err := schema.GenerateMigrationsTableSQL("oracle")
	if err == nil {
		t.Error("expected error for unsupported dialect")
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
