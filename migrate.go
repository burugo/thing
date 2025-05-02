package thing

import (
	"context"
	"fmt"
	"thing/internal/schema"
)

// AutoMigrate 生成并执行建表 SQL，支持批量建表
func AutoMigrate(models ...interface{}) error {
	if globalDB == nil {
		return fmt.Errorf("AutoMigrate: globalDB is nil, please call thing.Configure(db, cache)")
	}
	sqls, err := schema.AutoMigrate(models...)
	if err != nil {
		return err
	}
	for _, sql := range sqls {
		fmt.Println("[AutoMigrate] Executing SQL:\n", sql)
		_, err := globalDB.Exec(context.Background(), sql)
		if err != nil {
			return fmt.Errorf("AutoMigrate: failed to execute SQL: %w\nSQL: %s", err, sql)
		}
	}
	return nil
}
