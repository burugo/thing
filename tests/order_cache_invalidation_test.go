package thing_test

import (
	"testing"

	"github.com/burugo/thing"
	"github.com/stretchr/testify/require"
)

func TestQueryListCacheInvalidatesWhenOrderFieldChanges(t *testing.T) {
	th, mockCache, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	later := &User{BaseModel: thing.BaseModel{}, Name: "Order Cache", Email: "b@example.com"}
	earlier := &User{BaseModel: thing.BaseModel{}, Name: "Order Cache", Email: "a@example.com"}
	require.NoError(t, th.Save(later))
	require.NoError(t, th.Save(earlier))

	params := thing.QueryParams{
		Where: "name = ?",
		Args:  []interface{}{"Order Cache"},
		Order: "email ASC, id ASC",
	}

	initial, err := th.Query(params).Fetch(0, 10)
	require.NoError(t, err)
	require.Len(t, initial, 2)
	require.Equal(t, earlier.ID, initial[0].ID)
	require.Equal(t, later.ID, initial[1].ID)
	listKey := thing.GenerateCacheKey("list", "users", params)
	countKey := thing.GenerateCacheKey("count", "users", params)
	_, listFound := mockCache.GetValue(listKey)
	_, countFound := mockCache.GetValue(countKey)
	require.True(t, listFound)
	require.True(t, countFound)

	later.Email = "0@example.com"
	require.NoError(t, th.Save(later))
	_, listFound = mockCache.GetValue(listKey)
	_, countFound = mockCache.GetValue(countKey)
	require.False(t, listFound)
	require.True(t, countFound)

	afterUpdate, err := th.Query(params).Fetch(0, 10)
	require.NoError(t, err)
	require.Len(t, afterUpdate, 2)
	require.Equal(t, later.ID, afterUpdate[0].ID)
	require.Equal(t, earlier.ID, afterUpdate[1].ID)
}
