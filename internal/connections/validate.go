package connections

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Validate checks the required fields for the engine.
func Validate(conn models.DBConn) error {
	invalid := func(desc string) error {
		return appErrors.Error("Invalid connection.", desc, fmt.Errorf("invalid connection: %s", desc))
	}

	if strings.TrimSpace(conn.Name) == "" {
		return invalid("The connection name is required.")
	}
	if conn.Color != "" && !hexColor.MatchString(conn.Color) {
		return invalid("The color must have the format #RRGGBB.")
	}

	switch conn.Engine {
	case models.EngineSQLite:
		if strings.TrimSpace(conn.DB) == "" {
			return invalid("Select the SQLite database file.")
		}
		return nil
	case models.EnginePostgres, models.EngineMySQL:
	default:
		return invalid(fmt.Sprintf("Unknown database engine %q.", conn.Engine))
	}

	if strings.TrimSpace(conn.Host) == "" {
		return invalid("The host is required.")
	}
	if conn.Port < 1 || conn.Port > 65535 {
		return invalid("The port must be between 1 and 65535.")
	}
	if strings.TrimSpace(conn.User) == "" {
		return invalid("The user is required.")
	}
	return nil
}
