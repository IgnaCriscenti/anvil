package query

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

// StmtResult is the summary of an executed statement.
type StmtResult struct {
	SQL      string
	HasRows  bool
	Rows     int // rows read in the first page
	Affected int64
	Duration time.Duration
	Err      error
}

// Summary is the text for the status bar.
func (r StmtResult) Summary() string {
	d := r.Duration.Round(time.Millisecond)
	switch {
	case r.Err != nil:
		return "error"
	case r.HasRows:
		return fmt.Sprintf("%d rows · %s", r.Rows, d)
	default:
		return fmt.Sprintf("%d affected · %s", r.Affected, d)
	}
}

// RowsPage is a page of the result of an editor statement. Editor
// results are always read-only (SPEC §7.1).
type RowsPage struct {
	Result
	SQL     string
	Index   int
	Size    int
	HasNext bool
	// Repeatable reports whether it can be run again to paginate
	// (read statements). An INSERT … RETURNING is not.
	Repeatable bool
	wrap       bool
	driver     drivers.Driver
}

// RunResult is the result of running a Plan.
type RunResult struct {
	Statements []StmtResult
	Failed     int       // index of the statement that failed, -1 if none
	Rows       *RowsPage // last result with rows
	// RolledBack reports that a transaction was left open and was rolled back.
	RolledBack bool
	Duration   time.Duration
}

// Affected adds up the affected rows of every statement.
func (r RunResult) Affected() int64 {
	var n int64
	for _, s := range r.Statements {
		n += s.Affected
	}
	return n
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Run executes the plan's statements in order, in a single session. It
// stops at the first one that fails; the error describes which one and what
// had already run. A transaction left open is rolled back.
func Run(ctx context.Context, db *sql.DB, p Plan, size int) (RunResult, error) {
	res := RunResult{Failed: -1}
	start := time.Now()
	defer func() { res.Duration = time.Since(start) }()

	conn, err := db.Conn(ctx)
	if err != nil {
		return res, engineError("Failed to run the query.", err)
	}
	defer conn.Close()

	inTx := false
	for i, s := range p.Statements {
		sr := StmtResult{SQL: s.Text}
		t0 := time.Now()

		if s.Info.ReturnsRows() || returning(s) {
			page := RowsPage{SQL: s.Text, Size: size, Repeatable: s.Info.ReturnsRows(), wrap: wrappable(s), driver: p.driver}
			page, err = fetch(ctx, conn, page, 0)
			if err == nil {
				sr.HasRows, sr.Rows = true, len(page.Rows)
				res.Rows = &page
			}
		} else {
			var r sql.Result
			r, err = conn.ExecContext(ctx, s.Text)
			if err == nil {
				sr.Affected, _ = r.RowsAffected()
			}
		}
		sr.Duration = time.Since(t0)

		if err != nil {
			sr.Err = err
			res.Statements = append(res.Statements, sr)
			res.Failed = i
			if inTx || s.Info.Kind == drivers.KindTx {
				res.RolledBack = rollback(conn)
			}
			return res, statementError(p, i, err)
		}
		res.Statements = append(res.Statements, sr)
		if s.Info.Kind == drivers.KindTx {
			inTx = txOpen(s, inTx)
		}
	}

	if inTx {
		res.RolledBack = rollback(conn)
	}
	return res, nil
}

func rollback(conn *sql.Conn) bool {
	_, err := conn.ExecContext(context.Background(), "ROLLBACK")
	return err == nil
}

// statementError describes the failed statement i and how many ran.
func statementError(p Plan, i int, err error) error {
	if isCanceled(err) {
		return appErrors.Error("Query cancelled.", fmt.Sprintf("The query was cancelled while running statement %d.", i+1), err)
	}
	desc := err.Error()
	if len(p.Statements) > 1 {
		desc = fmt.Sprintf("Statement %d of %d failed (%s): %s", i+1, len(p.Statements), snippet(p.Statements[i].Text), err)
		if i > 0 {
			desc += fmt.Sprintf("\nThe previous %d statement(s) were executed.", i)
		}
	}
	return appErrors.Error("Query failed.", desc, err)
}

// wrappable reports whether the statement can be paginated by wrapping it in
// SELECT * FROM (…) (SPEC §6.2).
func wrappable(s Statement) bool {
	return s.Info.Keyword == "SELECT" || s.Info.Keyword == "WITH" && s.Info.Kind == drivers.KindRead
}

// returning detects a DML statement that returns rows (… RETURNING).
func returning(s Statement) bool {
	return s.Info.Kind == drivers.KindWrite && strings.Contains(strings.ToUpper(s.Text), "RETURNING")
}

// fetch reads page index of the statement. If wrapping it fails (for
// example duplicate columns, or an ORDER BY in a SQL Server subquery), it
// paginates on the client.
func fetch(ctx context.Context, q querier, page RowsPage, index int) (RowsPage, error) {
	page.Index = index
	offset := index * page.Size

	if page.wrap {
		// No AS before the alias: Oracle does not accept it.
		wrapped := "SELECT * FROM (" + page.SQL + ") anvil_q" +
			page.driver.Limit("", strconv.Itoa(page.Size+1), strconv.Itoa(offset))
		rows, err := q.QueryContext(ctx, wrapped)
		if err == nil {
			res, more, err := scan(rows, 0, page.Size)
			if err == nil {
				page.Result, page.HasNext = res, more
				return page, nil
			}
		}
		if ctx.Err() != nil {
			return page, ctx.Err()
		}
		page.wrap = false
	}

	rows, err := q.QueryContext(ctx, page.SQL)
	if err != nil {
		return page, err
	}
	res, more, err := scan(rows, offset, page.Size)
	if err != nil {
		return page, err
	}
	page.Result, page.HasNext = res, more
	return page, nil
}

// LoadRowsPage runs the result's statement again to read another page.
// Only for Repeatable pages.
func LoadRowsPage(ctx context.Context, db *sql.DB, page RowsPage, index int) (RowsPage, error) {
	if !page.Repeatable {
		return page, appErrors.Error("Cannot paginate.", "This result comes from a statement that modifies data; run it again to see other rows.", fmt.Errorf("non repeatable result"))
	}
	p, err := fetch(ctx, db, page, index)
	if err != nil {
		return page, engineError("Failed to load rows.", err)
	}
	return p, nil
}
