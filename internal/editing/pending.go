// Package editing keeps the pending changes of a table grid and turns them
// into DML that runs in a single transaction (SPEC §7). It does not know
// about the UI: the grid sends it edits and reads it back to paint.
package editing

import (
	"maps"
	"slices"
)

// Value is an edited cell. NULL is distinct from "".
type Value struct {
	Null bool
	Text string
}

// Null is the NULL value.
var Null = Value{Null: true}

// Text returns a non-NULL value.
func Text(s string) Value { return Value{Text: s} }

type Kind int

const (
	Update Kind = iota
	Insert
	Delete
)

func (k Kind) String() string {
	switch k {
	case Insert:
		return "INSERT"
	case Delete:
		return "DELETE"
	}
	return "UPDATE"
}

// Change is a pending change. Row is the row's index in the page for
// Update/Delete and the new row's ID for Insert. Values holds the modified
// columns (Update) or the ones set by the user (Insert).
type Change struct {
	Kind   Kind
	Row    int
	Values map[string]Value
}

// Pending holds the changes in the order they were made.
type Pending struct {
	changes []*Change
	nextID  int
}

// Len is the number of changes (what Save (N) shows).
func (p *Pending) Len() int { return len(p.changes) }

// Changes returns a copy of the changes in order.
func (p *Pending) Changes() []Change {
	out := make([]Change, len(p.changes))
	for i, c := range p.changes {
		out[i] = Change{Kind: c.Kind, Row: c.Row, Values: maps.Clone(c.Values)}
	}
	return out
}

// Clear drops every change.
func (p *Pending) Clear() { p.changes = nil }

// existing returns the Update or Delete of an existing row.
func (p *Pending) existing(row int) (int, *Change) {
	for i, c := range p.changes {
		if c.Kind != Insert && c.Row == row {
			return i, c
		}
	}
	return -1, nil
}

// inserted returns the Insert of a new row.
func (p *Pending) inserted(id int) (int, *Change) {
	for i, c := range p.changes {
		if c.Kind == Insert && c.Row == id {
			return i, c
		}
	}
	return -1, nil
}

// Set records the value of an existing row's cell. Setting the original
// value back drops the column from the Update (and the Update if it ends up
// empty). A deleted row is not edited.
func (p *Pending) Set(row int, column string, v, original Value) {
	i, c := p.existing(row)
	if c != nil && c.Kind == Delete {
		return
	}
	if v == original {
		if c != nil {
			delete(c.Values, column)
			if len(c.Values) == 0 {
				p.changes = slices.Delete(p.changes, i, i+1)
			}
		}
		return
	}
	if c == nil {
		c = &Change{Kind: Update, Row: row, Values: map[string]Value{}}
		p.changes = append(p.changes, c)
	}
	c.Values[column] = v
}

// Value returns the pending value of an existing row's cell.
func (p *Pending) Value(row int, column string) (Value, bool) {
	if _, c := p.existing(row); c != nil && c.Kind == Update {
		v, ok := c.Values[column]
		return v, ok
	}
	return Value{}, false
}

// Delete marks an existing row as deleted; its pending Update is dropped.
func (p *Pending) Delete(row int) {
	if i, c := p.existing(row); c != nil {
		if c.Kind == Delete {
			return
		}
		p.changes = slices.Delete(p.changes, i, i+1)
	}
	p.changes = append(p.changes, &Change{Kind: Delete, Row: row})
}

// Undelete removes the Delete of an existing row (its earlier edits are
// lost).
func (p *Pending) Undelete(row int) {
	if i, c := p.existing(row); c != nil && c.Kind == Delete {
		p.changes = slices.Delete(p.changes, i, i+1)
	}
}

// Deleted reports whether an existing row is marked as deleted.
func (p *Pending) Deleted(row int) bool {
	_, c := p.existing(row)
	return c != nil && c.Kind == Delete
}

// Modified reports whether an existing row has a pending Update.
func (p *Pending) Modified(row int) bool {
	_, c := p.existing(row)
	return c != nil && c.Kind == Update
}

// AddRow adds an empty new row and returns its ID.
func (p *Pending) AddRow() int {
	p.nextID++
	p.changes = append(p.changes, &Change{Kind: Insert, Row: p.nextID, Values: map[string]Value{}})
	return p.nextID
}

// NewRows returns the IDs of the new rows in order.
func (p *Pending) NewRows() []int {
	var ids []int
	for _, c := range p.changes {
		if c.Kind == Insert {
			ids = append(ids, c.Row)
		}
	}
	return ids
}

// SetNew sets a cell of a new row.
func (p *Pending) SetNew(id int, column string, v Value) {
	if _, c := p.inserted(id); c != nil {
		c.Values[column] = v
	}
}

// NewValue returns the value set in a cell of a new row.
func (p *Pending) NewValue(id int, column string) (Value, bool) {
	if _, c := p.inserted(id); c != nil {
		v, ok := c.Values[column]
		return v, ok
	}
	return Value{}, false
}

// DeleteNew removes a new row: nothing is inserted.
func (p *Pending) DeleteNew(id int) {
	if i, c := p.inserted(id); c != nil {
		p.changes = slices.Delete(p.changes, i, i+1)
	}
}
