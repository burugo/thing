package sql

import (
	"fmt"
	"log"
	"strings"
	"thing/internal/cache"
)

// BuildSelectSQL constructs a basic SELECT statement for all columns defined in ModelInfo.
// Note: This is now in the internal/sql package and takes explicit arguments.
func BuildSelectSQL(tableName string, columns []string) string {
	if tableName == "" || len(columns) == 0 {
		log.Printf("Error: BuildSelectSQL called with incomplete info: TableName='%s', Columns=%d", tableName, len(columns))
		return ""
	}
	quotedColumns := make([]string, len(columns))
	for i, col := range columns {
		// Basic quoting, might need dialect-specific quoting later
		quotedColumns[i] = fmt.Sprintf("\"%s\"", col)
	}
	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(quotedColumns, ", "), tableName)
}

// BuildSelectIDsSQL constructs a SELECT statement to fetch only primary key IDs.
// Note: Exported and takes explicit arguments.
func BuildSelectIDsSQL(tableName string, pkName string, params cache.QueryParams) (string, []interface{}) {
	var query strings.Builder
	args := []interface{}{}
	if tableName == "" || pkName == "" {
		log.Printf("Error: BuildSelectIDsSQL called with incomplete info: TableName='%s', PkName='%s'", tableName, pkName)
		return "", nil
	}
	query.WriteString(fmt.Sprintf("SELECT \"%s\" FROM %s", pkName, tableName))

	// Combine user WHERE with soft delete condition
	whereClause := params.Where
	// Use the flag from params directly
	if !params.IncludeDeleted {
		if whereClause != "" {
			whereClause = fmt.Sprintf("(%s) AND \"deleted\" = false", whereClause)
		} else {
			whereClause = "\"deleted\" = false"
		}
	}

	if whereClause != "" {
		query.WriteString(" WHERE ")
		query.WriteString(whereClause)
		args = append(args, params.Args...)
	}

	if params.Order != "" {
		query.WriteString(" ORDER BY ")
		query.WriteString(params.Order)
	}
	return query.String(), args
}
