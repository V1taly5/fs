package migrator

import (
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

type Config struct {
	// DBPath:         *dbPath,
	MigrationsPath string
}

type Migrator struct {
	db     *sql.DB
	config Config
	logger *slog.Logger
}

func NewMigrator(db *sql.DB, config Config, logger *slog.Logger) *Migrator {
	return &Migrator{
		db:     db,
		config: config,
		logger: logger,
	}
}

// MigrationDirection defines the direction of migrations
type MigrationDirection string

const (
	MigrationUp   MigrationDirection = "up"
	MigrationDown MigrationDirection = "down"
)

// createMigrator create a migration instance
func (m *Migrator) createMigrator() (*migrate.Migrate, error) {
	driver, err := sqlite.WithInstance(m.db, &sqlite.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to create migration driver: %w", err)
	}

	migrator, err := migrate.NewWithDatabaseInstance(
		fmt.Sprintf("file://%s", m.config.MigrationsPath),
		"sqlite", driver)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration instance: %w", err)
	}
	return migrator, nil
}

// RunMigrations applies database migrations in the specified direction
func (m *Migrator) RunMigrations(direction MigrationDirection) error {
	m.logger.Info("Running database migrations", "path", m.config.MigrationsPath, "direction", direction)

	migrator, err := m.createMigrator()
	if err != nil {
		return err
	}

	// apply migrations in the specified direction
	var migrationErr error
	switch direction {
	case MigrationUp:
		migrationErr = migrator.Up()
	case MigrationDown:
		migrationErr = migrator.Down()
	default:
		return fmt.Errorf("invalid migration direction: %s", direction)
	}

	// check for errors
	if migrationErr != nil && migrationErr != migrate.ErrNoChange {
		return fmt.Errorf("failed to apply migrations: %w", migrationErr)
	}

	// get current version
	version, dirty, err := migrator.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	if dirty {
		m.logger.Warn("Database schema is in a dirty state", "version", version)
		return fmt.Errorf("database schema is in a dirty state at version %d", version)
	}

	m.logger.Info("Database migrations completed successfully", "version", version, "direction", direction)
	return nil
}

// MigrateUp applying all up migrations
func (m *Migrator) MigrateUp() error {
	return m.RunMigrations(MigrationUp)
}

// MigrateDown rolls back all migrations
func (m *Migrator) MigrateDown() error {
	return m.RunMigrations(MigrationDown)
}

// MigrateDownN rolls back N migrations
func (m *Migrator) MigrateDownN(n int) error {
	m.logger.Info("Rolling back migrations", "path", m.config.MigrationsPath, "steps", n)

	migrator, err := m.createMigrator()
	if err != nil {
		return err
	}

	// roll back N steps
	if err := migrator.Steps(-n); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to rollback migrations: %w", err)
	}

	// get current version
	version, dirty, err := migrator.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	if dirty {
		m.logger.Warn("Database schema is in a dirty state", "version", version)
		return fmt.Errorf("database schema is in a dirty state at version %d", version)
	}

	m.logger.Info("Migration rollback completed successfully", "version", version, "steps", n)
	return nil
}

// MigrateTo migrates to a specific version
func (m *Migrator) MigrateTo(version uint) error {
	m.logger.Info("Migrating to version", "path", m.config.MigrationsPath, "version", version)

	migrator, err := m.createMigrator()
	if err != nil {
		return err
	}

	// migrate to specific version
	if err := migrator.Migrate(version); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to migrate to version %d: %w", version, err)
	}

	// get current version to verify
	currentVersion, dirty, err := migrator.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	if dirty {
		m.logger.Warn("Database schema is in a dirty state", "version", currentVersion)
		return fmt.Errorf("database schema is in a dirty state at version %d", currentVersion)
	}

	m.logger.Info("Migration completed successfully", "version", currentVersion)
	return nil
}

// GetMigrationVersion returns the current migration version
func (m *Migrator) GetMigrationVersion() (uint, bool, error) {
	migrator, err := m.createMigrator()
	if err != nil {
		return 0, false, err
	}

	// get current version
	version, dirty, err := migrator.Version()
	if err == migrate.ErrNilVersion {
		return 0, false, nil // No migrations have been applied yet
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to get migration version: %w", err)
	}

	return version, dirty, nil
}
