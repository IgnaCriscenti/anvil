package ui

import (
	"context"
	"slices"
	"testing"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/go-gui-org/go-gui/gui"
)

func TestTabs(t *testing.T) {
	a := testApp(t)
	w := newTestWindow(t, a)
	key := a.table().Key
	customers := a.Active
	big, orders := a.Tables[key][0], a.Tables[key][2]

	// A click opens a preview tab next to the pinned one; the next click
	// replaces it instead of adding another.
	openTable(w, key, big, false)
	preview := a.Active
	if len(a.Tabs) != 2 || !preview.preview() || a.table().Table != big {
		t.Fatalf("tabs = %d, preview = %v", len(a.Tabs), preview.preview())
	}
	openTable(w, key, orders, false)
	waitFor(t, w, func() bool { return a.table().Data.fetched })
	if len(a.Tabs) != 2 || a.Active != preview || a.table().Table != orders {
		t.Fatalf("preview not replaced: %d tabs, table %s", len(a.Tabs), a.table().Table.Name)
	}

	// Opening a table that has a tab activates it.
	openTable(w, key, customers.Table.Table, false)
	if a.Active != customers || len(a.Tabs) != 2 {
		t.Fatal("the open tab was not reused")
	}

	// A double click on the tab pins it: the next table gets a new tab.
	activateTab(w, preview)
	w.TestRender(nil)
	tabID := gui.ScopeIDN("tab", "", preview.ID)
	for range 2 {
		if err := w.TestClick(tabID); err != nil {
			t.Fatal(err)
		}
	}
	if preview.Preview {
		t.Fatal("double click did not pin the tab")
	}
	openTable(w, key, big, false)
	if len(a.Tabs) != 3 {
		t.Fatalf("tabs = %d", len(a.Tabs))
	}

	// Pending changes stay in their tab when switching away and back.
	activateTab(w, customers)
	waitFor(t, w, func() bool { return customers.Table.Data.fetched })
	typeCell(t, w, customers.Table, cellRef{Row: 0, Column: "name"}, "!", gui.KeyEnter, 0)
	activateTab(w, preview)
	activateTab(w, customers)
	if customers.Table.Data.Pending.Len() != 1 || !customers.dirty() {
		t.Fatal("pending changes lost when switching tabs")
	}
	if n, _ := unsavedChanges(a); n != 1 {
		t.Fatalf("unsaved = %d", n)
	}

	// An SQL tab has its own editor on the active tab's database.
	newSQLTab(w, key)
	sqlTab := a.Active
	if sqlTab.SQL == nil || sqlTab.SQL.Key != key || a.table() != nil {
		t.Fatal("SQL tab without its editor")
	}
	runSQL(w, sqlTab.SQL, "select name from customers order by id")
	waitFor(t, w, func() bool { return !sqlTab.SQL.Running })
	if sqlTab.SQL.Page == nil || len(sqlTab.SQL.Page.Rows) != 3 {
		t.Fatalf("SQL tab result: %+v (%s)", sqlTab.SQL.Page, sqlTab.SQL.Err)
	}
	w.TestRender(nil)
	checkFindings(t, w)
	screenshot(t, w, "tabs")

	// Closing the active tab activates its neighbor.
	if err := w.TestClick(gui.ScopeID(gui.ScopeIDN("tab", "", sqlTab.ID), "close")); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(a.Tabs, sqlTab) || a.Active == nil {
		t.Fatal("SQL tab not closed")
	}

	// Disconnecting asks for the dirty tab; Discard closes every tab.
	disconnect(w, key.ConnID)
	if a.Active != customers || len(a.Tabs) != 3 {
		t.Fatal("disconnected without asking")
	}
	clickDialog(t, w, "unsaved-discard")
	waitFor(t, w, func() bool { _, open := a.Sessions.Get(key.ConnID); return !open })
	if len(a.Tabs) != 0 || a.Active != nil {
		t.Fatalf("tabs left after disconnect: %d", len(a.Tabs))
	}
}

func TestRefreshTables(t *testing.T) {
	a := testApp(t)
	w := newTestWindow(t, a)
	key := a.table().Key
	s, _ := a.Sessions.Get(key.ConnID)
	ctx := context.Background()
	db, err := s.Pool(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "create table added_later (id integer primary key)"); err != nil {
		t.Fatal(err)
	}

	w.TestRender(nil)
	btn := gui.ScopeID(gui.ScopeIDN("conn", "", int(key.ConnID)), "refresh")
	if err := w.TestClick(btn); err != nil {
		t.Fatal(err)
	}
	has := func() bool {
		return slices.ContainsFunc(a.Tables[key], func(tb drivers.Table) bool { return tb.Name == "added_later" })
	}
	waitFor(t, w, has)
	// The click on the button does not toggle the connection row.
	if !a.Expanded[key.ConnID] {
		t.Fatal("refresh collapsed the connection")
	}
	checkFindings(t, w)
}
