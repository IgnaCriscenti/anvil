package ui

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Watts-Argentina/anvil/internal/query"
	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/ui/components"
	"github.com/go-gui-org/go-gui/gui"
)

// dataState is a table's Data tab: a read-only paginated grid (editing
// comes with SPEC §7).
type dataState struct {
	Page     query.Page
	Size     int
	Loading  bool
	Err      string
	Count    string // empty until requested
	Counting bool
	fetched  bool
	gridSize boxSize
}

func (d dataState) loaded() bool { return d.fetched || d.Loading }

const firstPage = query.First

var pageSizes = []string{"20", "50", "100", "200", "500"}

func itoa(n int) string { return strconv.Itoa(n) }

// tableSource builds the table source with its database's pool.
func tableSource(ctx context.Context, a *App, tv *tableView) (query.TableSource, error) {
	s, ok := a.Sessions.Get(tv.Key.ConnID)
	if !ok {
		return query.TableSource{}, fmt.Errorf("connection closed")
	}
	db, err := s.Pool(ctx, tv.Key.Database)
	if err != nil {
		return query.TableSource{}, err
	}
	return query.TableSource{Driver: s.Driver, DB: db, Table: tv.Table, Columns: tv.Columns}, nil
}

func loadData(w *gui.Window, tv *tableView, dir query.Dir) {
	a := appOf(w)
	if tv.Data.Loading {
		return
	}
	tv.Data.Loading, tv.Data.Err = true, ""
	cur, size := tv.Data.Page, tv.Data.Size

	async(w, func(ctx context.Context) (query.Page, error) {
		src, err := tableSource(ctx, a, tv)
		if err != nil {
			return query.Page{}, err
		}
		return query.LoadTablePage(ctx, src, cur, dir, size)
	}, func(w *gui.Window, p query.Page, err error) {
		tv.Data.Loading, tv.Data.fetched = false, true
		if err != nil {
			tv.Data.Err = errText(err)
			return
		}
		tv.Data.Page = p
	})
}

func countRows(w *gui.Window, tv *tableView) {
	a := appOf(w)
	tv.Data.Counting = true
	async(w, func(ctx context.Context) (int64, error) {
		src, err := tableSource(ctx, a, tv)
		if err != nil {
			return 0, err
		}
		return query.Count(ctx, src)
	}, func(w *gui.Window, n int64, err error) {
		tv.Data.Counting = false
		if err != nil {
			showError(w, err)
			return
		}
		tv.Data.Count = query.FormatCount(n) + " rows"
	})
}

func dataTab(w *gui.Window, a *App, tv *tableView) gui.View {
	t := gui.CurrentTheme()
	d := tv.Data

	status := ""
	switch {
	case d.Loading:
		status = "Loading…"
	case d.fetched:
		first := d.Page.Index*d.Page.Size + 1
		status = fmt.Sprintf("Rows %d–%d", first, first+len(d.Page.Rows)-1)
		if len(d.Page.Rows) == 0 {
			status = "No rows"
		}
	}

	countView := gui.View(gui.Button(gui.ButtonCfg{
		ID:       "data-count",
		Label:    "Count rows",
		Variant:  gui.ButtonGhost,
		Disabled: d.Counting || !d.fetched,
		OnClick: func(ctx gui.EventCtx) {
			countRows(ctx.Window, tv)
			ctx.Consume()
		},
	}))
	if d.Count != "" {
		countView = gui.Text(gui.TextCfg{Text: d.Count, TextStyle: t.TextStyleSecondary})
	}

	toolbar := gui.Row(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.PadVH(4, 4),
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		VAlign:     gui.VAlignMiddle,
		Content: []gui.View{
			components.IconButton("refresh", "", gui.ButtonCfg{
				ID:       "data-refresh",
				A11YCfg:  gui.A11YCfg{A11YLabel: "Refresh"},
				Disabled: d.Loading,
				OnClick: func(ctx gui.EventCtx) {
					tv.Data.Count = ""
					loadData(ctx.Window, tv, query.Reload)
					ctx.Consume()
				},
			}),
			components.IconButton("chevronLeft", "", gui.ButtonCfg{
				ID:       "data-prev",
				A11YCfg:  gui.A11YCfg{A11YLabel: "Previous page"},
				Disabled: d.Loading || d.Page.Index == 0,
				OnClick: func(ctx gui.EventCtx) {
					loadData(ctx.Window, tv, query.Prev)
					ctx.Consume()
				},
			}),
			gui.Text(gui.TextCfg{Text: "Page " + itoa(d.Page.Index+1), TextStyle: t.TextStyleBodySmall}),
			components.IconButton("chevronRight", "", gui.ButtonCfg{
				ID:       "data-next",
				A11YCfg:  gui.A11YCfg{A11YLabel: "Next page"},
				Disabled: d.Loading || !d.Page.HasNext,
				OnClick: func(ctx gui.EventCtx) {
					loadData(ctx.Window, tv, query.Next)
					ctx.Consume()
				},
			}),
			gui.Select(gui.SelectCfg{
				ID:       "data-page-size",
				Items:    pageSizes,
				Selected: []string{itoa(d.Size)},
				OnSelect: func(sel []string, ctx gui.EventCtx) {
					if n, err := strconv.Atoi(sel[0]); err == nil && n != tv.Data.Size {
						tv.Data.Size = min(max(n, models.MinPageSize), models.MaxPageSize)
						loadData(ctx.Window, tv, query.First)
					}
					ctx.Consume()
				},
			}),
			gui.Text(gui.TextCfg{Text: "rows per page", TextStyle: t.TextStyleCaption}),
			gui.Row(gui.ContainerCfg{Sizing: gui.FillFit, Padding: gui.NoPadding, SizeBorder: gui.NoBorder}),
			gui.Text(gui.TextCfg{Text: status, TextStyle: t.TextStyleCaption}),
			countView,
		},
	})

	var body gui.View
	switch {
	case d.Err != "":
		body = gui.Text(gui.TextCfg{Text: d.Err, Mode: gui.TextModeWrap, TextStyle: t.TextStyleError})
	case !d.fetched:
		body = gui.Text(gui.TextCfg{Text: "Loading rows…", TextStyle: t.TextStyleSecondary})
	default:
		body = resultGrid("data-grid", d.Page.Result, &tv.Data.gridSize)
	}

	return gui.Column(gui.ContainerCfg{
		ID:         "data",
		Sizing:     gui.FillFill,
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		Content:    []gui.View{toolbar, body},
	})
}
