package ui

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Watts-Argentina/anvil/internal/connections"
	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/internal/history"
	"github.com/Watts-Argentina/anvil/internal/settings"
	"github.com/Watts-Argentina/anvil/internal/storage"
	"github.com/Watts-Argentina/anvil/models"
	_ "github.com/glebarez/go-sqlite"
	"github.com/go-gui-org/go-edit/edit/buffer"
	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend/soft"
	"github.com/go-gui-org/go-gui/gui/svg"
	"github.com/zalando/go-keyring"
)

// testApp builds an App with an open SQLite connection and a closed PG one.
func testApp(t *testing.T) *App {
	t.Helper()
	keyring.MockInit()
	ctx := context.Background()

	db, err := storage.Open("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(&connections.Service{DB: db}, &settings.Service{DB: db}, &history.Service{DB: db})

	path := filepath.Join(t.TempDir(), "shop.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"create table customers (id integer primary key, name text not null, email text)",
		"create table orders (id integer primary key, customer_id integer, total real)",
		"create view big_orders as select * from orders where total > 100",
		"insert into customers (name, email) values ('Ana', 'ana@example.com'), ('Bruno', NULL), ('Carla', '')",
		"insert into orders (customer_id, total) values (1, 50), (1, 250.5), (2, 120)",
		"create table wide (id integer primary key, c01 text, c02 text, c03 text, c04 text, c05 text, c06 text, c07 text, c08 text, c09 text, c10 text, last_column text)",
		"insert into wide values (1, 'alpha', 'beta', 'gamma', 'delta', 'epsilon', 'zeta', 'eta', 'theta', 'iota', 'kappa', 'end')",
		"insert into wide values (2, " + strings.Repeat("'"+strings.Repeat("long text ", 20)+"', ", 10) + "'end')",
	} {
		if _, err := raw.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	lite, err := a.Conns.Save(ctx, models.DBConn{Name: "shop.db", Engine: models.EngineSQLite, DB: path, Color: "#46A758"}, "")
	if err != nil {
		t.Fatal(err)
	}
	pg := models.DBConn{Name: "prod-pg", Engine: models.EnginePostgres, Host: "db", Port: 5432, User: "app", Color: "#0090FF", ReadOnly: true}
	if _, err := a.Conns.Save(ctx, pg, ""); err != nil {
		t.Fatal(err)
	}
	if a.Connections, err = a.Conns.List(ctx); err != nil {
		t.Fatal(err)
	}

	s, err := a.Sessions.Connect(ctx, lite.Conn, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Sessions.CloseAll)
	id := lite.Conn.ID
	a.Selected = id
	a.Expanded[id] = true
	key := dbKey{ConnID: id}
	if a.Tables[key], err = s.Tables(ctx, ""); err != nil {
		t.Fatal(err)
	}
	cols, err := s.Columns(ctx, "", a.Tables[key][1])
	if err != nil {
		t.Fatal(err)
	}
	a.Table = &tableView{Key: key, Table: a.Tables[key][1], Columns: cols}
	return a
}

func newTestWindow(t *testing.T, a *App) *gui.Window {
	w := gui.NewTestWindow(t, gui.WindowCfg{State: a, Width: 1100, Height: 700})
	w.SetSvgParser(svg.New()) // the real backends set it up on their own
	w.TestRender(mainView)
	return w
}

// screenshot saves a PNG if ANVIL_SCREENSHOTS points to a directory.
func screenshot(t *testing.T, w *gui.Window, name string) {
	dir := os.Getenv("ANVIL_SCREENSHOTS")
	if dir == "" {
		return
	}
	if err := soft.RenderToPNG(w, 1, filepath.Join(dir, name+".png")); err != nil {
		t.Fatal(err)
	}
}

func checkFindings(t *testing.T, w *gui.Window) {
	t.Helper()
	if found := w.TestFindings(gui.DebugAll); len(found) > 0 {
		t.Fatalf("UI defects:\n%v", found)
	}
}

func TestMainView(t *testing.T) {
	for _, theme := range []string{models.ThemeDark, models.ThemeLight} {
		t.Run(theme, func(t *testing.T) {
			a := testApp(t)
			a.Prefs.Theme = theme
			applyTheme(nil, theme)
			t.Cleanup(func() { applyTheme(nil, models.ThemeDark) })
			w := newTestWindow(t, a)
			checkFindings(t, w)
			screenshot(t, w, "main-"+theme)
		})
	}
}

func TestConnForm(t *testing.T) {
	a := testApp(t)
	w := newTestWindow(t, a)

	openConnForm(w, models.DBConn{})
	w.TestRender(nil)
	checkFindings(t, w)
	screenshot(t, w, "conn-form-pg")

	// Switching to SQLite shows the file picker.
	a.Form.Conn.Engine = models.EngineSQLite
	w.TestRender(nil)
	if ids := w.ResolveID("cf-browse"); len(ids) != 1 {
		t.Fatalf("browse button not rendered: %v", ids)
	}
	screenshot(t, w, "conn-form-sqlite")
}

func TestSidebarClickSelectsTable(t *testing.T) {
	a := testApp(t)
	w := newTestWindow(t, a)
	a.Table = nil
	w.TestRender(nil)

	id := gui.ScopeIDN(gui.ScopeIDN("conn", "", int(a.Selected)), "table", 0)
	if err := w.TestClick(id); err != nil {
		t.Fatal(err)
	}
	if a.Table == nil || a.Table.Table.Name != "big_orders" {
		t.Fatalf("table not opened: %+v", a.Table)
	}
}

// TestAllDatabases needs ANVIL_TEST_PG="host port user password database".
func TestAllDatabases(t *testing.T) {
	v := strings.Fields(os.Getenv("ANVIL_TEST_PG"))
	if len(v) != 5 {
		t.Skip("ANVIL_TEST_PG not set")
	}
	a := testApp(t)
	ctx := context.Background()
	port, _ := strconv.Atoi(v[1])
	res, err := a.Conns.Save(ctx, models.DBConn{
		Name: "cluster", Engine: models.EnginePostgres, Host: v[0], Port: port,
		User: v[2], SSLMode: "disable", Color: "#8E4EC6",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	a.Connections, _ = a.Conns.List(ctx)
	w := newTestWindow(t, a)

	// Same path as the password prompt, waiting for the async.
	openSession(w, res.Conn, v[3], false)
	waitFor(t, w, func() bool { return len(a.Databases[res.Conn.ID]) > 0 })
	if !slices.Contains(a.Databases[res.Conn.ID], "postgres") {
		t.Fatalf("databases = %v", a.Databases[res.Conn.ID])
	}

	key := dbKey{ConnID: res.Conn.ID, Database: v[4]}
	toggleDatabase(w, key)
	waitFor(t, w, func() bool { return a.Tables[key] != nil || !a.Loading[key] })
	w.TestRender(nil)
	checkFindings(t, w)
	screenshot(t, w, "all-databases")
}

// waitFor processes frames until cond holds (the asyncs come back through
// QueueCommand).
func waitFor(t *testing.T, w *gui.Window, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for async work")
		}
		time.Sleep(20 * time.Millisecond)
		w.FrameFn() // runs the pending QueueCommands
	}
}

func TestTableTabs(t *testing.T) {
	a := testApp(t)
	w := newTestWindow(t, a)
	key := a.Table.Key

	// openTable loads the columns and the first data page.
	openTable(w, key, a.Tables[key][1]) // customers
	waitFor(t, w, func() bool { return a.Table.Data.fetched })
	if got := len(a.Table.Data.Page.Rows); got != 3 {
		t.Fatalf("rows = %d (%s)", got, a.Table.Data.Err)
	}
	if !a.Table.Data.Page.Rows[1][2].Null || a.Table.Data.Page.Rows[2][2].Null {
		t.Fatal("NULL and empty string must differ")
	}

	for _, tab := range []string{tabStructure, tabData, tabSQL} {
		selectTab(w, tab)
		w.TestRender(nil)
		checkFindings(t, w)
		screenshot(t, w, "tab-"+tab)
	}

	// Grid borders and header in the light theme.
	t.Cleanup(func() { applyTheme(w, models.ThemeDark) })
	applyTheme(w, models.ThemeLight)
	selectTab(w, tabData)
	w.TestRender(nil)
	checkFindings(t, w)
	screenshot(t, w, "tab-data-light")
}

func TestSQLRun(t *testing.T) {
	a := testApp(t)
	w := newTestWindow(t, a)
	key := a.Table.Key
	selectTab(w, tabSQL)
	w.TestRender(nil)

	// The Ctrl+Enter path (a go-edit action) needs a text backend;
	// here the statement under the cursor and runSQL are tested.
	ed := editorFor(a, key)
	text := "select name, email from customers order by id;\nselect 1 as x;"
	ed.Buf = buffer.FromBytes([]byte(text))
	ed.hl = nil
	if got := statementAt(a, key, text, textOffset(text, buffer.Position{Line: 0, ByteCol: 3})); got != "select name, email from customers order by id" {
		t.Fatalf("statement under cursor = %q", got)
	}
	if got := statementAt(a, key, text, textOffset(text, buffer.Position{Line: 1, ByteCol: 2})); got != "select 1 as x" {
		t.Fatalf("statement on line 2 = %q", got)
	}
	runSQL(w, key, "select name, email from customers order by id")
	waitFor(t, w, func() bool { return ed.Run != nil })
	w.TestRender(nil)
	if ed.Err != "" || ed.Page == nil || len(ed.Page.Columns) != 2 || len(ed.Page.Rows) != 3 {
		t.Fatalf("result = %+v err=%q", ed.Page, ed.Err)
	}
	screenshot(t, w, "sql-result")

	// A long result is paginated and the grid scrolls inside its panel.
	runSQL(w, key, "with recursive n(i) as (select 1 union all select i + 1 from n where i < 300) select i, 'row ' || i as label, null as empty from n")
	waitFor(t, w, func() bool { return !ed.Running && (ed.Err != "" || ed.Page != nil && len(ed.Page.Rows) == 100) })
	if ed.Err != "" {
		t.Fatalf("err = %s", ed.Err)
	}
	if !ed.Page.HasNext {
		t.Fatal("expected more pages")
	}
	loadResultPage(w, key, 2)
	waitFor(t, w, func() bool { return ed.Page.Index == 2 })
	if got := ed.Page.Rows[0][0].Text; got != "201" || ed.Page.HasNext {
		t.Fatalf("page 3 starts at %q, next=%v", got, ed.Page.HasNext)
	}
	w.TestRender(nil)
	w.TestRender(nil)
	checkFindings(t, w)
	screenshot(t, w, "sql-many-rows")

	var entries []models.QueryHistory
	waitFor(t, w, func() bool {
		entries, _ = a.History.List(context.Background(), key.ConnID, "", 10)
		return len(entries) == 2
	})
	if entries[1].SQL != "select name, email from customers order by id" {
		t.Fatalf("history = %q", entries[1].SQL)
	}

	// DELETE without WHERE asks for confirmation before running.
	runSQL(w, key, "delete from orders")
	w.TestRender(nil)
	if !w.DialogIsVisible() {
		t.Fatal("expected confirmation dialog")
	}
	screenshot(t, w, "sql-warning")
	w.DialogDismiss()

	// Engine errors are shown in the results panel.
	runSQL(w, key, "select * from nope")
	waitFor(t, w, func() bool { return !ed.Running && ed.Err != "" })
	if !strings.Contains(ed.Err, "no such table") {
		t.Fatalf("err = %q", ed.Err)
	}
}

// rowHeight returns the height of the first data row of table id.
func rowHeight(ly *gui.Layout, id string) float32 {
	if ly == nil || ly.Shape == nil {
		return -1
	}
	if ly.Shape.ID == id+":table:scroll" && len(ly.Children) > 0 {
		return ly.Children[0].Shape.Height
	}
	for i := range ly.Children {
		if h := rowHeight(&ly.Children[i], id); h >= 0 {
			return h
		}
	}
	return -1
}

// Regression: inside nested splitters gui.Table doubled the row height
// (go-gui v0.92); the SQL tab does not use a splitter of its own.
func TestSQLGridRowHeight(t *testing.T) {
	a := testApp(t)
	w := newTestWindow(t, a)
	soft.RenderToImage(w, 1) // text measurer, as in the app
	key := a.Table.Key
	selectTab(w, tabSQL)
	ed := editorFor(a, key)
	runSQL(w, key, "select name, email from customers order by id")
	waitFor(t, w, func() bool { return ed.Run != nil })
	w.TestRender(nil)
	ly := w.TestRender(nil)
	if h := rowHeight(ly, "sql-grid"); h <= 0 || h > 30 {
		t.Fatalf("row height = %.0f", h)
	}
}

func TestWideTable(t *testing.T) {
	a := testApp(t)
	w := newTestWindow(t, a)
	key := a.Table.Key
	var wide drivers.Table
	for _, tb := range a.Tables[key] {
		if tb.Name == "wide" {
			wide = tb
		}
	}
	openTable(w, key, wide)
	waitFor(t, w, func() bool { return a.Table.Data.fetched })
	w.TestRender(nil)
	w.TestRender(nil)
	checkFindings(t, w)
	screenshot(t, w, "wide")

	// Columns that do not fit are reachable with horizontal scroll up to the
	// last one (go-gui clipped the scroll to a stale content width).
	soft.RenderToImage(w, 1) // text measurer, as in the app
	w.TestRender(nil)
	w.TestRender(nil)
	id := w.ResolveID("data-grid")[0]
	if err := w.TestScroll(id, -1000, 0); err != nil {
		t.Fatal(err)
	}
	w.ScrollHorizontalTo(id, -1e6)
	w.TestRender(nil)
	size := a.Table.Data.gridSize
	if x, _, _ := w.TestScrollOffset(id); size.ContentW <= size.W || x > size.W-size.ContentW+1 {
		t.Fatalf("offset %v cannot reach the last column (content %v, viewport %v)", x, size.ContentW, size.W)
	}
	screenshot(t, w, "wide-scrolled")

	// With a narrow table the minimum width shrinks again.
	openTable(w, key, a.Tables[key][1]) // customers
	waitFor(t, w, func() bool { return a.Table.Data.fetched })
	w.TestRender(nil)
	w.TestRender(nil)
	if cw := a.Table.Data.gridSize.ContentW; cw == 0 || cw >= size.W {
		t.Fatalf("content width after a narrow table = %v", cw)
	}
}

func TestCellText(t *testing.T) {
	n := maxCellChars
	long := strings.Repeat("a", 2*n)
	for in, want := range map[string]string{
		"short":                "short",
		strings.Repeat("a", n): strings.Repeat("a", n),
		long:                   strings.Repeat("a", n-1) + "…",
		"first\nsecond":        "first…",
		"ñ" + long:             "ñ" + strings.Repeat("a", n-2) + "…",
	} {
		if got := cellText(in); got != want {
			t.Errorf("cellText(%q) = %q, want %q", in, got, want)
		}
	}
}
