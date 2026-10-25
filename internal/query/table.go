package query

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

// TableSource is a table or view open in a tab.
type TableSource struct {
	Driver  drivers.Driver
	DB      *sql.DB
	Table   drivers.Table
	Columns []drivers.Column
}

// keyColumn returns the single-column PK for keyset pagination (SPEC §6.2),
// or nil if OFFSET must be used.
func (s TableSource) keyColumn() *drivers.Column {
	var key *drivers.Column
	for i := range s.Columns {
		if s.Columns[i].PK {
			if key != nil {
				return nil // composite PK
			}
			key = &s.Columns[i]
		}
	}
	return key
}

func (s TableSource) orderBy() string {
	pk := slices.Clone(s.Columns)
	pk = slices.DeleteFunc(pk, func(c drivers.Column) bool { return !c.PK })
	slices.SortFunc(pk, func(a, b drivers.Column) int { return a.PKOrdinal - b.PKOrdinal })
	if len(pk) == 0 {
		return ""
	}
	parts := make([]string, len(pk))
	for i, c := range pk {
		parts[i] = s.Driver.QuoteIdent(c.Name)
	}
	return " ORDER BY " + strings.Join(parts, ", ")
}

type Dir int

const (
	First Dir = iota
	Next
	Prev
	Reload
)

// Page is a page of a table.
type Page struct {
	Result
	Index   int // starting at 0
	Size    int
	HasNext bool
	Keyset  bool

	first, last any // keyset keys of the page
}

// LoadTablePage loads the table page in direction dir starting from
// cur (the current page; empty for First).
func LoadTablePage(ctx context.Context, src TableSource, cur Page, dir Dir, size int) (Page, error) {
	page, err := loadTablePage(ctx, src, cur, dir, size)
	if err != nil {
		return Page{}, engineError("Failed to load rows.", err)
	}
	return page, nil
}

func loadTablePage(ctx context.Context, src TableSource, cur Page, dir Dir, size int) (Page, error) {
	d := src.Driver
	table := drivers.QualifiedName(d, src.Table)
	ph := func(n int) string { return d.Placeholder(n) }

	if dir == Reload && cur.Index == 0 || dir == Prev && cur.Index <= 1 {
		dir = First
	}
	if dir != First && len(cur.Rows) == 0 {
		dir = First
	}

	key := src.keyColumn()
	if key == nil {
		index := 0
		switch dir {
		case Next:
			index = cur.Index + 1
		case Prev:
			index = cur.Index - 1
		case Reload:
			index = cur.Index
		}
		q := "SELECT * FROM " + table + src.orderBy() + " LIMIT " + ph(1) + " OFFSET " + ph(2)
		rows, err := src.DB.QueryContext(ctx, q, size+1, index*size)
		if err != nil {
			return Page{}, err
		}
		res, more, err := scan(rows, 0, size)
		if err != nil {
			return Page{}, err
		}
		return Page{Result: res, Index: index, Size: size, HasNext: more}, nil
	}

	k := d.QuoteIdent(key.Name)
	var (
		q     string
		args  []any
		index int
	)
	switch dir {
	case First:
		q = "SELECT * FROM " + table + " ORDER BY " + k + " LIMIT " + ph(1)
		args = []any{size + 1}
	case Next:
		q = "SELECT * FROM " + table + " WHERE " + k + " > " + ph(1) + " ORDER BY " + k + " LIMIT " + ph(2)
		args, index = []any{cur.last, size + 1}, cur.Index+1
	case Reload:
		q = "SELECT * FROM " + table + " WHERE " + k + " >= " + ph(1) + " ORDER BY " + k + " LIMIT " + ph(2)
		args, index = []any{cur.first, size + 1}, cur.Index
	case Prev:
		q = "SELECT * FROM " + table + " WHERE " + k + " < " + ph(1) + " ORDER BY " + k + " DESC LIMIT " + ph(2)
		args, index = []any{cur.first, size}, cur.Index-1
	}

	rows, err := src.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return Page{}, err
	}
	res, more, err := scan(rows, 0, size)
	if err != nil {
		return Page{}, err
	}
	if dir == Prev {
		slices.Reverse(res.Rows)
		more = true // we are coming back from the next page
	}

	page := Page{Result: res, Index: index, Size: size, HasNext: more, Keyset: true}
	if col := slices.IndexFunc(res.Columns, func(c Column) bool { return c.Name == key.Name }); col >= 0 && len(res.Rows) > 0 {
		page.first = res.Rows[0][col].Raw
		page.last = res.Rows[len(res.Rows)-1][col].Raw
	}
	return page, nil
}

// Count counts the table's rows (on demand, SPEC §6.2).
func Count(ctx context.Context, src TableSource) (int64, error) {
	var n int64
	err := src.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+drivers.QualifiedName(src.Driver, src.Table)).Scan(&n)
	if err != nil {
		return 0, engineError("Failed to count rows.", err)
	}
	return n, nil
}

// engineError wraps an engine error for the UI.
func engineError(title string, err error) error {
	if isCanceled(err) {
		return appErrors.Error("Query cancelled.", "The query was cancelled.", err)
	}
	return appErrors.Error(title, err.Error(), err)
}

func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

// FormatCount formats a row count ("1,234").
func FormatCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
