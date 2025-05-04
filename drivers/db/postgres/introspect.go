package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	driversSchema "github.com/burugo/thing/drivers/schema"
)

// PostgreSQLIntrospector implements schema.Introspector for PostgreSQL.
type PostgreSQLIntrospector struct {
	DB *sql.DB
}

// GetTableInfo introspects the given table and returns its schema info (PostgreSQL).
func (pi *PostgreSQLIntrospector) GetTableInfo(ctx context.Context, tableName string) (*driversSchema.TableInfo, error) {
	if pi.DB == nil {
		return nil, fmt.Errorf("PostgreSQLIntrospector: DB is nil")
	}

	// 1. 获取字段信息
	colQuery := `SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_name = $1`
	colRows, err := pi.DB.QueryContext(ctx, colQuery, tableName)
	if err != nil {
		return nil, fmt.Errorf("information_schema.columns failed: %w", err)
	}
	defer colRows.Close()

	var columns []driversSchema.ColumnInfo
	rowCount := 0
	for colRows.Next() {
		rowCount++
		var name, dataType, isNullable string
		var defaultVal sql.NullString
		if err := colRows.Scan(&name, &dataType, &isNullable, &defaultVal); err != nil {
			return nil, fmt.Errorf("scan columns: %w", err)
		}
		nullable := isNullable == "YES"
		var defPtr *string
		if defaultVal.Valid {
			defPtr = &defaultVal.String
		}
		columns = append(columns, driversSchema.ColumnInfo{
			Name:       name,
			DataType:   dataType,
			IsNullable: nullable,
			Default:    defPtr,
		})
	}
	if err := colRows.Err(); err != nil {
		return nil, fmt.Errorf("columns rows: %w", err)
	}
	if rowCount == 0 {
		// Table does not exist
		return nil, nil
	}

	// 2. 主键信息
	pkQuery := `SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = $1::regclass AND i.indisprimary`
	pkRows, err := pi.DB.QueryContext(ctx, pkQuery, tableName)
	if err != nil {
		return nil, fmt.Errorf("pg_index primary key: %w", err)
	}
	defer pkRows.Close()
	var pkCol string
	for pkRows.Next() {
		if err := pkRows.Scan(&pkCol); err != nil {
			return nil, fmt.Errorf("scan pk: %w", err)
		}
		break // 只取第一个主键字段
	}
	if err := pkRows.Err(); err != nil {
		return nil, fmt.Errorf("pk rows: %w", err)
	}

	// 标记主键字段
	for i := range columns {
		if columns[i].Name == pkCol {
			columns[i].IsPrimary = true
		}
	}

	// 3. 唯一约束（列级）
	uniqQuery := `SELECT a.attname
		FROM pg_constraint c
		JOIN pg_class t ON c.conrelid = t.oid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(c.conkey)
		WHERE c.contype = 'u' AND t.relname = $1`
	uniqRows, err := pi.DB.QueryContext(ctx, uniqQuery, tableName)
	if err != nil {
		return nil, fmt.Errorf("pg_constraint unique: %w", err)
	}
	defer uniqRows.Close()
	uniqCols := make(map[string]bool)
	for uniqRows.Next() {
		var col string
		if err := uniqRows.Scan(&col); err != nil {
			return nil, fmt.Errorf("scan unique: %w", err)
		}
		uniqCols[col] = true
	}
	if err := uniqRows.Err(); err != nil {
		return nil, fmt.Errorf("unique rows: %w", err)
	}
	for i := range columns {
		if uniqCols[columns[i].Name] {
			columns[i].IsUnique = true
		}
	}

	// 4. 索引信息
	idxQuery := `SELECT indexname, indexdef FROM pg_indexes WHERE tablename = $1`
	idxRows, err := pi.DB.QueryContext(ctx, idxQuery, tableName)
	if err != nil {
		return nil, fmt.Errorf("pg_indexes: %w", err)
	}
	defer idxRows.Close()
	var indexes []driversSchema.IndexInfo
	for idxRows.Next() {
		var name, def string
		if err := idxRows.Scan(&name, &def); err != nil {
			return nil, fmt.Errorf("scan index: %w", err)
		}
		// 解析列名
		cols := parsePgIndexColumns(def)
		unique := strings.Contains(def, "UNIQUE INDEX") || strings.Contains(def, "UNIQUE CONSTRAINT")
		if name == "" || len(cols) == 0 {
			continue
		}
		// 跳过主键索引
		if name == "" || name == tableName+"_pkey" {
			continue
		}
		indexes = append(indexes, driversSchema.IndexInfo{
			Name:    name,
			Columns: cols,
			Unique:  unique,
		})
	}
	if err := idxRows.Err(); err != nil {
		return nil, fmt.Errorf("index rows: %w", err)
	}

	return &driversSchema.TableInfo{
		Name:       tableName,
		Columns:    columns,
		Indexes:    indexes,
		PrimaryKey: pkCol,
	}, nil
}

// parsePgIndexColumns 解析 pg_indexes.indexdef 字符串中的列名
func parsePgIndexColumns(def string) []string {
	// 例: CREATE UNIQUE INDEX idx_users_email ON public.users USING btree (email)
	start := strings.Index(def, "(")
	end := strings.Index(def, ")")
	if start == -1 || end == -1 || end <= start+1 {
		return nil
	}
	cols := strings.Split(def[start+1:end], ",")
	for i := range cols {
		cols[i] = strings.TrimSpace(cols[i])
	}
	return cols
}
