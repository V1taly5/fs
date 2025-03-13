package main

import (
	"database/sql"
	"flag"
	"fmt"
	"fs/internal/util/logger/sl"
	"fs/pkg/migrator"
	"log/slog"
	"os"

	_ "github.com/golang-migrate/migrate/v4/database/sqlite"
)

func main() {
	migrationDir := flag.String("path", "migrations", "Path to the migrations directory")
	dbPath := flag.String("db", "database.sqlite", "Path to the SQLite database file")
	direction := flag.String("direction", "up", "Migration direction: up, down, version, or rollback")
	version := flag.Int("version", 0, "Target version for migration")
	steps := flag.Int("steps", 1, "Number of steps to roll back")

	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		logger.Error("Failed to open database", sl.Err(err))
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		logger.Error("Failed to connect to database", sl.Err(err))
	}

	config := migrator.Config{
		MigrationsPath: *migrationDir,
	}

	m := migrator.NewMigrator(db, config, logger)

	switch *direction {
	case "up":
		if err := m.MigrateUp(); err != nil {
			logger.Error("Migration up failed", sl.Err(err))
			os.Exit(1)
		}
	case "down":
		if err := m.MigrateDown(); err != nil {
			logger.Error("Migration down failed", sl.Err(err))
			os.Exit(1)
		}
	case "version":
		version, dirty, err := m.GetMigrationVersion()
		if err != nil {
			logger.Error("Failed to get migration version", sl.Err(err))
			os.Exit(1)
		}
		fmt.Printf("Current migration version: %d (dirty: %v)\n", version, dirty)
	case "rollback":
		if err := m.MigrateDownN(*steps); err != nil {
			logger.Error("Rollback failed", sl.Err(err))
			os.Exit(1)
		}
	case "to":
		if *version == 0 {
			logger.Error("Please specify a target version with -version flag", sl.Err(err))
			os.Exit(1)
		}
		if err := m.MigrateTo(uint(*version)); err != nil {
			logger.Error("Migration to version failed", slog.Int("target_version", *version), sl.Err(err))
			os.Exit(1)
		}
	default:
		logger.Error("Unknown migration direction", slog.String("direction", *direction))
		os.Exit(1)
	}

	logger.Info("Migration completed successfully")
}
