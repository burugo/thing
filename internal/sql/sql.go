package sql

import (
	"fmt"
	"log"
	"strings"
)

// ModelInfo contains metadata about a database model.
type ModelInfo struct {
	TableName        string
	PkName           string            // Database name of the primary key field
	Columns          []string          // List of all database column names (including PK)
	Fields           []string          // Corresponding Go struct field names (order matches columns)
	FieldToColumnMap map[string]string // Map Go field name to its corresponding DB column name
	ColumnToFieldMap map[string]string // Map DB column name to its corresponding Go field name
}

// BuildSelectSQL constructs a SELECT statement.
func BuildSelectSQL(info *ModelInfo) string {
	if info.TableName == "" || len(info.Columns) == 0 {
		log.Printf("Error: BuildSelectSQL called with incomplete modelInfo: %+v", info)
		return ""
	}
	// Format: SELECT "col1", "col2", ... FROM tableName
	// Quote column names for safety (handles reserved words and special chars)
	quotedColumns := make([]string, len(info.Columns))
	for i, col := range info.Columns {
		quotedColumns[i] = fmt.Sprintf("\"%s\"", col)
	}
	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(quotedColumns, ", "), info.TableName)
}

// BuildSelectIDsSQL constructs a SELECT statement to fetch only primary key IDs.
func BuildSelectIDsSQL(info *ModelInfo, params interface{}) (string, []interface{}) {
	var query strings.Builder
	args := []interface{}{}

	// Params could be a QueryParams struct with Where, Args, Order, Limit, Start fields
	// For simplicity in this example, we'll use a simple interface approach
	type QueryParams struct {
		Where string
		Args  []interface{}
		Order string
		Limit int
		Start int
	}

	queryParams, ok := params.(QueryParams)
	if !ok {
		log.Printf("Error: params is not a QueryParams struct: %T", params)
		return "", nil
	}

	if info.TableName == "" || info.PkName == "" {
		log.Printf("Error: BuildSelectIDsSQL called with incomplete modelInfo: %+v", info)
		return "", nil
	}
	query.WriteString(fmt.Sprintf("SELECT \"%s\" FROM %s", info.PkName, info.TableName))
	if queryParams.Where != "" {
		query.WriteString(" WHERE ")
		query.WriteString(queryParams.Where)
		args = append(args, queryParams.Args...)
	}
	if queryParams.Order != "" {
		query.WriteString(" ORDER BY ")
		query.WriteString(queryParams.Order)
	}
	if queryParams.Limit > 0 {
		query.WriteString(" LIMIT ?")
		args = append(args, queryParams.Limit)
	}
	if queryParams.Start > 0 {
		query.WriteString(" OFFSET ?")
		args = append(args, queryParams.Start)
	}
	return query.String(), args
}

// BuildSelectSQLWithParams constructs SELECT SQL query including WHERE, ORDER, LIMIT, OFFSET.
func BuildSelectSQLWithParams(info *ModelInfo, params interface{}) (string, []interface{}) {
	baseQuery := BuildSelectSQL(info) // Use existing helper for SELECT ... FROM ...

	// Params could be a QueryParams struct with Where, Args, Order, Limit, Start fields
	type QueryParams struct {
		Where string
		Args  []interface{}
		Order string
		Limit int
		Start int
	}

	queryParams, ok := params.(QueryParams)
	if !ok {
		log.Printf("Error: params is not a QueryParams struct: %T", params)
		return "", nil
	}

	var whereClause string
	var args = queryParams.Args
	if queryParams.Where != "" {
		whereClause = " WHERE " + queryParams.Where
	}

	var orderClause string
	if queryParams.Order != "" {
		orderClause = " ORDER BY " + queryParams.Order
	}

	var limitClause string
	if queryParams.Limit > 0 {
		// Use placeholders for limit/offset for broader DB compatibility
		limitClause = " LIMIT ?"
		args = append(args, queryParams.Limit)
		if queryParams.Start > 0 {
			limitClause += " OFFSET ?"
			args = append(args, queryParams.Start)
		}
	}

	finalQuery := baseQuery + whereClause + orderClause + limitClause
	log.Printf("Built SQL: %s | Args: %v", finalQuery, args) // Debug log
	return finalQuery, args
}
