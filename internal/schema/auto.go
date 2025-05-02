package schema

import (
	"fmt"
	"reflect"
)

// AutoMigrate 只负责生成 SQL 并返回，由 migrate.go 调用时实际执行 SQL
// 移除 AutoMigrate 方法，只保留 AutoMigrateWithDialect。

// AutoMigrateWithDialect 支持指定方言
func AutoMigrateWithDialect(dialect string, models ...interface{}) ([]string, error) {
	var sqls []string
	for _, m := range models {
		t := reflect.TypeOf(m)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		// 这里只能用反射/tag 解析生成 SQL，不能依赖 thing.ModelInfo
		info, err := GetCachedModelInfo(t)
		if err != nil {
			return nil, fmt.Errorf("AutoMigrate: failed to get model info for %s: %w", t.Name(), err)
		}
		sql, err := GenerateCreateTableSQL(info, dialect)
		if err != nil {
			return nil, fmt.Errorf("AutoMigrateWithDialect: failed to generate SQL for %s: %w", t.Name(), err)
		}
		sqls = append(sqls, sql)
	}
	return sqls, nil
}
