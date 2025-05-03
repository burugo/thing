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

	tableName := info.TableName
	createTableSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n);", tableName, strings.Join(cols, ",\n  "))

	// 索引 SQL 生成
	var indexSQLs []string
	for _, idx := range info.Indexes {
		idxName := idx.Name
		if idxName == "" {
			idxName = fmt.Sprintf("idx_%s_%s", tableName, strings.Join(idx.Columns, "_"))
		}
		indexSQL := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s);", idxName, tableName, strings.Join(idx.Columns, ", "))
		indexSQLs = append(indexSQLs, indexSQL)
	}
	for _, idx := range info.UniqueIndexes {
		idxName := idx.Name
		if idxName == "" {
			idxName = fmt.Sprintf("uniq_%s_%s", tableName, strings.Join(idx.Columns, "_"))
		}
		indexSQL := fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (%s);", idxName, tableName, strings.Join(idx.Columns, ", "))
		indexSQLs = append(indexSQLs, indexSQL)
	}

	allSQL := createTableSQL
	if len(indexSQLs) > 0 {
		allSQL += "\n" + strings.Join(indexSQLs, "\n")
	}

	return allSQL, nil
}

// GenerateAlterTableSQL 生成 ALTER TABLE 语句以将实际表结构迁移为目标 struct 定义
// modelInfo: 目标模型的 *ModelInfo（Go struct/tag 解析）
// tableInfo: 实际表结构（数据库 introspection 得到）
// dialect: "mysql" | "postgres" | "sqlite"
func GenerateAlterTableSQL(modelInfo *ModelInfo, tableInfo *TableInfo, dialect string) ([]string, error) {
	typeMap, ok := TypeMapping[dialect]
	if !ok {
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}
	var sqls []string
	tableName := modelInfo.TableName

	// 1. 目标列映射
	targetCols := make(map[string]ComparableFieldInfo)
	for _, f := range modelInfo.CompareFields {
		targetCols[f.DBColumn] = f
	}
	// 2. 实际列映射
	dbCols := make(map[string]ColumnInfo)
	for _, c := range tableInfo.Columns {
		dbCols[c.Name] = c
	}

	// 3. 新增列
	for col, f := range targetCols {
		if _, ok := dbCols[col]; !ok {
			// 新增列
			goType := f.Type.String()
			sqlType, ok := typeMap[goType]
			if !ok {
				sqlType, ok = typeMap[f.Kind.String()]
				if !ok {
					sqlType = "TEXT"
				}
			}
			addCol := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, col, sqlType)
			if col == modelInfo.PkName {
				addCol += " PRIMARY KEY"
			}
			// 可扩展: NOT NULL, DEFAULT, UNIQUE
			sqls = append(sqls, addCol)
		}
	}

	// 4. 删除列
	for col := range dbCols {
		if _, ok := targetCols[col]; !ok {
			// 删除列（部分数据库不支持，需手动处理）
			if dialect == "sqlite" {
				sqls = append(sqls, fmt.Sprintf("-- [manual] DROP COLUMN %s from %s (SQLite needs table rebuild)", col, tableName))
			} else {
				sqls = append(sqls, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableName, col))
			}
		}
	}

	// 5. 修改列类型/主键/唯一约束（仅检测，实际变更需谨慎）
	for col, f := range targetCols {
		if dbCol, ok := dbCols[col]; ok {
			goType := f.Type.String()
			sqlType, ok := typeMap[goType]
			if !ok {
				sqlType, ok = typeMap[f.Kind.String()]
				if !ok {
					sqlType = "TEXT"
				}
			}
			if dbCol.DataType != sqlType {
				// 类型变更
				sqls = append(sqls, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", tableName, col, sqlType))
			}
			if dbCol.IsPrimary != (col == modelInfo.PkName) {
				// 主键变更
				sqls = append(sqls, fmt.Sprintf("-- [manual] PRIMARY KEY change for %s (may require table rebuild)", col))
			}
			if dbCol.IsUnique != isUniqueInModel(col, modelInfo) {
				if isUniqueInModel(col, modelInfo) {
					sqls = append(sqls, fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS uniq_%s_%s ON %s (%s)", tableName, col, tableName, col))
				} else {
					sqls = append(sqls, fmt.Sprintf("DROP INDEX IF EXISTS uniq_%s_%s", tableName, col))
				}
			}
		}
	}

	// 6. 索引变更（仅简单支持单列索引/唯一索引）
	// 可扩展: 多列索引、外键等
	return sqls, nil
}

// isUniqueInModel 判断列是否在 modelInfo 中声明为唯一索引
func isUniqueInModel(col string, modelInfo *ModelInfo) bool {
	for _, idx := range modelInfo.UniqueIndexes {
		if len(idx.Columns) == 1 && idx.Columns[0] == col {
			return true
		}
	}
	return false
}

// GenerateMigrationsTableSQL 返回 schema_migrations 版本表的建表 SQL，兼容多数据库
func GenerateMigrationsTableSQL(dialect string) (string, error) {
	switch dialect {
	case "mysql":
		return `CREATE TABLE IF NOT EXISTS schema_migrations (\n  id INT AUTO_INCREMENT PRIMARY KEY,\n  version VARCHAR(255) NOT NULL UNIQUE,\n  applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,\n  description VARCHAR(255)\n);`, nil
	case "postgres":
		return `CREATE TABLE IF NOT EXISTS schema_migrations (\n  id SERIAL PRIMARY KEY,\n  version VARCHAR(255) NOT NULL UNIQUE,\n  applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,\n  description VARCHAR(255)\n);`, nil
	case "sqlite":
		return `CREATE TABLE IF NOT EXISTS schema_migrations (\n  id INTEGER PRIMARY KEY AUTOINCREMENT,\n  version TEXT NOT NULL UNIQUE,\n  applied_at DATETIME DEFAULT CURRENT_TIMESTAMP,\n  description TEXT\n);`, nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}
