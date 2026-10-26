package editing

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/internal/query"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

// Target is the table being edited and the page the changes refer to.
type Target struct {
	Driver  drivers.Driver
	Table   drivers.Table
	Columns []drivers.Column
	Page    query.Result // original rows: Change.Row indexes Page.Rows
}

// Statement is the DML of a change. It always runs parameterized.
type Statement struct {
	SQL    string
	Args   []any
	Change Change
	Label  string // "UPDATE of row 3", to report errors
}

// Build generates one statement per change, in order (SPEC §7.3–7.4).
func Build(t Target, changes []Change) ([]Statement, error) {
	stmts := make([]Statement, 0, len(changes))
	for _, c := range changes {
		st, err := t.build(c)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, st)
	}
	return stmts, nil
}

func (t Target) build(c Change) (Statement, error) {
	d := t.Driver
	table := drivers.QualifiedName(d, t.Table)
	var args []any
	ph := func(v any) string {
		args = append(args, v)
		return d.Placeholder(len(args))
	}

	switch c.Kind {
	case Insert:
		label := "INSERT of new row " + strconv.Itoa(c.Row)
		var cols, vals []string
		for _, col := range t.Columns {
			v, ok := c.Values[col.Name]
			if !ok || col.AutoInc && v == (Value{}) {
				continue
			}
			cols = append(cols, d.QuoteIdent(col.Name))
			vals = append(vals, ph(t.arg(col, v)))
		}
		sql := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(vals, ", ") + ")"
		if len(cols) == 0 {
			sql = "INSERT INTO " + table + " DEFAULT VALUES"
			if _, mysql := d.(drivers.MySQL); mysql {
				sql = "INSERT INTO " + table + " () VALUES ()"
			}
		}
		return Statement{SQL: sql, Args: args, Change: c, Label: label}, nil

	case Update:
		label := "UPDATE of row " + strconv.Itoa(c.Row+1)
		var sets []string
		for _, col := range t.Columns {
			if v, ok := c.Values[col.Name]; ok {
				sets = append(sets, d.QuoteIdent(col.Name)+" = "+ph(t.arg(col, v)))
			}
		}
		where, err := t.where(c.Row, ph)
		if err != nil {
			return Statement{}, err
		}
		sql := "UPDATE " + table + " SET " + strings.Join(sets, ", ") + " WHERE " + where
		return Statement{SQL: sql, Args: args, Change: c, Label: label}, nil

	default:
		label := "DELETE of row " + strconv.Itoa(c.Row+1)
		where, err := t.where(c.Row, ph)
		if err != nil {
			return Statement{}, err
		}
		return Statement{SQL: "DELETE FROM " + table + " WHERE " + where, Args: args, Change: c, Label: label}, nil
	}
}

// where identifies the original row: by its PK or, without one, by every
// comparable column.
func (t Target) where(row int, ph func(any) string) (string, error) {
	if row < 0 || row >= len(t.Page.Rows) {
		return "", fmt.Errorf("row %d is not in the page", row)
	}
	orig := t.Page.Rows[row]

	keys := slices.Clone(t.Columns)
	keys = slices.DeleteFunc(keys, func(c drivers.Column) bool { return !c.PK })
	slices.SortFunc(keys, func(a, b drivers.Column) int { return a.PKOrdinal - b.PKOrdinal })
	if len(keys) == 0 {
		keys = slices.DeleteFunc(slices.Clone(t.Columns), func(c drivers.Column) bool { return !Comparable(c) })
	}

	var conds []string
	for _, col := range keys {
		i := slices.IndexFunc(t.Page.Columns, func(c query.Column) bool { return c.Name == col.Name })
		if i < 0 || orig[i].Blob {
			continue
		}
		name := t.Driver.QuoteIdent(col.Name)
		if orig[i].Null {
			conds = append(conds, name+" IS NULL")
		} else {
			conds = append(conds, name+" = "+ph(orig[i].Raw))
		}
	}
	if len(conds) == 0 {
		return "", appErrors.Error(
			"Rows cannot be identified.",
			"The table has no primary key and no column that can be compared, so its rows cannot be updated or deleted.",
			fmt.Errorf("no identifying columns in %s", t.Table.Name),
		)
	}
	return strings.Join(conds, " AND "), nil
}

// Comparable reports whether a column can identify a row without a PK:
// binary, JSON, XML, geometric and floating point columns are left out.
func Comparable(c drivers.Column) bool {
	t := strings.ToLower(c.DBType)
	if query.IsBinaryType(strings.ToUpper(t)) {
		return false
	}
	for _, s := range []string{"json", "xml", "point", "line", "lseg", "box", "path", "polygon", "circle", "geometry", "float", "real", "double"} {
		if strings.Contains(t, s) {
			return false
		}
	}
	return true
}

// arg is the parameter for a value: nil for NULL, the text otherwise.
// MySQL does not accept the offset that the grid shows in dates
// ("… +00:00"), so its dates are sent in UTC without it, the same way the
// connection reads them.
func (t Target) arg(col drivers.Column, v Value) any {
	if v.Null {
		return nil
	}
	if _, mysql := t.Driver.(drivers.MySQL); mysql {
		if tm, ok := ParseTemporal(col.DBType, v.Text); ok {
			return tm.UTC().Format(mysqlLayouts[TemporalKind(col.DBType)])
		}
	}
	return v.Text
}

// Preview returns the statement with its arguments inlined, only to show
// it: it always runs with parameters.
func (s Statement) Preview(d drivers.Driver) string {
	quote := d.QuoteIdent("")[:1] // the identifier quote, skipped while scanning
	var b strings.Builder
	sql, next := s.SQL, 0
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch {
		case ch == quote[0]:
			end := strings.IndexByte(sql[i+1:], ch)
			if end < 0 {
				end = len(sql) - i - 1
			}
			b.WriteString(sql[i : i+end+2])
			i += end + 1
		case ch == '?':
			b.WriteString(literal(s.Args, next))
			next++
		case ch == '$' && i+1 < len(sql) && sql[i+1] >= '0' && sql[i+1] <= '9':
			j := i + 1
			for j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
				j++
			}
			n, _ := strconv.Atoi(sql[i+1 : j])
			b.WriteString(literal(s.Args, n-1))
			i = j - 1
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

func literal(args []any, i int) string {
	if i < 0 || i >= len(args) {
		return "?"
	}
	switch v := args[i].(type) {
	case nil:
		return "NULL"
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	case []byte:
		return "'" + strings.ReplaceAll(string(v), "'", "''") + "'"
	case int64, float64, float32, bool:
		return query.Text(v)
	}
	return "'" + strings.ReplaceAll(query.Text(args[i]), "'", "''") + "'"
}
