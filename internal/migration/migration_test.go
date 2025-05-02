package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverMigrations(t *testing.T) {
	// 创建临时测试目录和文件
	tempDir, err := os.MkdirTemp("", "migration_test_")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// 创建一些有效和无效的迁移文件
	filesToCreate := []string{
		"00002_add_email.up.sql",
		"00001_create_users.up.sql",
		"00001_create_users.down.sql",
		"00002_add_email.down.sql",
		"00003_no_direction.sql",               // 无效
		"not_a_migration.txt",                  // 无效
		"abc_wrong_version.up.sql",             // 无效
		"123456789012345678901_too_big.up.sql", // 版本号过大，无法解析
	}
	for _, fname := range filesToCreate {
		err := os.WriteFile(filepath.Join(tempDir, fname), []byte("-- test"), 0644)
		require.NoError(t, err)
	}

	// 创建一个子目录
	err = os.Mkdir(filepath.Join(tempDir, "subdir"), 0755)
	require.NoError(t, err)

	// 测试 DiscoverMigrations
	migrations, err := DiscoverMigrations(tempDir)
	require.NoError(t, err)

	// 验证结果
	assert.Len(t, migrations, 4, "should find 4 valid migration files")

	// 验证排序和内容
	assert.Equal(t, int64(1), migrations[0].Version)
	assert.Equal(t, "create_users", migrations[0].Name)
	assert.Equal(t, "down", migrations[0].Direction) // down 应排在 up 前面，但实际版本号一样，go sort 不稳定，顺序不保证

	assert.Equal(t, int64(1), migrations[1].Version)
	assert.Equal(t, "create_users", migrations[1].Name)
	assert.Equal(t, "up", migrations[1].Direction)

	assert.Equal(t, int64(2), migrations[2].Version)
	assert.Equal(t, "add_email", migrations[2].Name)
	assert.Equal(t, "down", migrations[2].Direction) // 同上

	assert.Equal(t, int64(2), migrations[3].Version)
	assert.Equal(t, "add_email", migrations[3].Name)
	assert.Equal(t, "up", migrations[3].Direction)

	// 测试目录不存在的情况
	migs, err := DiscoverMigrations(filepath.Join(tempDir, "non_existent_dir"))
	require.NoError(t, err)
	assert.Empty(t, migs)
}
