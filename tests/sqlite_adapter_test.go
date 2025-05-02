package thing_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	// For QueryParams used in GetCount test
	"github.com/burugo/thing/common"
	"github.com/burugo/thing/internal/drivers/db/sqlite"
	"github.com/burugo/thing/internal/interfaces"
	"github.com/burugo/thing/internal/schema"
	"github.com/burugo/thing/internal/types"

	_ "github.com/mattn/go-sqlite3" // Import driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestItem is a simple struct for adapter tests.
type TestItem struct {
	ID    int64     `db:"id"`
	Name  string    `db:"name"`
	Value float64   `db:"value"`
	Time  time.Time `db:"time_added"`
}

// setupSQLiteAdapterTest initializes an in-memory SQLite DB, creates a test table,
// and returns the adapter and a cleanup function.
func setupSQLiteAdapterTest(t *testing.T) (interfaces.DBAdapter, func()) {
	t.Helper()
	// Use a unique in-memory database for each test run
	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())
	// Or use a file-based DB for inspection:
	// dsn := "./test_adapter.db"
	// os.Remove(dsn) // Clean up previous run

	// Use the actual NewSQLiteAdapter constructor
	adapter, err := sqlite.NewSQLiteAdapter(dsn)
	require.NoError(t, err, "Failed to create SQLite adapter")
	require.NotNil(t, adapter, "Adapter should not be nil")

	// Get the underlying *sql.DB to create schema (adapter doesn't expose this)
	// This requires either exporting the db field or using a test helper/type assertion if possible.
	// For now, we'll re-open a connection just for setup, assuming the adapter uses the same DSN.
	// This is not ideal, but necessary without changing the adapter struct visibility.
	setupDB, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err, "Failed to open DB connection for setup")
	defer setupDB.Close()

	// Create a test table
	createTableSQL := `
	CREATE TABLE test_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		value REAL,
		time_added DATETIME
	);`
	_, err = setupDB.Exec(createTableSQL)
	require.NoError(t, err, "Failed to create test_items table")

	cleanup := func() {
		err := adapter.Close()
		assert.NoError(t, err, "Failed to close adapter")
		// If using a file DB: os.Remove(dsn)
	}

	return adapter, cleanup
}

// --- Test Cases Start Here ---

func TestNewSQLiteAdapter(t *testing.T) {
	adapter, cleanup := setupSQLiteAdapterTest(t)
	defer cleanup()
	require.NotNil(t, adapter)

	// Ping test implicitly done in setup, but could add explicit ping test if desired.
}

func TestSQLiteAdapter_Get_Found(t *testing.T) {
	adapter, cleanup := setupSQLiteAdapterTest(t)
	defer cleanup()
	ctx := context.Background()

	// Insert test data directly
	now := time.Now().Truncate(time.Second) // Truncate for comparison
	res, err := adapter.Exec(ctx, "INSERT INTO test_items (name, value, time_added) VALUES (?, ?, ?)", "TestGetName", 123.45, now)
	require.NoError(t, err)
	id, _ := res.LastInsertId()

	var item TestItem
	err = adapter.Get(ctx, &item, "SELECT id, name, value, time_added FROM test_items WHERE id = ?", id)

	require.NoError(t, err)
	assert.Equal(t, id, item.ID)
	assert.Equal(t, "TestGetName", item.Name)
	assert.Equal(t, 123.45, item.Value)
	assert.WithinDuration(t, now, item.Time, time.Second, "Time mismatch")
}

func TestSQLiteAdapter_Get_NotFound(t *testing.T) {
	adapter, cleanup := setupSQLiteAdapterTest(t)
	defer cleanup()
	ctx := context.Background()

	var item TestItem
	err := adapter.Get(ctx, &item, "SELECT id, name, value, time_added FROM test_items WHERE id = ?", 999)

	require.Error(t, err)
	assert.True(t, errors.Is(err, common.ErrNotFound), "Expected ErrNotFound")
}

func TestSQLiteAdapter_Select_Structs(t *testing.T) {
	adapter, cleanup := setupSQLiteAdapterTest(t)
	defer cleanup()
	ctx := context.Background()

	// Insert test data
	_, err := adapter.Exec(ctx, "INSERT INTO test_items (name, value) VALUES (?, ?), (?, ?)", "ItemA", 1.1, "ItemB", 2.2)
	require.NoError(t, err)

	var items []TestItem // Slice of structs
	err = adapter.Select(ctx, &items, "SELECT id, name, value FROM test_items ORDER BY name ASC")

	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "ItemA", items[0].Name)
	assert.Equal(t, 1.1, items[0].Value)
	assert.Equal(t, "ItemB", items[1].Name)
	assert.Equal(t, 2.2, items[1].Value)
}

func TestSQLiteAdapter_Select_PtrStructs(t *testing.T) {
	adapter, cleanup := setupSQLiteAdapterTest(t)
	defer cleanup()
	ctx := context.Background()

	// Insert test data
	_, err := adapter.Exec(ctx, "INSERT INTO test_items (name, value) VALUES (?, ?), (?, ?)", "PtrItemC", 3.3, "PtrItemD", 4.4)
	require.NoError(t, err)

	var items []*TestItem // Slice of pointers to structs
	err = adapter.Select(ctx, &items, "SELECT id, name, value FROM test_items ORDER BY name ASC")

	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "PtrItemC", items[0].Name)
	assert.Equal(t, 3.3, items[0].Value)
	assert.Equal(t, "PtrItemD", items[1].Name)
	assert.Equal(t, 4.4, items[1].Value)
}

func TestSQLiteAdapter_Select_BasicType(t *testing.T) {
	adapter, cleanup := setupSQLiteAdapterTest(t)
	defer cleanup()
	ctx := context.Background()

	// Insert test data
	_, err := adapter.Exec(ctx, "INSERT INTO test_items (name, value) VALUES (?, ?), (?, ?)", "BasicName1", 5.5, "BasicName2", 6.6)
	require.NoError(t, err)

	var names []string // Slice of basic type
	err = adapter.Select(ctx, &names, "SELECT name FROM test_items ORDER BY name ASC")

	require.NoError(t, err)
	require.Len(t, names, 2)
	assert.Equal(t, "BasicName1", names[0])
	assert.Equal(t, "BasicName2", names[1])

	var values []float64
	err = adapter.Select(ctx, &values, "SELECT value FROM test_items ORDER BY value ASC")
	require.NoError(t, err)
	require.Len(t, values, 2)
	assert.Equal(t, 5.5, values[0])
	assert.Equal(t, 6.6, values[1])
}

func TestSQLiteAdapter_Exec_InsertUpdateDelete(t *testing.T) {
	adapter, cleanup := setupSQLiteAdapterTest(t)
	defer cleanup()
	ctx := context.Background()

	// Insert
	res, err := adapter.Exec(ctx, "INSERT INTO test_items (name, value) VALUES (?, ?)", "ExecTest", 7.7)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	require.NotZero(t, id)
	rowsAffected, _ := res.RowsAffected()
	assert.EqualValues(t, 1, rowsAffected)

	// Verify Insert with Get
	var item TestItem
	err = adapter.Get(ctx, &item, "SELECT id, name, value FROM test_items WHERE id = ?", id)
	require.NoError(t, err)
	assert.Equal(t, "ExecTest", item.Name)

	// Update
	res, err = adapter.Exec(ctx, "UPDATE test_items SET name = ? WHERE id = ?", "ExecUpdated", id)
	require.NoError(t, err)
	rowsAffected, _ = res.RowsAffected()
	assert.EqualValues(t, 1, rowsAffected)

	// Verify Update with Get
	err = adapter.Get(ctx, &item, "SELECT id, name, value FROM test_items WHERE id = ?", id)
	require.NoError(t, err)
	assert.Equal(t, "ExecUpdated", item.Name)

	// Delete
	res, err = adapter.Exec(ctx, "DELETE FROM test_items WHERE id = ?", id)
	require.NoError(t, err)
	rowsAffected, _ = res.RowsAffected()
	assert.EqualValues(t, 1, rowsAffected)

	// Verify Delete with Get
	err = adapter.Get(ctx, &item, "SELECT id, name, value FROM test_items WHERE id = ?", id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, common.ErrNotFound))
}

func TestSQLiteAdapter_GetCount(t *testing.T) {
	adapter, cleanup := setupSQLiteAdapterTest(t)
	defer cleanup()
	ctx := context.Background()

	// Insert test data
	_, err := adapter.Exec(ctx, "INSERT INTO test_items (name, value) VALUES (?, ?), (?, ?), (?, ?)", "CountMe", 1.0, "CountMe", 2.0, "DontCount", 3.0)
	require.NoError(t, err)

	// Minimal ModelInfo needed for GetCount (TableName)
	info := &schema.ModelInfo{TableName: "test_items"}
	params := types.QueryParams{Where: "name = ?", Args: []interface{}{"CountMe"}}

	count, err := adapter.GetCount(ctx, info, params)
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)

	// Test zero count
	paramsZero := types.QueryParams{Where: "name = ?", Args: []interface{}{"NoSuchName"}}
	countZero, err := adapter.GetCount(ctx, info, paramsZero)
	require.NoError(t, err)
	assert.EqualValues(t, 0, countZero)

	// Test count all
	paramsAll := types.QueryParams{}
	countAll, err := adapter.GetCount(ctx, info, paramsAll)
	require.NoError(t, err)
	assert.EqualValues(t, 3, countAll)
}

func TestSQLiteAdapter_Transaction_Commit(t *testing.T) {
	adapter, cleanup := setupSQLiteAdapterTest(t)
	defer cleanup()
	ctx := context.Background()

	tx, err := adapter.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Perform operations within Tx
	res, err := tx.Exec(ctx, "INSERT INTO test_items (name, value) VALUES (?, ?)", "CommitTest", 10.1)
	require.NoError(t, err)
	id, _ := res.LastInsertId()

	// Verify within Tx (using tx.Get)
	var itemTx TestItem
	err = tx.Get(ctx, &itemTx, "SELECT name FROM test_items WHERE id = ?", id)
	require.NoError(t, err)
	assert.Equal(t, "CommitTest", itemTx.Name)

	// Commit
	err = tx.Commit()
	require.NoError(t, err)

	// Verify outside Tx (using adapter.Get)
	var itemAdapter TestItem
	err = adapter.Get(ctx, &itemAdapter, "SELECT name FROM test_items WHERE id = ?", id)
	require.NoError(t, err)
	assert.Equal(t, "CommitTest", itemAdapter.Name)
}

func TestSQLiteAdapter_Transaction_Rollback(t *testing.T) {
	adapter, cleanup := setupSQLiteAdapterTest(t)
	defer cleanup()
	ctx := context.Background()

	tx, err := adapter.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, tx)

	// Perform operations within Tx
	res, err := tx.Exec(ctx, "INSERT INTO test_items (name, value) VALUES (?, ?)", "RollbackTest", 11.2)
	require.NoError(t, err)
	id, _ := res.LastInsertId()

	// Rollback
	err = tx.Rollback()
	require.NoError(t, err)

	// Verify outside Tx (should not exist)
	var itemAdapter TestItem
	err = adapter.Get(ctx, &itemAdapter, "SELECT name FROM test_items WHERE id = ?", id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, common.ErrNotFound))
}
