package thing_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/burugo/thing"
	"github.com/burugo/thing/drivers/db/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test Models ---

// User represents a user for testing.
type User struct {
	thing.BaseModel         // Use embedding again
	Name            string  `db:"name"`
	Email           string  `db:"email,unique"`
	Books           []*Book `thing:"hasMany;fk:user_id;model:Book" db:"-"`
}

// Change TableName to pointer receiver to satisfy Model[T] interface directly
func (u *User) TableName() string {
	return "users"
}

// Book represents a book for testing.
type Book struct {
	thing.BaseModel
	Title  string `db:"title"`
	UserID int64  `db:"user_id,index"`
	User   *User  `thing:"belongsTo;fk:user_id" db:"-"`
}

// Change TableName to pointer receiver
func (b *Book) TableName() string {
	return "books"
}

// Index declaration test model
// TestIndexModel is used to test AutoMigrate index/unique index

type TestIndexModel struct {
	ID    int64  `db:"id,pk"`
	Name  string `db:"name,index"`
	Email string `db:"email,unique"`
}

func (t *TestIndexModel) TableName() string { return "test_index_models" }

type DropIdxV1 struct {
	ID   int64  `db:"id,pk"`
	ColA string `db:"col_a,index:idx_ab"`
	ColB int    `db:"col_b,index:idx_ab"`
	ColC string `db:"col_c"`
}

func (m *DropIdxV1) TableName() string { return "drop_idx_models" }

type DropIdxV2 struct {
	ID   int64  `db:"id,pk"`
	ColA string `db:"col_a"`
	ColB int    `db:"col_b"`
	ColC string `db:"col_c"`
}

func (m *DropIdxV2) TableName() string { return "drop_idx_models" }

// --- Models for Index Testing ---

// testBaseModel for index tests
type testBaseModel struct {
	ID        int64     `db:"id,pk"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
	DeletedAt time.Time `db:"deleted_at,index"` // Simple index on base model
}

// New models for composite index testing
type TestCompositeIndexModel struct {
	testBaseModel
	ColA string `db:"col_a,index:idx_ab"`
	ColB int    `db:"col_b,index:idx_ab"` // Part of idx_ab
	ColC string `db:"col_c,index:idx_cd"`
	ColD bool   `db:"col_d,index:idx_cd"` // Part of idx_cd
}

func (m *TestCompositeIndexModel) TableName() string { return "composite_index_models" }

type TestCompositeUniqueIndexModel struct {
	testBaseModel
	KeyA string `db:"key_a,unique:uq_ab"`
	KeyB string `db:"key_b,unique:uq_ab"` // Part of uq_ab
	KeyC int    `db:"key_c"`
}

func (m *TestCompositeUniqueIndexModel) TableName() string { return "composite_unique_index_models" }

type TestMixedIndexesModel struct {
	testBaseModel
	Field1   string `db:"f1,index"`
	Field2   string `db:"f2,unique"`
	Field3   int    `db:"f3,index:idx_comp"`
	Field4   bool   `db:"f4,index:idx_comp"`
	Field5   string `db:"f5,unique:uq_comp"`
	Field6   string `db:"f6,unique:uq_comp"`
	Relation string `thing:"belongsTo;fk:rel_id" db:"-"`
	RelID    int64  `db:"rel_id,index"`
}

func (m *TestMixedIndexesModel) TableName() string { return "mixed_indexes_models" }

// --- End of Models for Index Testing ---

func TestAutoMigrate_IndexAndUnique(t *testing.T) {
	// Use SQLite in-memory database
	adapter, err := sqlite.NewSQLiteAdapter(":memory:")
	require.NoError(t, err)
	// Provide a non-nil cache client (use setupCacheTest or similar if available)
	_, cache, cleanup := setupTestDB(t)
	defer cleanup()
	err = thing.Configure(adapter, cache)
	require.NoError(t, err)
	db := adapter.DB()

	// Auto migrate
	err = thing.AutoMigrate(&TestIndexModel{})
	require.NoError(t, err)

	// Query sqlite_master to verify indexes
	rows, err := db.Query(`SELECT name, sql FROM sqlite_master WHERE type='index' AND tbl_name='test_index_models'`)
	require.NoError(t, err)
	defer rows.Close()

	var foundIndex, foundUnique bool
	for rows.Next() {
		var name, sql string
		require.NoError(t, rows.Scan(&name, &sql))
		if name == "idx_test_index_models_name" && sql != "" {
			foundIndex = true
		}
		if name == "uniq_test_index_models_email" && sql != "" && strings.Contains(sql, "UNIQUE") {
			foundUnique = true
		}
	}
	require.True(t, foundIndex, "Index was not created")
	require.True(t, foundUnique, "Unique index was not created")
}

func TestAutoMigrate_CompositeIndexes(t *testing.T) {
	dbs := []struct {
		name    string
		setup   func(tb testing.TB) (thing.DBAdapter, *mockCacheClient, func())
		getIdx  func(t *testing.T, db thing.DBAdapter, tableName string) map[string][]string // indexName -> sorted columns
		getUniq func(t *testing.T, db thing.DBAdapter, tableName string) map[string][]string // uniqueIndexName -> sorted columns
	}{
		{
			name:  "SQLite",
			setup: setupTestDB,
			getIdx: func(t *testing.T, db thing.DBAdapter, tableName string) map[string][]string {
				indexes := make(map[string][]string)
				query := fmt.Sprintf("PRAGMA index_list(%s);", tableName)
				rows, err := db.DB().Query(query)
				require.NoError(t, err)
				defer rows.Close()
				for rows.Next() {
					var seq, unique, partial int
					var name, origin string
					require.NoError(t, rows.Scan(&seq, &name, &unique, &origin, &partial))
					if strings.HasPrefix(name, "sqlite_autoindex_") || unique == 1 { // Skip auto PK and unique indexes
						continue
					}
					indexes[name] = getSQLiteIndexColumns(t, db, name)
				}
				return indexes
			},
			getUniq: func(t *testing.T, db thing.DBAdapter, tableName string) map[string][]string {
				indexes := make(map[string][]string)
				query := fmt.Sprintf("PRAGMA index_list(%s);", tableName)
				rows, err := db.DB().Query(query)
				require.NoError(t, err)
				defer rows.Close()
				for rows.Next() {
					var seq, unique, partial int
					var name, origin string
					require.NoError(t, rows.Scan(&seq, &name, &unique, &origin, &partial))
					if strings.HasPrefix(name, "sqlite_autoindex_") || unique == 0 { // Skip auto PK and non-unique indexes
						continue
					}
					indexes[name] = getSQLiteIndexColumns(t, db, name)
				}
				return indexes
			},
		},
		// TODO: Add MySQL and Postgres checks if setup functions are available and logic is implemented
	}

	for _, dbcase := range dbs {
		t.Run(dbcase.name, func(t *testing.T) {
			db, cache, cleanup := dbcase.setup(t)
			defer cleanup()
			require.NoError(t, thing.Configure(db, cache))

			// Migrate all test models
			err := thing.AutoMigrate(
				&TestCompositeIndexModel{},
				&TestCompositeUniqueIndexModel{},
				&TestMixedIndexesModel{},
			)
			require.NoError(t, err)

			// --- Verify TestCompositeIndexModel --- (Table: composite_index_models)
			idxComp := dbcase.getIdx(t, db, "composite_index_models")
			assert.Contains(t, idxComp, "idx_ab", "SQLite: Index idx_ab expected")
			assert.Equal(t, []string{"col_a", "col_b"}, idxComp["idx_ab"], "SQLite: Columns for idx_ab")
			assert.Contains(t, idxComp, "idx_cd", "SQLite: Index idx_cd expected")
			assert.Equal(t, []string{"col_c", "col_d"}, idxComp["idx_cd"], "SQLite: Columns for idx_cd")
			// Check for base model index (name might vary in SQLite)
			foundDeletedAtComp := false
			for _, cols := range idxComp {
				if len(cols) == 1 && cols[0] == "deleted_at" {
					foundDeletedAtComp = true
					break
				}
			}
			assert.True(t, foundDeletedAtComp, "SQLite: Simple index on deleted_at expected for composite_index_models")

			// --- Verify TestCompositeUniqueIndexModel --- (Table: composite_unique_index_models)
			uniqComp := dbcase.getUniq(t, db, "composite_unique_index_models")
			assert.Contains(t, uniqComp, "uq_ab", "SQLite: Unique index uq_ab expected")
			assert.Equal(t, []string{"key_a", "key_b"}, uniqComp["uq_ab"], "SQLite: Columns for uq_ab")
			// Check for base model index (should be non-unique)
			idxCompUniqTable := dbcase.getIdx(t, db, "composite_unique_index_models")
			foundDeletedAtUniq := false
			for _, cols := range idxCompUniqTable {
				if len(cols) == 1 && cols[0] == "deleted_at" {
					foundDeletedAtUniq = true
					break
				}
			}
			assert.True(t, foundDeletedAtUniq, "SQLite: Simple index on deleted_at expected for composite_unique_index_models")

			// --- Verify TestMixedIndexesModel --- (Table: mixed_indexes_models)
			idxMixed := dbcase.getIdx(t, db, "mixed_indexes_models")
			uniqMixed := dbcase.getUniq(t, db, "mixed_indexes_models")
			assert.Contains(t, idxMixed, "idx_comp", "SQLite: Index idx_comp expected")
			assert.Equal(t, []string{"f3", "f4"}, idxMixed["idx_comp"], "SQLite: Columns for idx_comp")
			assert.Contains(t, uniqMixed, "uq_comp", "SQLite: Unique index uq_comp expected")
			assert.Equal(t, []string{"f5", "f6"}, uniqMixed["uq_comp"], "SQLite: Columns for uq_comp")
			// Check single and base indexes
			foundDeletedAtMixed, foundF1 := false, false
			for _, cols := range idxMixed {
				if len(cols) == 1 {
					switch cols[0] {
					case "deleted_at":
						foundDeletedAtMixed = true
					case "f1":
						foundF1 = true
					}
				}
			}
			foundF2 := false
			for _, cols := range uniqMixed {
				if len(cols) == 1 && cols[0] == "f2" {
					foundF2 = true
					break
				}
			}
			assert.True(t, foundDeletedAtMixed, "SQLite: Simple index on deleted_at expected for mixed_indexes_models")
			assert.True(t, foundF1, "SQLite: Simple index on f1 expected for mixed_indexes_models")
			assert.True(t, foundF2, "SQLite: Simple unique index on f2 expected for mixed_indexes_models")
		})
	}
}

// Helper for SQLite index column fetching
func getSQLiteIndexColumns(t *testing.T, db thing.DBAdapter, indexName string) []string {
	t.Helper()
	infoQuery := fmt.Sprintf("PRAGMA index_info(%s);", indexName)
	rows, err := db.DB().Query(infoQuery)
	require.NoError(t, err)
	defer rows.Close()
	var colNames []string
	for rows.Next() {
		var seqno, cid int
		var name string
		require.NoError(t, rows.Scan(&seqno, &cid, &name))
		colNames = append(colNames, name)
	}
	sort.Strings(colNames)
	return colNames
}

func TestBaseModel_ToJSONWithOptions(t *testing.T) {
	// Use a model with more fields for testing options
	type TestJSONModel struct {
		thing.BaseModel
		Name   string `json:"name"`
		Email  string `json:"email,omitempty"`
		Status int    `json:"status"`
		Hidden string `json:"-"` // Should always be excluded by tag
	}

	// Setup Thing instance (using test DB and cache)
	db, cache, cleanup := setupTestDB(t)
	defer cleanup()
	thingInstance, err := thing.New[*TestJSONModel](db, cache)
	require.NoError(t, err)

	now := time.Now()
	m := &TestJSONModel{
		BaseModel: thing.BaseModel{
			ID:        123,
			CreatedAt: now,
			UpdatedAt: now,
		},
		Name:   "Test User",
		Email:  "test@example.com",
		Status: 1,
		Hidden: "should not see",
	}

	tests := []struct {
		name       string
		opts       []thing.JSONOption
		expected   map[string]interface{}
		unexpected []string // Fields that should NOT be in the output
	}{
		{
			name: "No Options (Defaults)",
			opts: nil,
			expected: map[string]interface{}{
				"id":         float64(123), // JSON numbers are float64 by default
				"created_at": now.Format(time.RFC3339Nano),
				"updated_at": now.Format(time.RFC3339Nano),
				"deleted":    false,
				"name":       "Test User",
				"email":      "test@example.com",
				"status":     float64(1),
			},
			unexpected: []string{"Hidden", "isNewRecord"},
		},
		{
			name: "Include Specific Fields",
			opts: []thing.JSONOption{thing.Include("name", "status")},
			expected: map[string]interface{}{
				"id":     float64(123), // ID included by default
				"name":   "Test User",
				"status": float64(1),
			},
			unexpected: []string{"created_at", "updated_at", "deleted", "email", "Hidden"},
		},
		{
			name: "Exclude Specific Fields",
			opts: []thing.JSONOption{thing.Exclude("created_at", "updated_at", "email")},
			expected: map[string]interface{}{
				"id":      float64(123),
				"deleted": false,
				"name":    "Test User",
				"status":  float64(1),
			},
			unexpected: []string{"created_at", "updated_at", "email", "Hidden"},
		},
		{
			name: "Exclude ID",
			opts: []thing.JSONOption{thing.Exclude("id")},
			expected: map[string]interface{}{
				"created_at": now.Format(time.RFC3339Nano),
				"updated_at": now.Format(time.RFC3339Nano),
				"deleted":    false,
				"name":       "Test User",
				"email":      "test@example.com",
				"status":     float64(1),
			},
			unexpected: []string{"id", "Hidden"},
		},
		{
			name: "Include Specific Fields (Exclude ID)",
			opts: []thing.JSONOption{thing.Include("name", "status"), thing.Exclude("id")},
			expected: map[string]interface{}{
				"name":   "Test User",
				"status": float64(1),
			},
			unexpected: []string{"id", "created_at", "updated_at", "deleted", "email", "Hidden"},
		},
		{
			name: "Exclude All Base Fields",
			opts: []thing.JSONOption{thing.Exclude("id", "created_at", "updated_at", "deleted")},
			expected: map[string]interface{}{
				"name":   "Test User",
				"email":  "test@example.com",
				"status": float64(1),
			},
			unexpected: []string{"id", "created_at", "updated_at", "deleted", "Hidden"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call ToJSON on the Thing instance, passing the model
			jsonBytes, err := thingInstance.ToJSON(m, tt.opts...)
			if err != nil {
				t.Fatalf("ToJSON failed: %v", err)
			}

			var outputMap map[string]interface{}
			err = json.Unmarshal(jsonBytes, &outputMap)
			if err != nil {
				t.Fatalf("Failed to unmarshal result JSON: %v\nJSON: %s", err, string(jsonBytes))
			}

			// Check expected fields
			for key, expectedValue := range tt.expected {
				actualValue, ok := outputMap[key]
				if !ok {
					t.Errorf("Expected field '%s' missing from JSON output", key)
					continue
				}
				// Special handling for time comparison
				if expectedTimeStr, ok := expectedValue.(string); ok {
					if actualTimeStr, ok := actualValue.(string); ok {
						expectedTime, _ := time.Parse(time.RFC3339Nano, expectedTimeStr)
						actualTime, _ := time.Parse(time.RFC3339Nano, actualTimeStr)
						if !expectedTime.Equal(actualTime) {
							t.Errorf("Field '%s': expected time %v, got %v", key, expectedTime, actualTime)
						}
						continue // Skip next comparison for time
					}
				}

				if actualValue != expectedValue {
					t.Errorf("Field '%s': expected %v (%T), got %v (%T)", key, expectedValue, expectedValue, actualValue, actualValue)
				}
			}

			// Check unexpected fields
			for _, key := range tt.unexpected {
				if _, ok := outputMap[key]; ok {
					t.Errorf("Unexpected field '%s' found in JSON output", key)
				}
			}
		})
	}
}

// --- Schema Diff/ALTER TABLE TDD ---

type DiffUserV1 struct {
	ID   int64  `db:"id,pk"`
	Name string `db:"name"`
}

func (u *DiffUserV1) TableName() string { return "diff_users" }

type DiffUserV2 struct {
	ID    int64  `db:"id,pk"`
	Name  string `db:"name"`
	Email string `db:"email"`
}

func (u *DiffUserV2) TableName() string { return "diff_users" }

func TestAutoMigrate_SchemaDiff(t *testing.T) {
	dbs := []struct {
		name  string
		setup func(tb testing.TB) (thing.DBAdapter, thing.CacheClient, func())
		check func(t *testing.T, db thing.DBAdapter)
	}{
		{
			name: "SQLite",
			setup: func(tb testing.TB) (thing.DBAdapter, thing.CacheClient, func()) {
				db, cache, cleanup := setupTestDB(tb)
				return db, cache, cleanup
			},
			check: func(t *testing.T, db thing.DBAdapter) {
				rows, err := db.DB().Query(`PRAGMA table_info(diff_users)`)
				require.NoError(t, err)
				defer rows.Close()
				var cols []string
				for rows.Next() {
					var cid int
					var name, ctype string
					var notnull, pk int
					var dflt interface{}
					require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
					cols = append(cols, name)
				}
				require.Contains(t, cols, "email", "Should have added email column")
			},
		},
		{
			name:  "MySQL",
			setup: setupMySQLTestDB,
			check: func(t *testing.T, db thing.DBAdapter) {
				rows, err := db.DB().Query(`SHOW COLUMNS FROM diff_users`)
				require.NoError(t, err)
				defer rows.Close()
				var cols []string
				for rows.Next() {
					var field, ctype, null, key, def, extra string
					require.NoError(t, rows.Scan(&field, &ctype, &null, &key, &def, &extra))
					cols = append(cols, field)
				}
				require.Contains(t, cols, "email", "Should have added email column")
			},
		},
		{
			name:  "Postgres",
			setup: setupPostgresTestDB,
			check: func(t *testing.T, db thing.DBAdapter) {
				rows, err := db.DB().Query(`SELECT column_name FROM information_schema.columns WHERE table_name = 'diff_users'`)
				require.NoError(t, err)
				defer rows.Close()
				var cols []string
				for rows.Next() {
					var name string
					require.NoError(t, rows.Scan(&name))
					cols = append(cols, name)
				}
				require.Contains(t, cols, "email", "Should have added email column")
			},
		},
	}

	for _, dbcase := range dbs {
		t.Run(dbcase.name, func(t *testing.T) {
			db, cache, cleanup := dbcase.setup(t)
			defer cleanup()
			require.NoError(t, thing.Configure(db, cache))

			// Step 1: Initial migration (V1)
			err := thing.AutoMigrate(&DiffUserV1{})
			require.NoError(t, err)

			// Step 2: Insert a row
			_, err = db.Exec(context.Background(), "INSERT INTO diff_users (id, name) VALUES (?, ?)", 1, "Alice")
			// MySQL/Postgres 可能需要不同的占位符处理，这里假设统一接口
			if err != nil {
				t.Fatalf("Insert failed: %v", err)
			}

			// Step 3: Change struct (V2: add Email)
			err = thing.AutoMigrate(&DiffUserV2{})
			require.NoError(t, err)

			// Step 4: Check table columns (should have id, name, email)
			dbcase.check(t, db)

			// Step 5: Remove Email from struct, re-migrate (should NOT drop column)
			err = thing.AutoMigrate(&DiffUserV1{})
			require.NoError(t, err)
			// 再次检查 email 列依然存在
			dbcase.check(t, db)
		})
	}
}

// Test dropping composite index via AutoMigrate
func TestAutoMigrate_DropCompositeIndex(t *testing.T) {
	db, cache, cleanup := setupTestDB(t)
	defer cleanup()
	require.NoError(t, thing.Configure(db, cache))

	// Step 1: Migrate with composite index
	err := thing.AutoMigrate(&DropIdxV1{})
	require.NoError(t, err)
	// Check idx_ab exists
	idxs := getSQLiteIndexes(t, db, "drop_idx_models")
	_, hasIdxAb := idxs["idx_ab"]
	assert.True(t, hasIdxAb, "idx_ab should exist after first migration")

	// Step 2: Remove index from model, migrate again
	err = thing.AutoMigrate(&DropIdxV2{})
	require.NoError(t, err)
	idxs2 := getSQLiteIndexes(t, db, "drop_idx_models")
	_, hasIdxAb2 := idxs2["idx_ab"]
	assert.False(t, hasIdxAb2, "idx_ab should be dropped after removing from model and re-migrating")
}

// Helper: get all indexes for a table (name -> columns)
func getSQLiteIndexes(t *testing.T, db thing.DBAdapter, tableName string) map[string][]string {
	t.Helper()
	idxs := make(map[string][]string)
	rows, err := db.DB().Query("PRAGMA index_list(" + tableName + ");")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		require.NoError(t, rows.Scan(&seq, &name, &unique, &origin, &partial))
		if strings.HasPrefix(name, "sqlite_autoindex_") {
			continue
		}
		idxs[name] = getSQLiteIndexColumns(t, db, name)
	}
	return idxs
}
