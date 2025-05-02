package schema

import (
	"fmt"
	"strings"
)

// TypeMapping 定义 Go 类型到 SQL 类型的映射表
var TypeMapping = map[string]map[string]string{
	"mysql": {
		"int":       "INT",
		"int8":      "TINYINT",
		"int16":     "SMALLINT",
		"int32":     "INT",
		"int64":     "BIGINT",
		"uint":      "INT UNSIGNED",
		"uint8":     "TINYINT UNSIGNED",
		"uint16":    "SMALLINT UNSIGNED",
		"uint32":    "INT UNSIGNED",
		"uint64":    "BIGINT UNSIGNED",
		"float32":   "FLOAT",
		"float64":   "DOUBLE",
		"string":    "VARCHAR(255)",
		"bool":      "BOOLEAN",
		"time.Time": "DATETIME",
		"[]byte":    "BLOB",
	},
	"postgres": {
		"int":       "INTEGER",
		"int8":      "SMALLINT",
		"int16":     "SMALLINT",
		"int32":     "INTEGER",
		"int64":     "BIGINT",
		"uint":      "BIGINT",
		"uint8":     "SMALLINT",
		"uint16":    "INTEGER",
		"uint32":    "BIGINT",
		"uint64":    "NUMERIC",
		"float32":   "REAL",
		"float64":   "DOUBLE PRECISION",
		"string":    "VARCHAR(255)",
		"bool":      "BOOLEAN",
		"time.Time": "TIMESTAMP",
		"[]byte":    "BYTEA",
	},
	"sqlite": {
		"int":       "INTEGER",
		"int8":      "INTEGER",
		"int16":     "INTEGER",
		"int32":     "INTEGER",
		"int64":     "INTEGER",
		"uint":      "INTEGER",
		"uint8":     "INTEGER",
		"uint16":    "INTEGER",
		"uint32":    "INTEGER",
		"uint64":    "INTEGER",
		"float32":   "REAL",
		"float64":   "REAL",
		"string":    "TEXT",
		"bool":      "BOOLEAN",
		"time.Time": "DATETIME",
		"[]byte":    "BLOB",
	},
}

// GenerateCreateTableSQL 生成单表 CREATE TABLE 语句
// info: 目标模型的 *schema.ModelInfo
// dialect: "mysql" | "postgres" | "sqlite"
func GenerateCreateTableSQL(info *ModelInfo, dialect string) (string, error) {
	typeMap, ok := TypeMapping[dialect]
	if !ok {
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}

	var cols []string
	pkCol := info.PkName
	for _, f := range info.CompareFields {
		col := f.DBColumn
		goType := f.Type.String() // e.g. "int64", "string", "time.Time"
		sqlType, ok := typeMap[goType]
		if !ok {
			// fallback: try Kind
			sqlType, ok = typeMap[f.Kind.String()]
			if !ok {
				sqlType = "TEXT" // fallback
			}
		}
		var constraints []string
		if col == pkCol {
			constraints = append(constraints, "PRIMARY KEY")
			// 自增
			if dialect == "mysql" {
				if strings.HasPrefix(sqlType, "INT") || strings.HasPrefix(sqlType, "BIGINT") {
					constraints = append(constraints, "AUTO_INCREMENT")
				}
			}
			if dialect == "sqlite" {
				if sqlType == "INTEGER" {
					constraints = append(constraints, "AUTOINCREMENT")
				}
			}
			if dialect == "postgres" {
				if sqlType == "BIGINT" {
					sqlType = "BIGSERIAL"
				}
			}
		}
		// 非空
		if !f.IsEmbedded && !strings.HasSuffix(col, "_at") && col != pkCol {
			constraints = append(constraints, "NOT NULL")
		}
		// 唯一（可扩展：解析 tag）
		// 默认值、索引、外键等后续扩展
		colDef := fmt.Sprintf("%s %s %s", col, sqlType, strings.Join(constraints, " "))
		cols = append(cols, strings.TrimSpace(colDef))
	}

	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n);", info.TableName, strings.Join(cols, ",\n  "))
	return sql, nil
}
