package thing_test

import (
	"testing"

	"github.com/burugo/thing/drivers/db/postgres"
	"github.com/burugo/thing/internal/sqlbuilder"
	"github.com/stretchr/testify/require"
)

func TestExpandInClausesExpandsSliceWhenInClauseIsNotLastCondition(t *testing.T) {
	cases := []struct {
		name        string
		where       string
		args        []interface{}
		wantWhere   string
		wantRebound string
		wantArgs    []interface{}
	}{
		{
			name:        "in last condition stays expanded",
			where:       `(user_id = ? AND id IN (?)) AND "deleted" = false`,
			args:        []interface{}{int64(99), []int64{10, 20, 30}},
			wantWhere:   `(user_id = ? AND id IN (?, ?, ?)) AND "deleted" = false`,
			wantRebound: `(user_id = $1 AND id IN ($2, $3, $4)) AND "deleted" = false`,
			wantArgs:    []interface{}{int64(99), int64(10), int64(20), int64(30)},
		},
		{
			name:        "in before scalar expands",
			where:       `(id IN (?) AND status = ?) AND "deleted" = false`,
			args:        []interface{}{[]int64{10, 20, 30}, "active"},
			wantWhere:   `(id IN (?, ?, ?) AND status = ?) AND "deleted" = false`,
			wantRebound: `(id IN ($1, $2, $3) AND status = $4) AND "deleted" = false`,
			wantArgs:    []interface{}{int64(10), int64(20), int64(30), "active"},
		},
		{
			name:        "multiple in clauses expand in argument order",
			where:       `(owner_id IN (?) AND id IN (?) AND status = ?) AND "deleted" = false`,
			args:        []interface{}{[]int64{7, 8}, []int64{10, 20, 30}, "active"},
			wantWhere:   `(owner_id IN (?, ?) AND id IN (?, ?, ?) AND status = ?) AND "deleted" = false`,
			wantRebound: `(owner_id IN ($1, $2) AND id IN ($3, $4, $5) AND status = $6) AND "deleted" = false`,
			wantArgs:    []interface{}{int64(7), int64(8), int64(10), int64(20), int64(30), "active"},
		},
		{
			name:        "empty in slice renders null and consumes no bind args",
			where:       `(id IN (?) AND status = ?) AND "deleted" = false`,
			args:        []interface{}{[]int64{}, "active"},
			wantWhere:   `(id IN (NULL) AND status = ?) AND "deleted" = false`,
			wantRebound: `(id IN (NULL) AND status = $1) AND "deleted" = false`,
			wantArgs:    []interface{}{"active"},
		},
		{
			name:        "not in slice expands",
			where:       `(status NOT IN (?) AND owner_id = ?) AND "deleted" = false`,
			args:        []interface{}{[]string{"deleted", "archived"}, int64(7)},
			wantWhere:   `(status NOT IN (?, ?) AND owner_id = ?) AND "deleted" = false`,
			wantRebound: `(status NOT IN ($1, $2) AND owner_id = $3) AND "deleted" = false`,
			wantArgs:    []interface{}{"deleted", "archived", int64(7)},
		},
	}

	builder := sqlbuilder.NewSQLBuilder(postgres.Dialector{})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expandedWhere, expandedArgs := sqlbuilder.ExpandInClauses(postgres.Dialector{}, tc.where, tc.args)
			reboundWhere := builder.Rebind(expandedWhere)

			require.Equal(t, tc.wantWhere, expandedWhere)
			require.Equal(t, tc.wantRebound, reboundWhere)
			require.Equal(t, tc.wantArgs, expandedArgs)
		})
	}
}
