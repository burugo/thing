package thing_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/burugo/thing"
	"github.com/burugo/thing/drivers/db/sqlite"
	"github.com/burugo/thing/internal/schema"
	"github.com/stretchr/testify/require"
)

type PartialIndexUser struct {
	thing.BaseModel
	GitHubID string `db:"github_id,default:''"`
	GoogleID string `db:"google_id,default:''"`
}

func (PartialIndexUser) TableName() string {
	return "partial_index_users"
}

func (PartialIndexUser) Indexes() []thing.Index {
	return []thing.Index{
		{
			Name:    "uniq_partial_index_users_google_id_present",
			Columns: []string{"google_id"},
			Unique:  true,
			Where:   "google_id <> ''",
		},
	}
}

type PartialIndexAPIKey struct {
	thing.BaseModel
	Provider        string `db:"provider"`
	ProviderSubject string `db:"provider_subject,default:''"`
}

func (PartialIndexAPIKey) TableName() string {
	return "partial_index_api_keys"
}

func (PartialIndexAPIKey) Indexes() []thing.Index {
	return []thing.Index{
		{
			Name:    "uniq_partial_index_api_keys_provider_subject_present",
			Columns: []string{"provider", "provider_subject"},
			Unique:  true,
			Where:   "provider_subject <> ''",
		},
	}
}

type ConflictingPartialIndexUser struct {
	thing.BaseModel
	GoogleID string `db:"google_id,unique:uniq_conflicting_partial_index_users_google_id"`
}

func (ConflictingPartialIndexUser) TableName() string {
	return "conflicting_partial_index_users"
}

func (ConflictingPartialIndexUser) Indexes() []thing.Index {
	return []thing.Index{
		{
			Name:    "uniq_conflicting_partial_index_users_google_id",
			Columns: []string{"google_id"},
			Unique:  true,
			Where:   "google_id <> ''",
		},
	}
}

func TestAutoMigrateSQLitePartialUniqueIndex(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "partial_indexes_test_")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "partial_indexes.db")
	dbAdapter, err := sqlite.NewSQLiteAdapter("file:" + dbPath + "?cache=shared")
	require.NoError(t, err)
	defer dbAdapter.Close()

	require.NoError(t, thing.Configure(dbAdapter, nil, 0))
	require.NoError(t, thing.AutoMigrate(PartialIndexUser{}))

	ctx := context.Background()
	_, err = dbAdapter.Exec(ctx, "INSERT INTO partial_index_users (github_id, google_id) VALUES (?, ?)", "gh1", "")
	require.NoError(t, err)
	_, err = dbAdapter.Exec(ctx, "INSERT INTO partial_index_users (github_id, google_id) VALUES (?, ?)", "gh2", "")
	require.NoError(t, err)
	_, err = dbAdapter.Exec(ctx, "INSERT INTO partial_index_users (github_id, google_id) VALUES (?, ?)", "gh3", "g1")
	require.NoError(t, err)
	_, err = dbAdapter.Exec(ctx, "INSERT INTO partial_index_users (github_id, google_id) VALUES (?, ?)", "gh4", "g1")
	require.Error(t, err)
}

func TestAutoMigratePostgresPartialUniqueIndex(t *testing.T) {
	dbAdapter, _, cleanup := setupPostgresTestDB(t)
	defer cleanup()

	ctx := context.Background()
	_, _ = dbAdapter.Exec(ctx, "DROP TABLE IF EXISTS partial_index_users;")
	defer func() { _, _ = dbAdapter.Exec(ctx, "DROP TABLE IF EXISTS partial_index_users;") }()

	require.NoError(t, thing.Configure(dbAdapter, nil, 0))
	require.NoError(t, thing.AutoMigrate(PartialIndexUser{}))

	_, err := dbAdapter.Exec(ctx, "INSERT INTO partial_index_users (github_id, google_id) VALUES (?, ?)", "gh1", "")
	require.NoError(t, err)
	_, err = dbAdapter.Exec(ctx, "INSERT INTO partial_index_users (github_id, google_id) VALUES (?, ?)", "gh2", "")
	require.NoError(t, err)
	_, err = dbAdapter.Exec(ctx, "INSERT INTO partial_index_users (github_id, google_id) VALUES (?, ?)", "gh3", "g1")
	require.NoError(t, err)
	_, err = dbAdapter.Exec(ctx, "INSERT INTO partial_index_users (github_id, google_id) VALUES (?, ?)", "gh4", "g1")
	require.Error(t, err)
}

func TestGenerateMigrationSQLPostgresPartialUniqueIndex(t *testing.T) {
	sqls, err := schema.AutoMigrateWithDialect("postgres", PartialIndexUser{})
	require.NoError(t, err)
	require.Len(t, sqls, 1)

	sql := sqls[0]
	require.Contains(t, sql, `CREATE UNIQUE INDEX IF NOT EXISTS "uniq_partial_index_users_google_id_present"`)
	require.Contains(t, sql, `ON "partial_index_users" ("google_id")`)
	require.Contains(t, sql, `WHERE google_id <> ''`)
}

func TestGenerateMigrationSQLMySQLPartialIndexFailsExplicitly(t *testing.T) {
	_, err := schema.AutoMigrateWithDialect("mysql", PartialIndexUser{})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "partial indexes") && strings.Contains(err.Error(), "MySQL"), err.Error())
}

func TestGenerateMigrationSQLCompositeProgrammaticPartialIndex(t *testing.T) {
	sqls, err := schema.AutoMigrateWithDialect("sqlite", PartialIndexAPIKey{})
	require.NoError(t, err)
	require.Len(t, sqls, 1)

	sql := sqls[0]
	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS `uniq_partial_index_api_keys_provider_subject_present`")
	require.Contains(t, sql, "(`provider`, `provider_subject`)")
	require.Contains(t, sql, "WHERE provider_subject <> ''")
}

func TestGenerateMigrationSQLDuplicateIndexNameConflictFails(t *testing.T) {
	_, err := schema.AutoMigrateWithDialect("sqlite", ConflictingPartialIndexUser{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicts with an existing index definition")
}
