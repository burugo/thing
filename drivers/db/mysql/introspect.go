package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	driversSchema "github.com/burugo/thing/drivers/schema"
)

// MySQLIntrospector implements schema.Introspector for MySQL.
type MySQLIntrospector struct {
	DB *sql.DB
}

// GetTableInfo introspects the given table and returns its schema info (MySQL).
func (mi *MySQLIntrospector) GetTableInfo(ctx context.Context, tableName string) (*driversSchema.TableInfo, error) {
	if mi.DB == nil {
		return nil, fmt.Errorf("MySQLIntrospector: DB is nil")
	}

	// 1. 获取字段信息
	colRows, err := mi.DB.QueryContext(ctx, "SHOW COLUMNS FROM "+tableName) // #nosec G202
	if err != nil {
		// MySQL: Error 1146 (42S02): Table 'xxx' doesn't exist
		if err.Error() == "Error 1146: Table '"+tableName+"' doesn't exist" ||
			strings.Contains(err.Error(), "doesn't exist") {
			return nil, nil
		}
		return nil, fmt.Errorf("SHOW COLUMNS failed: %w", err)
	}
	defer colRows.Close()

	var columns []driversSchema.ColumnInfo
	var pkCol string
	for colRows.Next() {
		var field, colType, nullStr, key, extra string
		var def sql.NullString
		if err := colRows.Scan(&field, &colType, &nullStr, &key, &def, &extra); err != nil {
			return nil, fmt.Errorf("scan SHOW COLUMNS: %w", err)
		}
		isNullable := nullStr == "YES"
		isPrimary := key == "PRI"
		isUnique := key == "UNI"
		var defPtr *string
		if def.Valid {
			defPtr = &def.String
		}
		if isPrimary {
			pkCol = field
		}
		columns = append(columns, driversSchema.ColumnInfo{
			Name:       field,
			DataType:   colType,
			IsNullable: isNullable,
			IsPrimary:  isPrimary,
			IsUnique:   isUnique,
			Default:    defPtr,
		})
	}
	if err := colRows.Err(); err != nil {
		return nil, fmt.Errorf("SHOW COLUMNS rows: %w", err)
	}

	// 2. 获取索引信息
	idxRows, err := mi.DB.QueryContext(ctx, "SELECT Key_name, Non_unique, Column_name FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ?", tableName)
	if err != nil {
		return nil, fmt.Errorf("SHOW INDEX failed: %w", err)
	}
	defer idxRows.Close()

	idxMap := make(map[string]struct {
		unique  bool
		columns []string
	})
	for idxRows.Next() {
		var keyName string
		var nonUnique int
		var colName string
		if err := idxRows.Scan(&keyName, &nonUnique, &colName); err != nil {
			return nil, fmt.Errorf("scan SHOW INDEX: %w", err)
		}
		if keyName == "" || colName == "" {
			continue
		}
		uniq := nonUnique == 0
		ent := idxMap[keyName]
		ent.unique = uniq
		ent.columns = append(ent.columns, colName)
		idxMap[keyName] = ent
	}
	if err := idxRows.Err(); err != nil {
		return nil, fmt.Errorf("SHOW INDEX rows: %w", err)
	}

	var indexes []driversSchema.IndexInfo
	for name, ent := range idxMap {
		// 跳过主键索引（通常名为 PRIMARY）
		if name == "PRIMARY" {
			continue
		}
		indexes = append(indexes, driversSchema.IndexInfo{
			Name:    name,
			Columns: ent.columns,
			Unique:  ent.unique,
		})
	}

	return &driversSchema.TableInfo{
		Name:       tableName,
		Columns:    columns,
		Indexes:    indexes,
		PrimaryKey: pkCol,
	}, nil
}
