package sqlbuilder

import (
	"fmt"
	"log"
	"reflect"
	"strings"
	"thing/internal/types"
)

// Dialector defines how to quote identifiers and bind variables for a specific SQL dialect.
type Dialector interface {
	Quote(identifier string) string // Quote a SQL identifier (table/column name)
	Placeholder(index int) string   // Bind variable placeholder (e.g. ?, $1)
}

// SQLBuilder provides SQL generation with dialect-specific identifier quoting.
type SQLBuilder struct {
	Dialect Dialector
}

// NewSQLBuilder creates a new SQLBuilder with the given Dialector.
func NewSQLBuilder(dialect Dialector) *SQLBuilder {
	return &SQLBuilder{Dialect: dialect}
}

// BuildSelectSQL constructs a basic SELECT statement for all columns defined in ModelInfo.
func (b *SQLBuilder) BuildSelectSQL(tableName string, columns []string) string {
	if tableName == "" || len(columns) == 0 {
		log.Printf("Error: BuildSelectSQL called with incomplete info: TableName='%s', Columns=%d", tableName, len(columns))
		return ""
	}
	quotedColumns := make([]string, len(columns))
	for i, col := range columns {
		quotedColumns[i] = b.Dialect.Quote(col)
	}
	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(quotedColumns, ", "), b.Dialect.Quote(tableName))
}

// BuildSelectIDsSQL constructs a SELECT statement to fetch only primary key IDs.
// Note: Exported and takes explicit arguments.
func (b *SQLBuilder) BuildSelectIDsSQL(tableName string, pkName string, params types.QueryParams) (string, []interface{}) {
	var query strings.Builder
	args := []interface{}{}
	if tableName == "" || pkName == "" {
		log.Printf("Error: BuildSelectIDsSQL called with incomplete info: TableName='%s', PkName='%s'", tableName, pkName)
		return "", nil
	}
	query.WriteString(fmt.Sprintf("SELECT %s FROM %s", b.Dialect.Quote(pkName), b.Dialect.Quote(tableName)))

	// Combine user WHERE with soft delete condition
	whereClause := params.Where
	// Use the flag from params directly
	if !params.IncludeDeleted {
		if whereClause != "" {
			whereClause = fmt.Sprintf("(%s) AND %s = false", whereClause, b.Dialect.Quote("deleted"))
		} else {
			whereClause = fmt.Sprintf("%s = false", b.Dialect.Quote("deleted"))
		}
	}

	if whereClause != "" {
		// Expand IN (?) with slice args to IN (?, ?, ...)
		expandedWhere, expandedArgs := b.expandInClauses(whereClause, params.Args)
		query.WriteString(" WHERE ")
		query.WriteString(expandedWhere)
		args = append(args, expandedArgs...)
	} else {
		args = append(args, params.Args...)
	}

	if params.Order != "" {
		query.WriteString(" ORDER BY ")
		query.WriteString(params.Order)
	}
	return query.String(), args
}

// expandInClauses replaces IN (?) with the correct number of placeholders and flattens slice args.
func (b *SQLBuilder) expandInClauses(where string, args []interface{}) (string, []interface{}) {
	var newWhere strings.Builder
	newArgs := make([]interface{}, 0, len(args))
	argIdx := 0
	conditions := strings.Split(where, " AND ")
	for i, cond := range conditions {
		if i > 0 {
			newWhere.WriteString(" AND ")
		}
		cond = strings.TrimSpace(cond)
		// Preserve parentheses around the condition
		openParen := strings.HasPrefix(cond, "(")
		closeParen := strings.HasSuffix(cond, ")")
		coreCond := cond
		if openParen {
			coreCond = coreCond[1:]
		}
		if closeParen && len(coreCond) > 0 {
			coreCond = coreCond[:len(coreCond)-1]
		}
		if strings.Contains(coreCond, "IN (?)") && argIdx < len(args) {
			prefix := coreCond[:strings.Index(coreCond, "IN (?)")+2] // up to 'IN'
			arg := args[argIdx]
			sliceVal := reflect.ValueOf(arg)
			if sliceVal.Kind() == reflect.Slice || sliceVal.Kind() == reflect.Array {
				n := sliceVal.Len()
				if n == 0 {
					// IN () is invalid SQL, but we can use IN (NULL) to ensure no match
					coreCond = prefix + "(NULL)"
				} else {
					placeholders := make([]string, n)
					for j := 0; j < n; j++ {
						placeholders[j] = b.Dialect.Placeholder(j + 1) // Use dialect-specific placeholders
						newArgs = append(newArgs, sliceVal.Index(j).Interface())
					}
					coreCond = prefix + "(" + strings.Join(placeholders, ", ") + ")"
				}
				argIdx++
				// Re-wrap with parentheses if needed
				if openParen {
					coreCond = "(" + coreCond
				}
				if closeParen {
					coreCond = coreCond + ")"
				}
				newWhere.WriteString(coreCond)
				continue
			}
		}
		// Not an IN clause, or not a slice arg
		if openParen {
			coreCond = "(" + coreCond
		}
		if closeParen {
			coreCond = coreCond + ")"
		}
		newWhere.WriteString(coreCond)
		if argIdx < len(args) {
			newArgs = append(newArgs, args[argIdx])
			argIdx++
		}
	}
	return newWhere.String(), newArgs
}

// BuildInsertSQL constructs an INSERT statement for the given table and columns.
func (b *SQLBuilder) BuildInsertSQL(tableName string, columns []string) string {
	if tableName == "" || len(columns) == 0 {
		log.Printf("Error: BuildInsertSQL called with incomplete info: TableName='%s', Columns=%d", tableName, len(columns))
		return ""
	}
	quotedCols := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, c := range columns {
		quotedCols[i] = b.Dialect.Quote(c)
		placeholders[i] = b.Dialect.Placeholder(i + 1) // Use dialect-specific placeholders
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", b.Dialect.Quote(tableName), strings.Join(quotedCols, ", "), strings.Join(placeholders, ", "))
}

// BuildUpdateSQL constructs an UPDATE statement for the given table, set clauses, and primary key.
func (b *SQLBuilder) BuildUpdateSQL(tableName string, setClauses []string, pkName string) string {
	if tableName == "" || len(setClauses) == 0 || pkName == "" {
		log.Printf("Error: BuildUpdateSQL called with incomplete info: TableName='%s', SetClauses=%d, PkName='%s'", tableName, len(setClauses), pkName)
		return ""
	}
	// Generate set clauses with incrementing placeholders
	setParts := make([]string, len(setClauses))
	for i, clause := range setClauses {
		// clause is like "col = ?", replace ? with correct placeholder
		setParts[i] = strings.Replace(clause, "?", b.Dialect.Placeholder(i+1), 1)
	}
	// WHERE uses next placeholder
	wherePlaceholder := b.Dialect.Placeholder(len(setClauses) + 1)
	return fmt.Sprintf("UPDATE %s SET %s WHERE %s = %s",
		b.Dialect.Quote(tableName),
		strings.Join(setParts, ", "),
		b.Dialect.Quote(pkName),
		wherePlaceholder)
}

// BuildDeleteSQL constructs a DELETE statement for the given table and primary key.
func (b *SQLBuilder) BuildDeleteSQL(tableName, pkName string) string {
	if tableName == "" || pkName == "" {
		log.Printf("Error: BuildDeleteSQL called with incomplete info: TableName='%s', PkName='%s'", tableName, pkName)
		return ""
	}
	return fmt.Sprintf("DELETE FROM %s WHERE %s = %s",
		b.Dialect.Quote(tableName),
		b.Dialect.Quote(pkName),
		b.Dialect.Placeholder(1))
}

// BuildCountSQL constructs a SELECT COUNT(*) statement for the given table and optional WHERE clause.
func (b *SQLBuilder) BuildCountSQL(tableName string, whereClause string) string {
	if tableName == "" {
		log.Printf("Error: BuildCountSQL called with incomplete info: TableName='%s'", tableName)
		return ""
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", b.Dialect.Quote(tableName))
	if whereClause != "" {
		query = fmt.Sprintf("%s WHERE %s", query, whereClause)
	}
	return query
}

// Rebind transforms SQL prepared statements using ? placeholders to the dialect's specific placeholders.
// For example: PostgreSQL uses $1, $2, etc.
func (b *SQLBuilder) Rebind(query string) string {
	// For MySQL and SQLite, ? placeholders are already correct

	// Replace ? with $1, $2, etc. (must increment index for each ?)
	var (
		result strings.Builder
		idx    = 1
	)
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			result.WriteString(b.Dialect.Placeholder(idx))
			idx++
		} else {
			result.WriteByte(query[i])
		}
	}
	return result.String()
}
