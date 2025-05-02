package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"thing/internal/schema"
)

// MySQLIntrospector implements schema.Introspector for MySQL.
type MySQLIntrospector struct {
	DB *sql.DB
}

// GetTableInfo introspects the given table and returns its schema info (MySQL).
func (mi *MySQLIntrospector) GetTableInfo(ctx context.Context, tableName string) (*schema.TableInfo, error) {
	if mi.DB == nil {
		return nil, fmt.Errorf("MySQLIntrospector: DB is nil")
	}

	// 1. 获取字段信息
	colRows, err := mi.DB.QueryContext(ctx, "SHOW COLUMNS FROM "+tableName)
	if err != nil {
		return nil, fmt.Errorf("SHOW COLUMNS failed: %w", err)
	}
	defer colRows.Close()

	var columns []schema.ColumnInfo
	var pkCol string
	for colRows.Next() {
		var field, colType, nullStr, key, defaultVal, extra string
		if err := colRows.Scan(&field, &colType, &nullStr, &key, &defaultVal, &extra); err != nil {
			return nil, fmt.Errorf("scan SHOW COLUMNS: %w", err)
		}
		isNullable := nullStr == "YES"
		isPrimary := key == "PRI"
		isUnique := key == "UNI"
		var defPtr *string
		if defaultVal != "" {
			defPtr = &defaultVal
		}
		if isPrimary {
			pkCol = field
		}
		columns = append(columns, schema.ColumnInfo{
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
	idxRows, err := mi.DB.QueryContext(ctx, "SHOW INDEX FROM "+tableName)
	if err != nil {
		return nil, fmt.Errorf("SHOW INDEX failed: %w", err)
	}
	defer idxRows.Close()

	// map[indexName] -> (unique, []columns)
	idxMap := make(map[string]struct {
		unique  bool
		columns []string
	})
	for idxRows.Next() {
		var (
			table, nonUnique, keyName, seqInIndex, colName, collation, cardinality, subPart, packed, nullStr, indexType, comment, indexComment sql.NullString
		)
		// SHOW INDEX 返回字段较多，这里只关心 keyName, nonUnique, colName
		if err := idxRows.Scan(&table, &nonUnique, &keyName, &seqInIndex, &colName, &collation, &cardinality, &subPart, &packed, &nullStr, &indexType, &comment, &indexComment); err != nil {
			return nil, fmt.Errorf("scan SHOW INDEX: %w", err)
		}
		if !keyName.Valid || !colName.Valid {
			continue
		}
		uniq := false
		if nonUnique.Valid && nonUnique.String == "0" {
			uniq = true
		}
		ent := idxMap[keyName.String]
		ent.unique = uniq
		ent.columns = append(ent.columns, colName.String)
		idxMap[keyName.String] = ent
	}
	if err := idxRows.Err(); err != nil {
		return nil, fmt.Errorf("SHOW INDEX rows: %w", err)
	}

	var indexes []schema.IndexInfo
	for name, ent := range idxMap {
		// 跳过主键索引（通常名为 PRIMARY）
		if name == "PRIMARY" {
			continue
		}
		indexes = append(indexes, schema.IndexInfo{
			Name:    name,
			Columns: ent.columns,
			Unique:  ent.unique,
		})
	}

	return &schema.TableInfo{
		Name:       tableName,
		Columns:    columns,
		Indexes:    indexes,
		PrimaryKey: pkCol,
	}, nil
}
