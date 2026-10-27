package ui

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Watts-Argentina/anvil/internal/connections"
	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/internal/editing"
	"github.com/Watts-Argentina/anvil/internal/history"
	"github.com/Watts-Argentina/anvil/internal/schema"
	"github.com/Watts-Argentina/anvil/internal/settings"
	"github.com/Watts-Argentina/anvil/internal/storage"
	"github.com/Watts-Argentina/anvil/models"
	"github.com/go-gui-org/go-edit/edit/buffer"
	"github.com/go-gui-org/go-gui/gui"
	"github.com/go-gui-org/go-gui/gui/backend/soft"
	"github.com/go-gui-org/go-gui/gui/svg"
	"github.com/zalando/go-keyring"
)

// bookstoreDDL is the demo database of the README screenshots.
var bookstoreDDL = []string{
	`create table authors (id integer primary key, name text not null, country text, born date)`,
	`create table books (id integer primary key, title text not null, author_id integer references authors (id),
		published date, price numeric(8, 2), stock integer not null default 0)`,
	`create table customers (id integer primary key, name text not null, email text unique)`,
	`create table orders (id integer primary key, customer_id integer not null references customers (id),
		book_id integer not null references books (id), quantity integer not null, ordered_at timestamp)`,
	`create view low_stock as select * from books where stock < 5`,
	`insert into authors (name, country, born) values
		('Jorge Luis Borges', 'Argentina', '1899-08-24'), ('Julio Cortázar', 'Argentina', '1914-08-26'),
		('Ursula K. Le Guin', 'United States', '1929-10-21'), ('Italo Calvino', 'Italy', '1923-10-15'),
		('Clarice Lispector', 'Brazil', '1920-12-10'), ('Silvina Ocampo', 'Argentina', NULL)`,
	`insert into books (title, author_id, published, price, stock) values
		('Ficciones', 1, '1944-01-01', 18.50, 12), ('El Aleph', 1, '1949-06-01', 16.90, 3),
		('Rayuela', 2, '1963-06-28', 24.00, 7), ('Bestiario', 2, '1951-01-01', 14.75, 0),
		('The Left Hand of Darkness', 3, '1969-03-01', 19.99, 9), ('The Dispossessed', 3, '1974-05-01', 21.00, 4),
		('Invisible Cities', 4, '1972-11-01', 15.40, 15), ('If on a winter''s night a traveler', 4, '1979-06-01', 17.25, 8),
		('The Hour of the Star', 5, '1977-10-26', 13.90, 6), ('La furia', 6, NULL, 20.00, 2)`,
	`insert into customers (name, email) values ('Ana Pérez', 'ana@example.com'), ('Bruno Díaz', NULL),
		('Carla Gómez', 'carla@example.com')`,
	`insert into orders (customer_id, book_id, quantity, ordered_at) values
		(1, 3, 1, '2026-10-01 10:24:00'), (1, 7, 2, '2026-10-03 18:02:10'), (2, 1, 1, '2026-10-12 09:15:42'),
		(3, 5, 3, '2026-10-20 21:40:05'), (3, 3, 1, '2026-10-25 13:05:00')`,
}

// TestReadmeScreenshots regenerates the README screenshots:
//
//	ANVIL_README_SHOTS=docs/screenshots go test ./ui/ -run TestReadmeScreenshots
func TestReadmeScreenshots(t *testing.T) {
	dir := os.Getenv("ANVIL_README_SHOTS")
	if dir == "" {
		t.Skip("ANVIL_README_SHOTS not set")
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join("..", dir) // relative to the repo root
	}
	// measure lays out with the real text widths first. The SQL editor
	// skips it: go-edit keeps the measurer of the first soft render and
	// uses it after it is closed.
	shot := func(w *gui.Window, name string, measure bool) {
		t.Helper()
		if measure {
			soft.RenderToImage(w, 1)
			w.FrameFn()
			w.TestRender(nil)
		}
		w.TestRender(nil)
		if err := soft.RenderToPNG(w, 2, filepath.Join(dir, name+".png")); err != nil {
			t.Fatal(err)
		}
	}
	books := drivers.Table{Name: "books"}

	t.Run("data", func(t *testing.T) {
		a, w := demoApp(t, models.ThemeDark)
		tv := openDemoTable(t, w, a, books, tabData)
		p := &tv.Data.Pending
		p.Set(1, "price", editing.Text("15.90"), editing.Text("16.9"))
		p.Set(3, "stock", editing.Text("5"), editing.Text("0"))
		p.Delete(9)
		id := p.AddRow()
		p.SetNew(id, "title", editing.Text("Cuentos completos"))
		p.SetNew(id, "author_id", editing.Text("6"))
		selectRow(tv, rowKey{Row: 4}, "title", 0)
		selectRow(tv, rowKey{Row: 6}, "title", gui.ModCtrl)
		shot(w, "data", true)
	})

	t.Run("structure", func(t *testing.T) {
		_, w := demoApp(t, models.ThemeLight)
		openDemoTable(t, w, appOf(w), drivers.Table{Name: "orders"}, tabStructure)
		shot(w, "structure", true)
	})

	t.Run("sql", func(t *testing.T) {
		a, w := demoApp(t, models.ThemeDark)
		tv := openDemoTable(t, w, a, books, tabSQL)
		ed := editorFor(a, tv.Key)
		text := "-- Best-selling authors\nselect a.name as author, count(*) as orders, sum(o.quantity) as copies\n" +
			"from orders o\n  join books b on b.id = o.book_id\n  join authors a on a.id = b.author_id\n" +
			"group by a.name\norder by copies desc;"
		ed.Buf = buffer.FromBytes([]byte(text))
		ed.hl = nil
		runSQL(w, tv.Key, text)
		waitFor(t, w, func() bool { return ed.Run != nil })
		shot(w, "sql", false)
	})

	t.Run("new-table", func(t *testing.T) {
		a, w := demoApp(t, models.ThemeDark)
		tv := openDemoTable(t, w, a, books, tabData)
		openNewTable(w, tv.Key)
		f := &a.NewTable
		f.Name = "reviews"
		f.Columns[1].Column = schema.Column{Name: "book_id", Type: "integer"}
		f.addColumn(schema.Column{Name: "rating", Type: "integer", Default: "5"})
		f.addColumn(schema.Column{Name: "comment", Type: "text", Nullable: true})
		f.addColumn(schema.Column{Name: "created_at", Type: "timestamp", Default: "CURRENT_TIMESTAMP"})
		shot(w, "new-table", true)
	})

	t.Run("connection", func(t *testing.T) {
		a, w := demoApp(t, models.ThemeLight)
		openDemoTable(t, w, a, books, tabData)
		openConnForm(w, models.DBConn{})
		a.Form.Conn.Name = "analytics"
		a.Form.Conn.User = "reporting"
		shot(w, "connection", true)
	})
}

// demoApp builds an App connected to a new bookstore.db, with a closed
// PostgreSQL and MySQL connection in the sidebar.
func demoApp(t *testing.T, theme string) (*App, *gui.Window) {
	t.Helper()
	keyring.MockInit()
	ctx := context.Background()
	applyTheme(nil, theme)
	t.Cleanup(func() { applyTheme(nil, models.ThemeDark) })

	db, err := storage.Open("file:" + t.Name() + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(&connections.Service{DB: db}, &settings.Service{DB: db}, &history.Service{DB: db})
	a.Prefs.Theme = theme

	path := filepath.Join(t.TempDir(), "bookstore.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range bookstoreDDL {
		if _, err := raw.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	raw.Close()

	for _, c := range []models.DBConn{
		{Name: "production", Engine: models.EnginePostgres, Host: "db.internal", Port: 5432, User: "app", Color: "#0090FF", ReadOnly: true},
		{Name: "bookstore.db", Engine: models.EngineSQLite, DB: path, Color: "#46A758"},
		{Name: "legacy-shop", Engine: models.EngineMySQL, Host: "localhost", Port: 3306, User: "root", Color: "#F76B15"},
	} {
		if _, err := a.Conns.Save(ctx, c, ""); err != nil {
			t.Fatal(err)
		}
	}
	if a.Connections, err = a.Conns.List(ctx); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(a.Connections, func(c models.DBConn) bool { return c.Engine == models.EngineSQLite })
	if _, err := a.Sessions.Connect(ctx, a.Connections[i], ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Sessions.CloseAll)

	w := gui.NewTestWindow(t, gui.WindowCfg{State: a, Width: 1200, Height: 720})
	w.SetSvgParser(svg.New())
	w.TestRender(mainView)
	openSession(w, a.Connections[i], "", false)
	waitFor(t, w, func() bool { return len(a.Tables[dbKey{ConnID: a.Connections[i].ID}]) > 0 })
	return a, w
}

// openDemoTable opens a table of bookstore.db on a tab and waits for it.
func openDemoTable(t *testing.T, w *gui.Window, a *App, tb drivers.Table, tab string) *tableView {
	t.Helper()
	a.TabSel = tab
	for k := range a.Tables {
		openTable(w, k, tb)
	}
	waitFor(t, w, func() bool { return a.Table != nil && !a.Table.Loading })
	if tab == tabData {
		waitFor(t, w, func() bool { return a.Table.Data.fetched })
	}
	return a.Table
}
