// Package schema builds and runs the DDL to create tables and databases.
// Identifiers are quoted with the driver; column types and defaults are
// SQL written by the user and shown in a preview before running.
package schema

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

// Column is a column of a new table. Default is an SQL expression ('abc',
// 0, CURRENT_TIMESTAMP); empty means no default.
type Column struct {
	Name     string
	Type     string
	Default  string
	Nullable bool
	PK       bool
	AutoInc  bool
}

// Table is a new table, created in the pool's database (and, in PG, in the
// first schema of the search_path).
type Table struct {
	Name    string
	Columns []Column
}

// typeRe accepts type names with optional arguments and array brackets:
// "integer", "varchar(255)", "numeric(10, 2)", "timestamp with time zone",
// "int unsigned", "text[]".
var typeRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_ ]*(\(\s*[0-9]+\s*(,\s*[0-9]+\s*)?\))?[A-Za-z0-9_ ]*(\[\])?$`)

// CreateTable returns the CREATE TABLE statement for t.
func CreateTable(d drivers.Driver, t Table) (string, error) {
	if err := validate(t); err != nil {
		return "", appErrors.Error("Invalid table.", err.Error(), err)
	}

	var pk []string
	inlinePK := false
	lines := make([]string, 0, len(t.Columns)+1)
	for _, c := range t.Columns {
		name := d.QuoteIdent(strings.TrimSpace(c.Name))
		typ := strings.TrimSpace(c.Type)
		if c.AutoInc {
			def, inline := d.AutoIncrement(typ)
			inlinePK = inline
			lines = append(lines, name+" "+def)
			pk = append(pk, name)
			continue
		}
		def := name + " " + typ
		if !c.Nullable || c.PK {
			def += " NOT NULL"
		}
		if v := strings.TrimSpace(c.Default); v != "" {
			def += " DEFAULT " + v
		}
		lines = append(lines, def)
		if c.PK {
			pk = append(pk, name)
		}
	}
	if len(pk) > 0 && !inlinePK {
		lines = append(lines, "PRIMARY KEY ("+strings.Join(pk, ", ")+")")
	}

	stmt := "CREATE TABLE " + d.QuoteIdent(strings.TrimSpace(t.Name)) + " (\n  " + strings.Join(lines, ",\n  ") + "\n)"
	// A default can be any expression: make sure it does not end the
	// statement.
	if len(d.SplitStatements(stmt)) != 1 {
		err := errors.New("a default value contains a statement separator")
		return "", appErrors.Error("Invalid table.", "A default value must be a single SQL expression without ';'.", err)
	}
	return stmt, nil
}

func validate(t Table) error {
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("The table needs a name.")
	}
	if len(t.Columns) == 0 {
		return errors.New("The table needs at least one column.")
	}

	seen := map[string]bool{}
	pks, autoInc := 0, 0
	for i, c := range t.Columns {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			return fmt.Errorf("Column %d needs a name.", i+1)
		}
		if seen[strings.ToLower(name)] {
			return fmt.Errorf("The column %q is repeated.", name)
		}
		seen[strings.ToLower(name)] = true
		if !typeRe.MatchString(strings.TrimSpace(c.Type)) {
			if strings.TrimSpace(c.Type) == "" {
				return fmt.Errorf("The column %q needs a type.", name)
			}
			return fmt.Errorf("The type of %q is not valid: use a type name such as integer, text or varchar(255).", name)
		}
		if c.PK {
			pks++
		}
		if c.AutoInc {
			autoInc++
			if !c.PK {
				return fmt.Errorf("The auto-increment column %q must be the primary key.", name)
			}
			if strings.TrimSpace(c.Default) != "" {
				return fmt.Errorf("The auto-increment column %q cannot have a default.", name)
			}
		}
	}
	if autoInc > 0 && pks > 1 {
		return errors.New("An auto-increment column must be the only primary key column.")
	}
	return nil
}
