package sqlbuilder

import (
	"fmt"
	"log"
	"reflect"
	"strings"
	"thing/internal/types"
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
func BuildSelectIDsSQL(tableName string, pkName string, params types.QueryParams) (string, []interface{}) {
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
		// Expand IN (?) with slice args to IN (?, ?, ...)
		expandedWhere, expandedArgs := expandInClauses(whereClause, params.Args)
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
func expandInClauses(where string, args []interface{}) (string, []interface{}) {
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
						placeholders[j] = "?"
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
