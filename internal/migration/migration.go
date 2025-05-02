package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// MigrationFile 代表一个迁移脚本文件
type MigrationFile struct {
	Version   int64  // 版本号
	Name      string // 描述性名称
	Direction string // 方向 ("up" or "down")
	FilePath  string // 完整文件路径
}

// migrationFilenameRegex 用于解析迁移文件名
var migrationFilenameRegex = regexp.MustCompile(`^(\d+)_([a-zA-Z0-9_]+)\.(up|down)\.sql$`)

// DiscoverMigrations 在指定目录中发现并排序迁移脚本文件
func DiscoverMigrations(dir string) ([]MigrationFile, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []MigrationFile{}, nil // 目录不存在，返回空列表
		}
		return nil, fmt.Errorf("failed to read migrations directory %s: %w", dir, err)
	}

	var migrations []MigrationFile
	for _, file := range files {
		if file.IsDir() {
			continue // 跳过子目录
		}

		match := migrationFilenameRegex.FindStringSubmatch(file.Name())
		if len(match) != 4 {
			// 文件名不匹配，跳过
			continue
		}

		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			// 版本号解析失败，可能数字过大或格式错误，跳过
			continue
		}

		migrations = append(migrations, MigrationFile{
			Version:   version,
			Name:      match[2],
			Direction: match[3],
			FilePath:  filepath.Join(dir, file.Name()),
		})
	}

	// 按版本号排序
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}
