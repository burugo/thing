package thing

import (
	"context"
	"fmt"

	"github.com/burugo/thing/internal/schema"
)

// GenerateMigrationSQL 生成建表 SQL，但不执行，支持批量模型
func GenerateMigrationSQL(models ...interface{}) ([]string, error) {
	if globalDB == nil {
		return nil, fmt.Errorf("GenerateMigrationSQL: globalDB is nil, please call thing.Configure(db, cache)")
	}
	dialect := globalDB.DialectName()
	return schema.AutoMigrateWithDialect(dialect, models...)
}

// AutoMigrate 生成并执行建表 SQL，支持批量建表
func AutoMigrate(models ...interface{}) error {
	sqls, err := GenerateMigrationSQL(models...)
	if err != nil {
		// Propagate error from GenerateMigrationSQL (which already checks globalDB)
		return err
	}

	// Execute the generated SQLs
	for _, sql := range sqls {
		fmt.Println("[AutoMigrate] Executing SQL:\n", sql)
		_, err := globalDB.Exec(context.Background(), sql)
		if err != nil {
			return fmt.Errorf("AutoMigrate: failed to execute SQL: %w\nSQL: %s", err, sql)
		}
	}
	return nil
}
