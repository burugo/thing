package thing_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/burugo/thing/internal/migration"

	"github.com/burugo/thing/internal/drivers/db/sqlite"
	"github.com/burugo/thing/internal/interfaces"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestMigrator 创建一个用于测试的 Migrator 和临时目录
func setupTestMigrator(t *testing.T) (*migration.Migrator, interfaces.DBAdapter, string, func()) {
	tempDir, err := os.MkdirTemp("", "migrator_test_")
	require.NoError(t, err)

	migrationsDir := filepath.Join(tempDir, "migrations")
	err = os.Mkdir(migrationsDir, 0755)
	require.NoError(t, err)

	dbPath := filepath.Join(tempDir, "test_migrate.db")
	db, err := sqlite.NewSQLiteAdapter("file:" + dbPath + "?mode=memory&cache=shared") // Use in-memory for speed
	require.NoError(t, err)

	migr := migration.NewMigrator(db, migrationsDir)
	migr.EnableLog(false) // Disable logging for cleaner test output

	cleanup := func() {
		db.Close()
		os.RemoveAll(tempDir)
	}

	return migr, db, migrationsDir, cleanup
}

// createMigrationFile 创建一个迁移文件
func createMigrationFile(t *testing.T, dir string, version int, name, direction, content string) {
	filename := fmt.Sprintf("%05d_%s.%s.sql", version, name, direction)
	path := filepath.Join(dir, filename)
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}

func TestMigrator_Migrate_Initial(t *testing.T) {
	migr, db, migrationsDir, cleanup := setupTestMigrator(t)
	defer cleanup()

	// 创建迁移文件
	createMigrationFile(t, migrationsDir, 1, "create_users", "up", "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);")
	createMigrationFile(t, migrationsDir, 1, "create_users", "down", "DROP TABLE users;")
	createMigrationFile(t, migrationsDir, 2, "add_email", "up", "ALTER TABLE users ADD COLUMN email TEXT;")

	// 执行迁移
	err := migr.Migrate(context.Background())
	require.NoError(t, err)

	// 验证数据库状态
	var countRows []int
	err = db.Select(context.Background(), &countRows, "SELECT COUNT(*) FROM users")
	assert.NoError(t, err, "users table should exist")
	if assert.Len(t, countRows, 1, "Should get one count row") {
		assert.GreaterOrEqual(t, countRows[0], 0, "users table count should be >= 0")
	}

	var tableInfo []struct {
		Name string `db:"name"`
	}
	err = db.Select(context.Background(), &tableInfo, "PRAGMA table_info(users)")
	require.NoError(t, err)
	assert.Len(t, tableInfo, 3, "users table should have 3 columns (id, name, email)") // SQLite returns one row per column
	foundEmail := false
	for _, col := range tableInfo {
		if col.Name == "email" {
			foundEmail = true
			break
		}
	}
	assert.True(t, foundEmail, "email column should exist")

	// 验证 schema_migrations 表 (只验证 version 和 description)
	type appliedResult struct { // Define a local struct for test result scanning
		Version     string
		Description string
	}
	var applied []appliedResult
	// Select only version and description for verification
	err = db.Select(context.Background(), &applied, "SELECT version, description FROM schema_migrations ORDER BY version ASC")
	require.NoError(t, err)
	assert.Len(t, applied, 2)
	assert.Equal(t, "1", applied[0].Version)
	assert.Equal(t, "create_users", applied[0].Description)
	assert.Equal(t, "2", applied[1].Version)
	assert.Equal(t, "add_email", applied[1].Description)
}

func TestMigrator_Migrate_AlreadyApplied(t *testing.T) {
	migr, db, migrationsDir, cleanup := setupTestMigrator(t)
	defer cleanup()

	// 创建迁移文件
	createMigrationFile(t, migrationsDir, 1, "create_users", "up", "CREATE TABLE users (id INTEGER PRIMARY KEY);")

	// 首次迁移
	err := migr.Migrate(context.Background())
	require.NoError(t, err)

	// 再次迁移
	err = migr.Migrate(context.Background())
	require.NoError(t, err, "Migrating again should do nothing and succeed")

	// 验证 schema_migrations 表只有一条记录
	var appliedCountRows []int
	err = db.Select(context.Background(), &appliedCountRows, "SELECT COUNT(*) FROM schema_migrations")
	require.NoError(t, err)
	if assert.Len(t, appliedCountRows, 1, "Should get one count row") {
		assert.Equal(t, 1, appliedCountRows[0])
	}
}

func TestMigrator_Migrate_WithError(t *testing.T) {
	migr, _, migrationsDir, cleanup := setupTestMigrator(t)
	defer cleanup()

	// 创建迁移文件 (一个有效，一个无效 SQL)
	createMigrationFile(t, migrationsDir, 1, "create_valid", "up", "CREATE TABLE valid (id INTEGER PRIMARY KEY);")
	createMigrationFile(t, migrationsDir, 2, "create_invalid", "up", "CREATE TABEL invalid (id INTEGER PRIMARY KEY);") // Typo in TABLE

	// 执行迁移，应该失败
	err := migr.Migrate(context.Background())
	require.Error(t, err, "Migration should fail due to invalid SQL")
	assert.Contains(t, err.Error(), "failed to execute migration 2", "Error message should indicate failed migration")

	// Verify schema_migrations using the exported Db field from the migrator
	type appliedResult struct { // Use the same local struct
		Version     string
		Description string
	}
	var applied []appliedResult
	dbCheck := migr.Db // Access the exported field

	// Select only version and description
	errCheck := dbCheck.Select(context.Background(), &applied, "SELECT version, description FROM schema_migrations ORDER BY version ASC")
	if errCheck != nil && !strings.Contains(strings.ToLower(errCheck.Error()), "no such table") {
		// Log if it's SQLite and not "no such table" error, but don't use the variable
		if _, ok := dbCheck.(*sqlite.SQLiteAdapter); ok {
			fmt.Printf("DEBUG: Query error on existing schema_migrations?: %v\n", errCheck)
		}
		require.NoError(t, errCheck, "Querying schema_migrations failed unexpectedly")
	}

	// Assertions based on whether the table exists / has rows
	if errCheck == nil {
		// Table exists and query succeeded
		assert.Len(t, applied, 0, "No migration should be applied after failed migration in SQLite (full rollback)")
	} else {
		// Table likely doesn't exist (rolled back), which is also a valid outcome
		assert.Contains(t, strings.ToLower(errCheck.Error()), "no such table", "Error should be 'no such table' if transaction rolled back table creation")
		assert.Empty(t, applied, "Applied list should be empty if schema_migrations table does not exist")
	}
}
