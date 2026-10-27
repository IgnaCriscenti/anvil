package schema

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

// CreateDatabase returns the CREATE DATABASE statement for name.
func CreateDatabase(d drivers.Driver, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		err := errors.New("empty database name")
		return "", appErrors.Error("Invalid database.", "The database needs a name.", err)
	}
	stmt := d.CreateDatabase(name)
	if stmt == "" {
		err := errors.New("engine without server databases")
		return "", appErrors.Error("Not supported.", "This engine has no server databases: create a new database file instead.", err)
	}
	return stmt, nil
}

// Exec runs a statement built by CreateTable or CreateDatabase. what names
// the object for the error ("table", "database").
func Exec(ctx context.Context, db *sql.DB, stmt, what string) error {
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return appErrors.Error("The "+what+" was not created.", "The server rejected the statement: "+err.Error(), err)
	}
	return nil
}
