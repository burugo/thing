package migration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"thing/internal/schema"
	"time"

	"thing"
)

const migrationsTableName = "schema_migrations"

// Migrator handles database migrations
type Migrator struct {
	Db         thing.DBAdapter
	dir        string
	dialect    string
	logEnabled bool
}

// NewMigrator creates a new Migrator instance
func NewMigrator(db thing.DBAdapter, migrationsDir string) *Migrator {
	return &Migrator{
		Db:         db,
		dir:        migrationsDir,
		dialect:    db.DialectName(),
		logEnabled: true, // Default to enabled
	}
}

// EnableLog enables/disables logging
func (m *Migrator) EnableLog(enable bool) {
	m.logEnabled = enable
}

func (m *Migrator) logf(format string, args ...interface{}) {
	if m.logEnabled {
		fmt.Printf("[Migrator] "+format+"\n", args...)
	}
}

// EnsureMigrationsTable creates the schema_migrations table if it doesn't exist
func (m *Migrator) ensureMigrationsTable(ctx context.Context) error {
	m.logf("Ensuring %s table exists...", migrationsTableName)
	sqlStmt, err := schema.GenerateMigrationsTableSQL(m.dialect)
	if err != nil {
		return fmt.Errorf("failed to generate migrations table SQL: %w", err)
	}

	_, err = m.Db.Exec(ctx, sqlStmt)
	if err != nil {
		// Ignore "already exists" errors gracefully for idempotency
		if strings.Contains(strings.ToLower(err.Error()), "already exists") ||
			strings.Contains(strings.ToLower(err.Error()), "duplicate table name") {
			m.logf("Table %s already exists.", migrationsTableName)
			return nil
		}
		return fmt.Errorf("failed to execute migrations table creation: %w\nSQL: %s", err, sqlStmt)
	}
	m.logf("Table %s ensured.", migrationsTableName)
	return nil
}

// AppliedVersion represents a record in the schema_migrations table
type AppliedVersion struct {
	ID          int64
	Version     string
	AppliedAt   string // Store as string for compatibility
	Description string
}

// getAppliedVersions retrieves the list of applied migration versions from the database
func (m *Migrator) getAppliedVersions(ctx context.Context) (map[int64]bool, error) {
	m.logf("Fetching applied migration versions...")
	query := fmt.Sprintf("SELECT version FROM %s", migrationsTableName)

	var versions []string
	err := m.Db.Select(ctx, &versions, query)
	if err != nil {
		// If the table doesn't exist yet, treat as no versions applied
		if strings.Contains(strings.ToLower(err.Error()), "no such table") ||
			strings.Contains(strings.ToLower(err.Error()), "relation \"schema_migrations\" does not exist") || // Postgres
			strings.Contains(strings.ToLower(err.Error()), "table 'schema_migrations' doesn't exist") { // MySQL
			m.logf("Table %s not found, assuming no migrations applied yet.", migrationsTableName)
			return make(map[int64]bool), nil
		}
		return nil, fmt.Errorf("failed to query applied versions: %w", err)
	}

	applied := make(map[int64]bool, len(versions))
	for _, vStr := range versions {
		vInt, err := strconv.ParseInt(vStr, 10, 64)
		if err != nil {
			// Should not happen if versions are stored correctly
			return nil, fmt.Errorf("invalid version format '%s' found in %s table: %w", vStr, migrationsTableName, err)
		}
		applied[vInt] = true
	}
	m.logf("Found %d applied migrations.", len(applied))
	return applied, nil
}

// recordAppliedVersion inserts a record for a successfully applied migration
func (m *Migrator) recordAppliedVersion(ctx context.Context, execer interface {
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}, version int64, description string) error {
	m.logf("Recording applied version %d (%s)...", version, description)
	query := fmt.Sprintf("INSERT INTO %s (version, description, applied_at) VALUES (?, ?, ?)", migrationsTableName)
	versionStr := fmt.Sprintf("%d", version)
	// Format time as ISO8601 compatible string for SQLite
	nowStr := time.Now().Format(time.RFC3339)

	_, err := execer.Exec(ctx, query, versionStr, description, nowStr) // Pass time as string
	if err != nil {
		return fmt.Errorf("failed to record applied version %d: %w", version, err)
	}
	return nil
}

// Migrate applies pending migrations
func (m *Migrator) Migrate(ctx context.Context) error {
	m.logf("Starting migration process...")

	// 1. Ensure migrations table exists
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return err
	}

	// 2. Discover migration files
	allFiles, err := DiscoverMigrations(m.dir)
	if err != nil {
		return fmt.Errorf("failed to discover migrations in %s: %w", m.dir, err)
	}
	m.logf("Discovered %d migration files in %s.", len(allFiles), m.dir)

	// 3. Get applied versions
	appliedVersions, err := m.getAppliedVersions(ctx)
	if err != nil {
		return err
	}

	// 4. Filter pending 'up' migrations
	var pendingMigrations []MigrationFile
	upMigrations := make(map[int64]MigrationFile)
	for _, mf := range allFiles {
		if mf.Direction == "up" {
			upMigrations[mf.Version] = mf
		}
	}

	// Sort versions to apply
	var versionsToApply []int64
	for version := range upMigrations {
		if !appliedVersions[version] {
			versionsToApply = append(versionsToApply, version)
		}
	}
	sort.Slice(versionsToApply, func(i, j int) bool { return versionsToApply[i] < versionsToApply[j] })

	for _, version := range versionsToApply {
		pendingMigrations = append(pendingMigrations, upMigrations[version])
	}

	if len(pendingMigrations) == 0 {
		m.logf("No pending migrations to apply.")
		return nil
	}

	m.logf("Found %d pending migrations to apply.", len(pendingMigrations))

	// 5. Apply pending migrations (within a transaction if possible)
	m.logf("Applying migrations...")
	tx, err := m.Db.BeginTx(ctx, nil) // Start transaction
	if err != nil {
		// If transaction start fails, attempt without transaction
		m.logf("Warning: Could not start transaction (%v), applying migrations without transaction.", err)
		tx = nil
	}

	var migrateErr error
	for _, mig := range pendingMigrations {
		m.logf("Applying migration %d: %s (%s)...", mig.Version, mig.Name, filepath.Base(mig.FilePath))
		sqlBytes, err := os.ReadFile(mig.FilePath)
		if err != nil {
			migrateErr = fmt.Errorf("failed to read migration file %s: %w", mig.FilePath, err)
			break
		}
		sqlScript := string(sqlBytes)

		var currentExecer interface {
			Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
		} = m.Db
		if tx != nil {
			currentExecer = tx
		}

		// Execute the script
		_, err = currentExecer.Exec(ctx, sqlScript)
		if err != nil {
			migrateErr = fmt.Errorf("failed to execute migration %d (%s): %w\nSQL:\n%s", mig.Version, mig.Name, err, sqlScript)
			break
		}

		// Record the version using the same execer (DB or Tx)
		if err := m.recordAppliedVersion(ctx, currentExecer, mig.Version, mig.Name); err != nil {
			migrateErr = fmt.Errorf("failed to record applied version %d: %w", mig.Version, err)
			break
		}
		m.logf("Successfully applied migration %d: %s", mig.Version, mig.Name)
	}

	// 6. Commit or Rollback transaction
	if tx != nil {
		if migrateErr != nil {
			m.logf("Migration failed, rolling back transaction...")
			if rbErr := tx.Rollback(); rbErr != nil {
				return fmt.Errorf("migration failed: %w; additionally, rollback failed: %v", migrateErr, rbErr)
			}
			return migrateErr // Return the original migration error
		} else {
			m.logf("Committing transaction...")
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("failed to commit migration transaction: %w", err)
			}
		}
	} else if migrateErr != nil {
		// If no transaction and error occurred, just return the error
		return migrateErr
	}

	m.logf("Migration process completed successfully.")
	return nil
}
