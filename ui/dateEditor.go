package ui

import (
	"time"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/internal/editing"
	"github.com/Watts-Argentina/anvil/ui/components"
	"github.com/go-gui-org/go-gui/gui"
)

// dateEdit is the state of the date editor: the cell, its column and the
// chosen moment. Clock is the time field ("15:04:05"); Moment keeps the
// date, the offset and the fraction of a second of the original value.
type dateEdit struct {
	Cell   cellRef
	Col    drivers.Column
	Moment time.Time
	Clock  string
	Err    string
}

var calendarID = gui.ScopeID("date-editor", "calendar")

const clockLayout = "15:04:05"

// openDateEditor edits a date/time cell with a calendar (dates) and a time
// field (times) instead of typing the value (SPEC §6.4).
func openDateEditor(w *gui.Window, tv *tableView, ref cellRef, col drivers.Column) {
	moment := time.Now().Truncate(time.Second)
	if v, _ := tv.cellValue(ref); !v.Null && v.Text != "" {
		if tm, ok := editing.ParseTemporal(col.DBType, v.Text); ok {
			moment = tm
		}
	}
	tv.Data.Date = dateEdit{Cell: ref, Col: col, Moment: moment, Clock: moment.Format(clockLayout)}
	// The calendar opens on the month of the value, not on the last one shown.
	w.DatePickerReset(calendarID)

	kind := editing.TemporalKind(col.DBType)
	focus := "date-ok"
	if kind != editing.DateKind {
		focus = "date-clock"
	}
	w.Dialog(gui.DialogCfg{
		ID:         "date-editor",
		DialogType: gui.DialogCustom,
		Title:      components.Ellipsis(col.Name, 24),
		Width:      360,
		FocusID:    focus,
		CustomView: func(w *gui.Window) gui.View { return dateEditorView(tv, kind) },
	})
}

func dateEditorView(tv *tableView, kind editing.Temporal) gui.View {
	t := gui.CurrentTheme()
	de := &tv.Data.Date

	content := []gui.View{gui.Text(gui.TextCfg{Text: de.Col.DBType, TextStyle: t.TextStyleCaption})}
	if kind != editing.TimeKind {
		content = append(content, gui.DatePicker(gui.DatePickerCfg{
			ID:    calendarID,
			Dates: []time.Time{de.Moment},
			OnSelect: func(days []time.Time, ctx gui.EventCtx) {
				if len(days) > 0 {
					m, d := de.Moment, days[0]
					de.Moment = time.Date(d.Year(), d.Month(), d.Day(), m.Hour(), m.Minute(), m.Second(), m.Nanosecond(), m.Location())
				}
				ctx.Consume()
			},
		}))
	}
	if kind != editing.DateKind {
		content = append(content, gui.Row(gui.ContainerCfg{
			Padding:    gui.NoPadding,
			Spacing:    gui.SpacingSmall,
			SizeBorder: gui.NoBorder,
			VAlign:     gui.VAlignMiddle,
			Content: []gui.View{
				gui.Text(gui.TextCfg{Text: "Time", TextStyle: t.TextStyleSecondary}),
				gui.Input(gui.InputCfg{
					ID:          "date-clock",
					Text:        de.Clock,
					Mask:        "99:99:99",
					Placeholder: "HH:MM:SS",
					Width:       110,
					Sizing:      gui.FixedFit,
					A11YCfg:     gui.A11YCfg{A11YLabel: "Time"},
					OnTextChanged: func(s string, ctx gui.EventCtx) {
						de.Clock, de.Err = s, ""
					},
					OnEnter: func(ctx gui.EventCtx) {
						applyDate(ctx.Window, tv)
						ctx.Consume()
					},
				}),
			},
		}))
	}
	if de.Err != "" {
		content = append(content, components.ErrorText(de.Err))
	}

	buttons := []gui.View{
		gui.Button(gui.ButtonCfg{
			ID:      "date-now",
			Label:   "Now",
			Variant: gui.ButtonGhost,
			OnClick: func(ctx gui.EventCtx) {
				now := time.Now().Truncate(time.Second)
				de.Moment, de.Clock, de.Err = now, now.Format(clockLayout), ""
				ctx.Window.DatePickerReset(calendarID)
				ctx.Consume()
			},
		}),
	}
	if de.Col.Nullable {
		buttons = append(buttons, gui.Button(gui.ButtonCfg{
			ID:      "date-null",
			Label:   "Set NULL",
			Variant: gui.ButtonGhost,
			OnClick: func(ctx gui.EventCtx) {
				if setCell(ctx.Window, tv, de.Cell, editing.Null) {
					ctx.Window.DialogDismiss()
				}
				ctx.Consume()
			},
		}))
	}
	buttons = append(buttons,
		components.Spacer(),
		components.DismissButton("date-cancel", "Cancel", gui.ButtonSecondary),
		gui.Button(gui.ButtonCfg{
			ID:      "date-ok",
			Label:   "OK",
			Variant: gui.ButtonPrimary,
			OnClick: func(ctx gui.EventCtx) {
				applyDate(ctx.Window, tv)
				ctx.Consume()
			},
		}),
	)
	content = append(content, gui.Row(gui.ContainerCfg{
		Sizing:     gui.FillFit,
		Padding:    gui.NoPadding,
		Spacing:    gui.SpacingSmall,
		SizeBorder: gui.NoBorder,
		VAlign:     gui.VAlignMiddle,
		Content:    buttons,
	}))

	return components.DialogBody(content...)
}

// applyDate records the chosen date and time as the cell's pending value.
func applyDate(w *gui.Window, tv *tableView) {
	de := &tv.Data.Date
	m := de.Moment
	if editing.TemporalKind(de.Col.DBType) != editing.DateKind {
		clock, err := time.Parse(clockLayout, de.Clock)
		if err != nil {
			de.Err = "Enter the time as HH:MM:SS (00:00:00 to 23:59:59)."
			return
		}
		// The fraction of a second survives only if the time was not changed.
		nsec := 0
		if de.Clock == m.Format(clockLayout) {
			nsec = m.Nanosecond()
		}
		m = time.Date(m.Year(), m.Month(), m.Day(), clock.Hour(), clock.Minute(), clock.Second(), nsec, m.Location())
	}
	if setCell(w, tv, de.Cell, editing.Text(editing.FormatTemporal(de.Col.DBType, m))) {
		w.DialogDismiss()
	}
}
