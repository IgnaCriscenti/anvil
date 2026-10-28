package query

import (
	"fmt"
	"strings"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

// Statement is a statement to execute, with its analysis.
type Statement struct {
	drivers.Span
	Info drivers.Statement
}

// Plan is the statements to execute and the warnings that must be confirmed
// first (SPEC §5.2, §8).
type Plan struct {
	Statements []Statement
	Warnings   []string
	driver     drivers.Driver // to paginate the results
}

// NewPlan splits sql into statements and runs the pre-checks. On a
// read-only connection it rejects everything if any statement is not a read.
func NewPlan(d drivers.Driver, sql string, readOnly bool) (Plan, error) {
	p := Plan{driver: d}
	for _, span := range d.SplitStatements(sql) {
		p.Statements = append(p.Statements, Statement{Span: span, Info: d.Analyze(span.Text)})
	}
	if len(p.Statements) == 0 {
		return Plan{}, appErrors.Error("Nothing to run.", "Write a SQL statement first.", fmt.Errorf("empty sql"))
	}

	if readOnly {
		for _, s := range p.Statements {
			if !s.Info.IsReadOnly() {
				return Plan{}, appErrors.Error(
					"Read-only connection.",
					fmt.Sprintf("This connection is read-only and %q is not a read statement: %s", s.Info.Keyword, snippet(s.Text)),
					fmt.Errorf("write statement on read-only connection"),
				)
			}
		}
	}

	inTx, outside := false, 0
	for _, s := range p.Statements {
		switch {
		case s.Info.Kind == drivers.KindTx:
			inTx = txOpen(s, inTx)
		case s.Info.Kind == drivers.KindWrite && !inTx:
			outside++
		}
		if (s.Info.Keyword == "UPDATE" || s.Info.Keyword == "DELETE") && !s.Info.HasWhere {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s without WHERE changes every row: %s", s.Info.Keyword, snippet(s.Text)))
		}
	}
	if outside > 0 {
		p.Warnings = append(p.Warnings, fmt.Sprintf("%d statement(s) modify data without a transaction.", outside))
	}
	return p, nil
}

// txOpen reports whether a transaction is left open after s.
func txOpen(s Statement, inTx bool) bool {
	switch s.Info.Keyword {
	case "BEGIN", "START":
		return true
	case "COMMIT", "END", "ABORT":
		return false
	case "ROLLBACK":
		// ROLLBACK [WORK] TO SAVEPOINT x does not end the transaction.
		for _, w := range strings.Fields(strings.ToUpper(s.Text))[1:] {
			if w == "TO" {
				return inTx
			}
		}
		return false
	}
	return inTx
}

// snippet shortens a statement to show it in a message.
func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		s = s[:77] + "…"
	}
	return s
}
