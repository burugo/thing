package thing

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/burugo/thing/drivers/schema"
	internalSchema "github.com/burugo/thing/internal/schema"
)

// AllowDropColumn controls whether AutoMigrate will execute 'DROP COLUMN' statements.
// Defaults to false (columns will not be dropped).
var AllowDropColumn = false

// GenerateMigrationSQL 生成建表 SQL，但不执行，支持批量模型
func GenerateMigrationSQL(models ...interface{}) ([]string, error) {
	if globalDB == nil {
		return nil, fmt.Errorf("GenerateMigrationSQL: globalDB is nil, please call thing.Configure(db, cache)")
	}
	dialect := globalDB.DialectName()
	return internalSchema.AutoMigrateWithDialect(dialect, models...)
}

// IntrospectorFactory is a function that returns a schema.Introspector for a given DBAdapter.
type IntrospectorFactory func(DBAdapter) schema.Introspector

var introspectorFactories = make(map[string]IntrospectorFactory)

// RegisterIntrospectorFactory registers a factory for a given dialect (e.g. "sqlite", "mysql", "postgres").
func RegisterIntrospectorFactory(dialect string, factory IntrospectorFactory) {
	introspectorFactories[dialect] = factory
}

// getIntrospectorFactory returns the registered factory for a dialect, or nil if not found.
func getIntrospectorFactory(dialect string) IntrospectorFactory {
	return introspectorFactories[dialect]
}

// introspectTable returns the current schema.TableInfo for a table using the registered driver introspector.
func introspectTable(db DBAdapter, tableName string, dialect string) (*schema.TableInfo, error) {
	factory := getIntrospectorFactory(dialect)
	if factory == nil {
		return nil, fmt.Errorf("no introspector registered for dialect: %s", dialect)
	}
	introspector := factory(db)
	return introspector.GetTableInfo(context.Background(), tableName)
}

// AutoMigrate 生成并执行建表 SQL，支持批量建表和 schema diff
func AutoMigrate(models ...interface{}) error {
	if globalDB == nil {
		return fmt.Errorf("AutoMigrate: globalDB is nil, please call thing.Configure(db, cache)")
	}
	dialect := globalDB.DialectName()
	ctx := context.Background()
	for _, model := range models {
		// 1. 获取模型元信息
		typeOf := reflect.TypeOf(model)
		if typeOf.Kind() == reflect.Ptr {
			typeOf = typeOf.Elem()
		}
		info, err := internalSchema.GetCachedModelInfo(typeOf)
		if err != nil {
			return fmt.Errorf("AutoMigrate: failed to get model info for %s: %w", typeOf.Name(), err)
		}
		tableName := info.TableName
		// 2. introspect table (returns nil, nil if not exists)
		dbTableInfo, err := introspectTable(globalDB, tableName, dialect)
		if err != nil {
			return fmt.Errorf("AutoMigrate: failed to introspect table %s: %w", tableName, err)
		}
		if dbTableInfo == nil {
			// 表不存在，直接建表
			createSQL, err := internalSchema.GenerateCreateTableSQL(info, dialect)
			if err != nil {
				return fmt.Errorf("AutoMigrate: failed to generate CREATE TABLE SQL for %s: %w", tableName, err)
			}
			// --- LOGGING SQL ---
			// 使用 %+q 格式化字符串，可以更清晰地显示包含换行的 SQL
			log.Printf("[AutoMigrate EXEC - CREATE] SQL passed to Exec: %+q", createSQL)
			_, err = globalDB.Exec(ctx, createSQL)
			if err != nil {
				return fmt.Errorf("AutoMigrate: failed to execute CREATE TABLE SQL: %w\nSQL: %s", err, createSQL)
			}
			continue
		}
		// 表已存在，做 schema diff
		alterSQLs, err := internalSchema.GenerateAlterTableSQL(info, internalSchema.ConvertTableInfo(dbTableInfo), dialect)
		if err != nil {
			return fmt.Errorf("AutoMigrate: failed to generate ALTER TABLE SQL for %s: %w", tableName, err)
		}
		for _, alterSQL := range alterSQLs {
			isDropColumnSQL := strings.Contains(strings.ToUpper(alterSQL), "DROP COLUMN") // 更通用的检查

			if isDropColumnSQL && !AllowDropColumn {
				// 如果是 DROP COLUMN 语句，并且 AllowDropColumn 为 false，则跳过
				log.Printf("[AutoMigrate] Skipping actual column drop (AllowDropColumn is false): %s", alterSQL)
				continue
			}
			// --- LOGGING SQL ---
			// 使用 %+q 格式化字符串
			log.Printf("[AutoMigrate EXEC - ALTER] SQL passed to Exec: %+q", alterSQL)
			_, err = globalDB.Exec(ctx, alterSQL)
			if err != nil {
				return fmt.Errorf("AutoMigrate: failed to execute ALTER TABLE SQL: %w\nSQL: %s", err, alterSQL)
			}
		}
	}
	return nil
}
