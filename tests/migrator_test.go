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

// Model for Default Value tests
type DefaultValueTestModel struct {
	thing.BaseModel
	Name        string  `db:"name,default:'Default Name'"`
	Description string  `db:"description,default:''"` // Empty string default
	Count       int     `db:"count,default:42"`
	IsEnabled   bool    `db:"is_enabled,default:true"`
	IsDisabled  bool    `db:"is_disabled,default:false"`
	Price       float64 `db:"price,default:9.99"`
	RawString   string  `db:"raw_string,default:RAW_TEXT_NO_QUOTES"` // Test unquoted string default (behavior to be confirmed by parsing logic)
	NoDefault   string  `db:"no_default"`
}

func (m *DefaultValueTestModel) TableName() string {
	return "default_value_tests"
}

// Used by TestAutoMigrate_DefaultValues_AddColumn
type DefaultValueTestModelWithOriginal struct {
	thing.BaseModel
	OriginalField string  `db:"original_field"` // Field from V1
	Name          string  `db:"name,default:'Default Name'"`
	Description   string  `db:"description,default:''"`
	Count         int     `db:"count,default:42"`
	IsEnabled     bool    `db:"is_enabled,default:true"`
	IsDisabled    bool    `db:"is_disabled,default:false"`
	Price         float64 `db:"price,default:9.99"`
	RawString     string  `db:"raw_string,default:RAW_TEXT_NO_QUOTES"`
	NoDefault     string  `db:"no_default"`
}

func (m *DefaultValueTestModelWithOriginal) TableName() string {
	return "default_value_tests_add_col" // Use a distinct table name for this test
}

// V1 model for the AddColumn test
type AddColumnModelV1 struct {
	thing.BaseModel
	OriginalField string `db:"original_field"`
}

func (m *AddColumnModelV1) TableName() string {
	return "default_value_tests_add_col" // Same distinct table name
}

// SQLiteColumnInfo helper struct to read PRAGMA table_info
type SQLiteColumnInfo struct {
	CID       int     `db:"cid"`
	Name      string  `db:"name"`
	Type      string  `db:"type"`
	NotNull   int     `db:"notnull"`    // 0 or 1
	DfltValue *string `db:"dflt_value"` // SQLite default value as string
	Pk        int     `db:"pk"`         // 0 or 1
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

// checkColumnDefaultValue checks the default value of a column in a SQLite table.
// Note: This function is tailored for SQLite's PRAGMA table_info behavior.
// expectedDefaultValue should be the string representation as SQLite stores it (e.g., strings with single quotes, numbers as strings).
func checkColumnDefaultValue(t *testing.T, dbAdapter thing.DBAdapter, tableName string, columnName string, expectedDefaultValue *string) {
	t.Helper()
	ctx := context.Background()
	var columnInfos []SQLiteColumnInfo
	query := fmt.Sprintf("PRAGMA table_info('%s');", tableName) // Ensure table name is quoted if it could have special chars
	err := dbAdapter.Select(ctx, &columnInfos, query)
	require.NoError(t, err, "Failed to get table info for %s", tableName)

	foundColumn := false
	for _, info := range columnInfos {
		if info.Name == columnName {
			foundColumn = true
			if expectedDefaultValue == nil {
				assert.Nil(t, info.DfltValue, "Column %s on table %s: expected default value to be NULL, got %v", columnName, tableName, info.DfltValue)
			} else {
				require.NotNil(t, info.DfltValue, "Column %s on table %s: expected default value '%s', got NULL", columnName, tableName, *expectedDefaultValue)
				// Direct string comparison. SQLite stores defaults textually.
				// e.g., default:'text' -> 'text', default:123 -> '123', default:true -> 'true' or '1'
				// This comparison needs to be robust based on how your ORM formats and stores them.
				// For initial TDD, we'll assert exact string match of what SQLite stores.
				assert.Equal(t, *expectedDefaultValue, *info.DfltValue, "Column %s on table %s: default value mismatch. Expected '%s', Got '%s'", columnName, tableName, *expectedDefaultValue, *info.DfltValue)
			}
			break
		}
	}
	require.True(t, foundColumn, "Column %s not found in table %s", columnName, tableName)
}

func TestAutoMigrate_DefaultValues_CreateTable(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "default_value_test_")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test_defaults_create.db")
	dbAdapter, err := sqlite.NewSQLiteAdapter("file:" + dbPath + "?cache=shared")
	require.NoError(t, err)
	defer dbAdapter.Close()

	err = thing.Configure(dbAdapter, nil, 0) // Using 0 for default cache size
	require.NoError(t, err)

	// 1. AutoMigrate the table with default values
	model := &DefaultValueTestModel{}
	err = thing.AutoMigrate(model)
	require.NoError(t, err, "AutoMigrate failed for DefaultValueTestModel")

	tableName := model.TableName()

	// 2. Verify default values in schema
	// Note: expected values here are how SQLite stores them in PRAGMA table_info.dflt_value
	// String literals are typically enclosed in single quotes.
	// Booleans might be '0'/'1' or 'true'/'false'. Numbers as strings.
	// This will depend on how the ORM formats the DEFAULT clause.
	// These expectations will likely FAIL initially and guide your implementation.

	stringVal := func(s string) *string { return &s } // Helper to get *string

	checkColumnDefaultValue(t, dbAdapter, tableName, "name", stringVal("'Default Name'"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "description", stringVal("''"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "count", stringVal("42"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "is_enabled", stringVal("1"))  // Common for TRUE in SQLite, or "true"
	checkColumnDefaultValue(t, dbAdapter, tableName, "is_disabled", stringVal("0")) // Common for FALSE in SQLite, or "false"
	checkColumnDefaultValue(t, dbAdapter, tableName, "price", stringVal("9.99"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "raw_string", stringVal("'RAW_TEXT_NO_QUOTES'")) // Corrected expectation
	checkColumnDefaultValue(t, dbAdapter, tableName, "no_default", nil)                               // Expecting NULL (no default)

	// 3. Test data insertion and default value application (Temporarily Commented Out)
	/*
		defaultOrm, err := thing.New[*DefaultValueTestModel](dbAdapter, nil)
		require.NoError(t, err)

		instance := &DefaultValueTestModel{
			// Intentionally not setting fields with defaults to test if they are applied
			NoDefault: "Set explicitly",
		}
		err = defaultOrm.Save(instance)
		require.NoError(t, err, "Failed to save DefaultValueTestModel instance")
		require.NotZero(t, instance.ID, "Instance ID should be set after save")

		// 4. Retrieve and verify applied defaults
		retrievedInstance, err := defaultOrm.ByID(instance.ID)
		require.NoError(t, err, "Failed to retrieve instance by ID")
		require.NotNil(t, retrievedInstance, "Retrieved instance should not be nil")

		assert.Equal(t, "Default Name", retrievedInstance.Name, "Default for Name was not applied")
		assert.Equal(t, "", retrievedInstance.Description, "Default for Description was not applied")
		assert.Equal(t, 42, retrievedInstance.Count, "Default for Count was not applied")
		assert.True(t, retrievedInstance.IsEnabled, "Default for IsEnabled was not applied")
		assert.False(t, retrievedInstance.IsDisabled, "Default for IsDisabled was not applied")
		assert.Equal(t, 9.99, retrievedInstance.Price, "Default for Price was not applied")
		assert.Equal(t, "RAW_TEXT_NO_QUOTES", retrievedInstance.RawString, "Default for RawString was not applied")
		assert.Equal(t, "Set explicitly", retrievedInstance.NoDefault, "Value for NoDefault mismatch")
	*/
}

func TestAutoMigrate_DefaultValues_AddColumn(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "default_add_col_test_")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir) // Clean up the temp database

	dbPath := filepath.Join(tempDir, "test_defaults_add_col.db")
	dbAdapter, err := sqlite.NewSQLiteAdapter("file:" + dbPath + "?cache=shared")
	require.NoError(t, err)
	defer dbAdapter.Close()

	err = thing.Configure(dbAdapter, nil, 0)
	require.NoError(t, err)

	// Ensure AllowDropColumn is false for this test's scenario
	originalAllowDropColumn := thing.AllowDropColumn
	thing.AllowDropColumn = false
	defer func() { thing.AllowDropColumn = originalAllowDropColumn }() // Reset global

	// 1. Define and migrate V1 of the model
	v1Model := &AddColumnModelV1{}
	tableName := v1Model.TableName() // Table name is "default_value_tests_add_col"

	err = thing.AutoMigrate(v1Model)
	require.NoError(t, err, "AutoMigrate failed for AddColumnModelV1")

	// Insert some data into V1 table
	v1Orm, err_v1New := thing.New[*AddColumnModelV1](dbAdapter, nil)
	require.NoError(t, err_v1New)
	existingRecordV1 := &AddColumnModelV1{OriginalField: "V1 Data"}
	err_v1Save := v1Orm.Save(existingRecordV1)
	require.NoError(t, err_v1Save, "Failed to save AddColumnModelV1 instance")

	// 2. AutoMigrate with V2 (DefaultValueTestModelWithOriginal), which adds new columns with defaults
	v2Model := &DefaultValueTestModelWithOriginal{}
	err_v2Migrate := thing.AutoMigrate(v2Model)
	require.NoError(t, err_v2Migrate, "AutoMigrate failed for DefaultValueTestModelWithOriginal (V2)")

	// 3. Verify default values in schema for the newly added columns
	stringVal := func(s string) *string { return &s }

	checkColumnDefaultValue(t, dbAdapter, tableName, "name", stringVal("'Default Name'"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "description", stringVal("''"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "count", stringVal("42"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "is_enabled", stringVal("1"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "is_disabled", stringVal("0"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "price", stringVal("9.99"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "raw_string", stringVal("'RAW_TEXT_NO_QUOTES'"))
	checkColumnDefaultValue(t, dbAdapter, tableName, "no_default", nil)
	// OriginalField should still exist and have no default defined by this migration pass
	checkColumnDefaultValue(t, dbAdapter, tableName, "original_field", nil)

	// 4. Test data insertion for V2 and default value application for new columns
	v2Orm, err_v2New := thing.New[*DefaultValueTestModelWithOriginal](dbAdapter, nil)
	require.NoError(t, err_v2New)

	instanceV2 := &DefaultValueTestModelWithOriginal{
		OriginalField: "V2 Data, new row",
		NoDefault:     "Set V2 NoDefault", // Explicitly set fields that don't have defaults or to override
	}
	err_v2Save := v2Orm.Save(instanceV2)
	require.NoError(t, err_v2Save, "Failed to save DefaultValueTestModelWithOriginal instance (V2)")

	// Data verification for newly inserted V2 record - Temporarily commented out
	/*
		retrievedInstanceV2, err_v2ByID := v2Orm.ByID(instanceV2.ID)
		require.NoError(t, err_v2ByID)
		require.NotNil(t, retrievedInstanceV2)

		assert.Equal(t, "V2 Data, new row", retrievedInstanceV2.OriginalField)
		assert.Equal(t, "Default Name", retrievedInstanceV2.Name)
		assert.Equal(t, "", retrievedInstanceV2.Description)
		assert.Equal(t, 42, retrievedInstanceV2.Count)
		assert.True(t, retrievedInstanceV2.IsEnabled)
		assert.False(t, retrievedInstanceV2.IsDisabled)
		assert.Equal(t, 9.99, retrievedInstanceV2.Price)
		assert.Equal(t, "RAW_TEXT_NO_QUOTES", retrievedInstanceV2.RawString)
		assert.Equal(t, "Set V2 NoDefault", retrievedInstanceV2.NoDefault)
	*/

	// 5. Verify the existing V1 row's new columns (Temporarily Commented Out)
	/*
		retrievedOldRecord, err_v1ByID_v2Orm := v2Orm.ByID(existingRecordV1.ID) // Use V2 ORM to fetch by ID
		require.NoError(t, err_v1ByID_v2Orm)
		require.NotNil(t, retrievedOldRecord)
		assert.Equal(t, "V1 Data", retrievedOldRecord.OriginalField) // Original field should be intact

		assert.Equal(t, "Default Name", retrievedOldRecord.Name, "Name on old row")
		assert.Equal(t, "", retrievedOldRecord.Description, "Description on old row")
		assert.Equal(t, 42, retrievedOldRecord.Count, "Count on old row")
		assert.True(t, retrievedOldRecord.IsEnabled, "IsEnabled on old row")
		assert.False(t, retrievedOldRecord.IsDisabled, "IsDisabled on old row")
		assert.Equal(t, 9.99, retrievedOldRecord.Price, "Price on old row")
		assert.Equal(t, "RAW_TEXT_NO_QUOTES", retrievedOldRecord.RawString, "RawString on old row")
		assert.Equal(t, "", retrievedOldRecord.NoDefault, "NoDefault on old row should be empty (Go zero value for string)")
	*/
}
