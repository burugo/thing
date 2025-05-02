package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"thing/internal/schema"
)

// SQLiteIntrospector implements schema.Introspector for SQLite.
type SQLiteIntrospector struct {
	DB *sql.DB
}

// GetTableInfo introspects the given table and returns its schema info (SQLite).
func (si *SQLiteIntrospector) GetTableInfo(ctx context.Context, tableName string) (*schema.TableInfo, error) {
	if si.DB == nil {
		return nil, fmt.Errorf("SQLiteIntrospector: DB is nil")
	}

	// 1. 获取字段信息
	colRows, err := si.DB.QueryContext(ctx, "PRAGMA table_info("+tableName+")")
	if err != nil {
		return nil, fmt.Errorf("PRAGMA table_info failed: %w", err)
	}
	defer colRows.Close()

	var columns []schema.ColumnInfo
	var pkCol string
	for colRows.Next() {
		var cid int
		var name, colType string
		var notnull, pk int
		var dfltValue sql.NullString
		if err := colRows.Scan(&cid, &name, &colType, &notnull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("scan table_info: %w", err)
		}
		isPrimary := pk > 0
		isNullable := notnull == 0
		var defPtr *string
		if dfltValue.Valid {
			defPtr = &dfltValue.String
		}
		if isPrimary {
			pkCol = name
		}
		columns = append(columns, schema.ColumnInfo{
			Name:       name,
			DataType:   colType,
			IsNullable: isNullable,
			IsPrimary:  isPrimary,
			Default:    defPtr,
		})
	}
	if err := colRows.Err(); err != nil {
		return nil, fmt.Errorf("table_info rows: %w", err)
	}

	// 2. 索引信息
	idxRows, err := si.DB.QueryContext(ctx, "PRAGMA index_list("+tableName+")")
	if err != nil {
		return nil, fmt.Errorf("PRAGMA index_list failed: %w", err)
	}
	defer idxRows.Close()

	var indexes []schema.IndexInfo
	for idxRows.Next() {
		var seq int
		var name string
		var unique int
		var origin, partial sql.NullString
		if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, fmt.Errorf("scan index_list: %w", err)
		}
		if name == "sqlite_autoindex_"+tableName+"_1" {
			continue // 跳过主键索引
		}
		// 获取索引列
		colInfoRows, err := si.DB.QueryContext(ctx, "PRAGMA index_info("+name+")")
		if err != nil {
			return nil, fmt.Errorf("PRAGMA index_info(%s) failed: %w", name, err)
		}
		var cols []string
		for colInfoRows.Next() {
			var seqno, cid int
			var colName string
			if err := colInfoRows.Scan(&seqno, &cid, &colName); err != nil {
				colInfoRows.Close()
				return nil, fmt.Errorf("scan index_info: %w", err)
			}
			cols = append(cols, colName)
		}
		colInfoRows.Close()
		indexes = append(indexes, schema.IndexInfo{
			Name:    name,
			Columns: cols,
			Unique:  unique == 1,
		})
	}
	if err := idxRows.Err(); err != nil {
		return nil, fmt.Errorf("index_list rows: %w", err)
	}

	// 3. 标记唯一列
	for i := range columns {
		for _, idx := range indexes {
			if idx.Unique && len(idx.Columns) == 1 && idx.Columns[0] == columns[i].Name {
				columns[i].IsUnique = true
			}
		}
	}

	return &schema.TableInfo{
		Name:       tableName,
		Columns:    columns,
		Indexes:    indexes,
		PrimaryKey: pkCol,
	}, nil
}
