package thing_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/burugo/thing"

	"github.com/burugo/thing/drivers/db/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Model for AutoMigrate drop column test
type AutoMigrateTestModel struct {
	thing.BaseModel
	Name        string `db:"name"`
	Email       string `db:"email"`       // This column will be "dropped"
	Age         int    `db:"age"`         // This column will be "dropped" in the second part
	Description string `db:"description"` // This column will be "dropped" in the second part
}

func (m *AutoMigrateTestModel) TableName() string {
	return "auto_migrate_test_models"
}

// Model for AutoMigrate drop column test - after dropping Email
type AutoMigrateTestModelNoEmail struct {
	thing.BaseModel
	Name        string `db:"name"`
	Age         int    `db:"age"`
	Description string `db:"description"`
}

func (m *AutoMigrateTestModelNoEmail) TableName() string {
	return "auto_migrate_test_models"
}

// Model for AutoMigrate drop column test - after dropping Email, Age, and Description
type AutoMigrateTestModelOnlyName struct {
	thing.BaseModel
	Name string `db:"name"`
}

func (m *AutoMigrateTestModelOnlyName) TableName() string {
	return "auto_migrate_test_models"
}

func TestAutoMigrate_DropColumnFeature(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "automigrate_test_")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_automigrate_drop.db")
	dbAdapter, err := sqlite.NewSQLiteAdapter("file:" + dbPath + "?cache=shared")
	require.NoError(t, err)
	defer dbAdapter.Close()

	err = thing.Configure(dbAdapter, nil, 0)
	require.NoError(t, err)

	ctx := context.Background()
	tableName := (&AutoMigrateTestModel{}).TableName()

	checkColumns := func(expectedColumns map[string]bool) {
		var tableInfoResults []struct {
			Name string `db:"name"`
		}
		query := fmt.Sprintf("PRAGMA table_info(%s);", tableName)
		errSelect := dbAdapter.Select(ctx, &tableInfoResults, query)
		require.NoError(t, errSelect, "Failed to get table info for %s", tableName)

		actualColumns := make(map[string]bool)
		for _, col := range tableInfoResults {
			actualColumns[col.Name] = true
		}
		assert.Equal(t, expectedColumns, actualColumns, "Column mismatch for table %s", tableName)
	}

	// 初始迁移
	err = thing.AutoMigrate(&AutoMigrateTestModel{})
	require.NoError(t, err, "Initial AutoMigrate failed")
	expectedInitialCols := map[string]bool{
		"id":          true,
		"created_at":  true,
		"updated_at":  true,
		"deleted":     true,
		"name":        true,
		"email":       true,
		"age":         true,
		"description": true,
	}
	checkColumns(expectedInitialCols)

	// AllowDropColumn=false，不应删除 email
	thing.AllowDropColumn = false
	err = thing.AutoMigrate(&AutoMigrateTestModelNoEmail{})
	require.NoError(t, err, "AutoMigrate with missing column (AllowDropColumn=false) failed")
	// email 列此时不应被删除
	expectedCols1 := map[string]bool{
		"id":          true,
		"created_at":  true,
		"updated_at":  true,
		"deleted":     true,
		"name":        true,
		"email":       true,
		"age":         true,
		"description": true,
	}
	checkColumns(expectedCols1)

	// AllowDropColumn=true，email 应被删除
	thing.AllowDropColumn = true
	err = thing.AutoMigrate(&AutoMigrateTestModelNoEmail{})
	require.NoError(t, err, "AutoMigrate with missing column (AllowDropColumn=true) failed")
	expectedCols2 := map[string]bool{
		"id":          true,
		"created_at":  true,
		"updated_at":  true,
		"deleted":     true,
		"name":        true,
		"age":         true,
		"description": true,
	}
	checkColumns(expectedCols2)

	// AllowDropColumn=true，age/description 也应被删除
	err = thing.AutoMigrate(&AutoMigrateTestModelOnlyName{})
	require.NoError(t, err, "AutoMigrate with multiple missing columns (AllowDropColumn=true) failed")
	expectedCols3 := map[string]bool{
		"id":         true,
		"created_at": true,
		"updated_at": true,
		"deleted":    true,
		"name":       true,
	}
	checkColumns(expectedCols3)

	thing.AllowDropColumn = false
}
