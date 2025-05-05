package schema

import (
	"fmt"
	"log"
	"sort"
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

// GenerateCreateTableSQL 生成单表 CREATE TABLE 语句及后续的 CREATE INDEX 语句
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
	// Use IF NOT EXISTS for table creation for better idempotency
	createTableSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n);", tableName, strings.Join(cols, ",\n  "))

	// --- Index SQL Generation --- (Modified logic)
	var indexSQLs []string
	mergedIndexes := make(map[string]IndexInfo) // name -> consolidated IndexInfo

	populateMergedMap := func(indexes []IndexInfo, unique bool) {
		for _, idx := range indexes {
			if len(idx.Columns) == 0 {
				continue
			}

			idxName := idx.Name
			// Generate default name for single-column unnamed indexes
			if idxName == "" && len(idx.Columns) == 1 {
				if unique {
					idxName = fmt.Sprintf("uniq_%s_%s", tableName, idx.Columns[0])
				} else {
					idxName = fmt.Sprintf("idx_%s_%s", tableName, idx.Columns[0])
				}
			} else if idxName == "" && len(idx.Columns) > 1 {
				log.Printf("Warning (CREATE): Composite index on table '%s' columns [%s] has no explicit name and will be skipped.", tableName, strings.Join(idx.Columns, ", "))
				continue
			}

			if existing, ok := mergedIndexes[idxName]; ok {
				// Index name exists, merge columns (ensure unique)
				if existing.Unique != unique {
					log.Printf("Warning (CREATE): Conflicting uniqueness for index name '%s' on table '%s'. Skipping merge.", idxName, tableName)
					continue
				}
				existingColumnsMap := make(map[string]bool)
				for _, col := range existing.Columns {
					existingColumnsMap[col] = true
				}
				for _, newCol := range idx.Columns {
					if !existingColumnsMap[newCol] {
						existing.Columns = append(existing.Columns, newCol)
						existingColumnsMap[newCol] = true
					}
				}
				// Sort merged columns for consistency
				sort.Strings(existing.Columns)
				mergedIndexes[idxName] = existing // Update map with merged columns
			} else {
				// New index name, add to map
				// Sort columns before adding
				colsCopy := make([]string, len(idx.Columns))
				copy(colsCopy, idx.Columns)
				sort.Strings(colsCopy)
				mergedIndexes[idxName] = IndexInfo{Name: idxName, Columns: colsCopy, Unique: unique}
			}
		}
	}

	populateMergedMap(info.Indexes, false)
	populateMergedMap(info.UniqueIndexes, true)

	// Generate SQL from the merged map
	// Sort index names for deterministic output order
	var sortedIndexNames []string
	for name := range mergedIndexes {
		sortedIndexNames = append(sortedIndexNames, name)
	}
	sort.Strings(sortedIndexNames)

	for _, name := range sortedIndexNames {
		idx := mergedIndexes[name]
		indexSQL := generateCreateIndexSQL(tableName, idx.Name, idx.Columns, idx.Unique, dialect)
		if indexSQL != "" {
			indexSQLs = append(indexSQLs, indexSQL)
		}
	}

	allSQL := createTableSQL
	if len(indexSQLs) > 0 {
		// Add a separator before index statements
		allSQL += "\n\n-- Indexes --\n" + strings.Join(indexSQLs, "\n")
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
			// if dialect == "sqlite" {
			// 	sqls = append(sqls, fmt.Sprintf("-- [manual] DROP COLUMN %s from %s (SQLite needs table rebuild)", col, tableName))
			// } else {
			// 	sqls = append(sqls, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableName, col))
			// }
			// 禁止自动 DROP COLUMN，统一输出注释
			sqls = append(sqls, fmt.Sprintf("-- [manual] DROP COLUMN %s from %s (column exists in DB but not in struct)", col, tableName))
		}
	}

	// 5. 修改列类型/主键（仅检测，实际变更需谨慎）
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
		}
	}

	// --- 6. 索引变更 (Modified logic) ---
	modelIndexes := make(map[string]IndexInfo) // name -> IndexInfo (combined unique/non-unique)
	dbIndexes := make(map[string]IndexInfo)    // name -> IndexInfo (combined unique/non-unique)

	// Populate modelIndexes map (generate default names for single-column unnamed)
	populateIndexMap := func(indexes []IndexInfo, unique bool) {
		for _, idx := range indexes {
			if len(idx.Columns) == 0 {
				continue
			}
			// *** Add default name generation for single-column unnamed indexes ***
			idxName := idx.Name
			if idxName == "" && len(idx.Columns) == 1 {
				if unique {
					idxName = fmt.Sprintf("uniq_%s_%s", tableName, idx.Columns[0])
				} else {
					idxName = fmt.Sprintf("idx_%s_%s", tableName, idx.Columns[0])
				}
			} else if idxName == "" && len(idx.Columns) > 1 {
				log.Printf("Warning (ALTER): Composite index on table '%s' columns [%s] has no explicit name and will be skipped.", tableName, strings.Join(idx.Columns, ", "))
				continue
			}

			mapKey := idxName // Use generated or provided name as map key
			if existing, ok := modelIndexes[mapKey]; ok {
				if existing.Unique != unique {
					log.Printf("Warning (ALTER): Conflicting uniqueness for index name '%s' on table '%s'. Skipping definition.", mapKey, tableName)
					continue
				}
				if !equalStringSlice(existing.Columns, idx.Columns) {
					log.Printf("Warning (ALTER): Index name '%s' on table '%s' used for different column sets? Skipping definition.", mapKey, tableName)
					continue
				}
			} else {
				modelIndexes[mapKey] = IndexInfo{Name: idxName, Columns: idx.Columns, Unique: unique}
			}
		}
	}
	populateIndexMap(modelInfo.Indexes, false)
	populateIndexMap(modelInfo.UniqueIndexes, true)

	// Populate dbIndexes map (using names from DB)
	for _, idx := range tableInfo.Indexes {
		if strings.HasPrefix(idx.Name, "sqlite_autoindex_") {
			continue
		} // Skip SQLite auto PK index
		// Sort columns from DB for consistent comparison
		cols := make([]string, len(idx.Columns))
		copy(cols, idx.Columns)
		sort.Strings(cols)
		dbIndexes[idx.Name] = IndexInfo{Name: idx.Name, Columns: cols, Unique: idx.Unique}
	}

	// Find indexes to ADD
	for name, mIdx := range modelIndexes {
		// log.Printf("[DEBUG ALTER ADD] Checking model index: Name='%s', Unique=%v, Columns=%v", name, mIdx.Unique, mIdx.Columns)
		if dbIdx, exists := dbIndexes[name]; !exists {
			// log.Printf("[DEBUG ALTER ADD] Index '%s' not in DB. Generating CREATE statement.", name)
			// Index exists in model but not in DB -> ADD
			createSQL := generateCreateIndexSQL(tableName, mIdx.Name, mIdx.Columns, mIdx.Unique, dialect)
			sqls = append(sqls, createSQL)
		} else {
			// log.Printf("[DEBUG ALTER ADD] Index '%s' exists in DB. Checking for changes.", name)
			// Index exists in both: Check for changes (Unique status or Columns)
			// Note: Changing columns usually requires DROP + ADD, handled below by drop logic first.
			if mIdx.Unique != dbIdx.Unique {
				log.Printf("Info (ALTER): Index '%s' on table '%s' needs uniqueness change (requires DROP+ADD).", name, tableName)
				dropSQL := generateDropIndexSQL(tableName, name, dialect)
				// Avoid adding duplicate drop/create if columns also changed
				if !containsSQL(sqls, dropSQL) {
					sqls = append(sqls, dropSQL)
				}
				createSQL := generateCreateIndexSQL(tableName, mIdx.Name, mIdx.Columns, mIdx.Unique, dialect)
				if !containsSQL(sqls, createSQL) {
					sqls = append(sqls, createSQL)
				}
			} else if !equalStringSlice(mIdx.Columns, dbIdx.Columns) {
				// Columns differ, requires DROP + ADD
				log.Printf("Info (ALTER): Index '%s' on table '%s' needs column change (requires DROP+ADD). Model:[%s], DB:[%s]", name, tableName, strings.Join(mIdx.Columns, ","), strings.Join(dbIdx.Columns, ","))
				dropSQL := generateDropIndexSQL(tableName, name, dialect)
				if !containsSQL(sqls, dropSQL) {
					sqls = append(sqls, dropSQL)
				}
				createSQL := generateCreateIndexSQL(tableName, mIdx.Name, mIdx.Columns, mIdx.Unique, dialect)
				if !containsSQL(sqls, createSQL) {
					sqls = append(sqls, createSQL)
				}
			}
		}
	}

	// Find indexes to DROP
	for name, dbIdx := range dbIndexes {
		modelIdx, exists := modelIndexes[name]
		if !exists || modelIdx.Unique != dbIdx.Unique || !equalStringSlice(modelIdx.Columns, dbIdx.Columns) {
			// Drop if: not in model OR uniqueness differs OR columns differ (and wasn't handled by ADD/MODIFY above)
			// Avoid dropping generated indexes if the column still exists and requires one
			// Check if the model index that *should* exist (based on columns/unique) has a *different* name
			shouldDrop := true
			potentialModelMatchName := ""
			for mName, mIdx := range modelIndexes {
				if equalStringSlice(dbIdx.Columns, mIdx.Columns) && dbIdx.Unique == mIdx.Unique {
					potentialModelMatchName = mName
					break
				}
			}

			if !exists && potentialModelMatchName != "" && potentialModelMatchName != name {
				// Index columns/type match a model index, but the name is different.
				// This suggests a rename occurred OR the DB index uses a default name while model uses explicit.
				// Avoid dropping the DB index in this case. A RENAME would be ideal but complex.
				log.Printf("Info (ALTER): Index '%s' on table '%s' seems to match model index '%s' with different name. Skipping drop.", name, tableName, potentialModelMatchName)
				shouldDrop = false
			}

			// If the DB index didn't match any model index by name, and doesn't match any model index by definition (cols/unique) either,
			// then it's truly orphaned and should be dropped.
			if exists && (modelIdx.Unique != dbIdx.Unique || !equalStringSlice(modelIdx.Columns, dbIdx.Columns)) {
				// Already handled by ADD section (DROP + ADD)
				shouldDrop = false
			}

			if shouldDrop {
				dropSQL := generateDropIndexSQL(tableName, name, dialect)
				if !containsSQL(sqls, dropSQL) { // Avoid duplicates if DROP+ADD already added it
					sqls = append(sqls, dropSQL)
				}
			}
		}
	}

	return sqls, nil
}

// --- Helper Functions ---

// containsSQL checks if a slice already contains a specific SQL string.
func containsSQL(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// generateCreateIndexSQL generates the CREATE INDEX statement.
func generateCreateIndexSQL(tableName, indexName string, columns []string, unique bool, dialect string) string {
	if len(columns) == 0 {
		return "" // Should not happen
	}
	stmt := "CREATE"
	if unique {
		stmt += " UNIQUE"
	}
	stmt += " INDEX"
	if dialect == "sqlite" || dialect == "postgres" {
		stmt += " IF NOT EXISTS"
	}
	// Basic quoting for safety, might need dialect-specific quoting improvement
	quotedTableName := fmt.Sprintf("`%s`", tableName) // Basic quoting
	quotedIndexName := fmt.Sprintf("`%s`", indexName)
	if dialect == "postgres" {
		quotedTableName = fmt.Sprintf(`"%s"`, tableName)
		quotedIndexName = fmt.Sprintf(`"%s"`, indexName)
	}

	// Quote each column name appropriately for the dialect
	quotedColumnsList := make([]string, len(columns))
	for i, col := range columns {
		if dialect == "postgres" {
			quotedColumnsList[i] = fmt.Sprintf(`"%s"`, col)
		} else {
			quotedColumnsList[i] = fmt.Sprintf("`%s`", col) // Basic quoting for mysql/sqlite
		}
	}

	// Join the quoted column names with commas
	columnsSQL := strings.Join(quotedColumnsList, ", ")

	return fmt.Sprintf("%s %s ON %s (%s);", stmt, quotedIndexName, quotedTableName, columnsSQL)
}

// generateDropIndexSQL generates the DROP INDEX statement.
func generateDropIndexSQL(tableName, indexName string, dialect string) string {
	// Basic quoting for safety
	quotedIndexName := fmt.Sprintf("`%s`", indexName)
	quotedTableName := fmt.Sprintf("`%s`", tableName)
	if dialect == "postgres" {
		quotedIndexName = fmt.Sprintf(`"%s"`, indexName)
		quotedTableName = fmt.Sprintf(`"%s"`, tableName)
	}

	switch dialect {
	case "mysql":
		// MySQL uses ALTER TABLE or DROP INDEX ... ON ...
		return fmt.Sprintf("DROP INDEX %s ON %s;", quotedIndexName, quotedTableName)
	case "postgres", "sqlite":
		return fmt.Sprintf("DROP INDEX IF EXISTS %s;", quotedIndexName)
	default:
		return fmt.Sprintf("-- Unsupported dialect for DROP INDEX: %s", dialect) // Or return error
	}
}

// equalStringSlice checks if two string slices contain the same elements, regardless of order.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	// Create copies and sort them for comparison
	sa := make([]string, len(a))
	copy(sa, a)
	sort.Strings(sa)
	sb := make([]string, len(b))
	copy(sb, b)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

// GenerateMigrationsTableSQL 返回 schema_migrations 版本表的建表 SQL，兼容多数据库
func GenerateMigrationsTableSQL(dialect string) (string, error) {
	switch dialect {
	case "mysql":
		return `CREATE TABLE IF NOT EXISTS schema_migrations (
  id INT AUTO_INCREMENT PRIMARY KEY,
  version VARCHAR(255) NOT NULL UNIQUE,
  applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  description VARCHAR(255)
);`, nil
	case "postgres":
		return `CREATE TABLE IF NOT EXISTS schema_migrations (
  id SERIAL PRIMARY KEY,
  version VARCHAR(255) NOT NULL UNIQUE,
  applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  description VARCHAR(255)
);`, nil
	case "sqlite":
		return `CREATE TABLE IF NOT EXISTS schema_migrations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  version TEXT NOT NULL UNIQUE,
  applied_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  description TEXT
);`, nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", dialect)
	}
}
