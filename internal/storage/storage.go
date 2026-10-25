// Package storage opens anvil's local configuration database.
package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Watts-Argentina/anvil/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DefaultPath returns <UserConfigDir>/anvil/anvil.db.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, "anvil", "anvil.db"), nil
}

// Open opens (or creates) the local database at path and applies the migrations.
// path can be an in-memory DSN such as "file::memory:?cache=shared".
func Open(path string) (*gorm.DB, error) {
	if filepath.IsAbs(path) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create config dir: %w", err)
		}
	}

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open local db: %w", err)
	}

	if err := db.AutoMigrate(&models.DBConn{}, &models.QueryHistory{}, &models.Settings{}); err != nil {
		return nil, fmt.Errorf("migrate local db: %w", err)
	}
	return db, nil
}
