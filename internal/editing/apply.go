package editing

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

// Apply runs the statements in one transaction: all or nothing. Every
// UPDATE/DELETE must affect exactly one row (SPEC §7.3); otherwise
// everything is rolled back and the offending change is reported.
func Apply(ctx context.Context, db *sql.DB, stmts []Statement) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return appErrors.Error("Changes not saved.", "The transaction could not be started: "+err.Error(), err)
	}
	defer tx.Rollback()

	for _, st := range stmts {
		res, err := tx.ExecContext(ctx, st.SQL, st.Args...)
		if err != nil {
			return appErrors.Error("Changes not saved.",
				fmt.Sprintf("The %s failed: %v. Nothing was saved.", st.Label, err), err)
		}
		if st.Change.Kind == Insert {
			continue
		}
		n, err := res.RowsAffected()
		if err != nil {
			return appErrors.Error("Changes not saved.",
				fmt.Sprintf("The rows affected by the %s could not be checked. Nothing was saved.", st.Label), err)
		}
		if n != 1 {
			return appErrors.Error("Changes not saved.", affectedText(st, n),
				fmt.Errorf("%s affected %d rows", st.Label, n))
		}
	}
	if err := tx.Commit(); err != nil {
		return appErrors.Error("Changes not saved.", "The transaction could not be committed: "+err.Error(), err)
	}
	return nil
}

func affectedText(st Statement, n int64) string {
	if n == 0 {
		return fmt.Sprintf("The %s found no row: it no longer exists or was changed by someone else. Nothing was saved; reload the page.", st.Label)
	}
	return fmt.Sprintf("The %s would affect %d identical rows. Nothing was saved.", st.Label, n)
}
