package editing

import (
	"fmt"
	"strings"
	"time"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
)

// Temporal is the kind of a date/time column.
type Temporal int

const (
	NotTemporal Temporal = iota
	DateKind
	TimeKind
	DateTimeKind
)

// Accepted ISO 8601 layouts and the example shown in the error.
var (
	layouts = map[Temporal][]string{
		DateKind: {"2006-01-02"},
		TimeKind: {"15:04:05.999999999", "15:04:05.999999999Z07:00", "15:04:05.999999999 -07:00"},
		DateTimeKind: {
			"2006-01-02 15:04:05.999999999 -07:00",
			"2006-01-02 15:04:05.999999999Z07:00",
			"2006-01-02 15:04:05.999999999",
			"2006-01-02T15:04:05.999999999Z07:00",
			"2006-01-02T15:04:05.999999999",
			"2006-01-02 15:04",
			"2006-01-02",
		},
	}
	examples = map[Temporal]string{DateKind: "2006-01-02", TimeKind: "15:04:05", DateTimeKind: "2006-01-02 15:04:05"}
	// mysqlLayouts is how dates are sent to MySQL (see Target.arg).
	mysqlLayouts = map[Temporal]string{DateKind: "2006-01-02", TimeKind: "15:04:05.999999", DateTimeKind: "2006-01-02 15:04:05.999999"}
)

// Validate checks a value before accepting it into the pending changes:
// NULL only in nullable columns and ISO 8601 for dates and times (SPEC §6.4).
func Validate(col drivers.Column, v Value) error {
	if v.Null {
		if !col.Nullable {
			return appErrors.Error("Invalid value.", fmt.Sprintf("The column %s does not accept NULL.", col.Name),
				fmt.Errorf("%s is not nullable", col.Name))
		}
		return nil
	}
	kind := TemporalKind(col.DBType)
	if kind == NotTemporal {
		return nil
	}
	if _, ok := ParseTemporal(col.DBType, v.Text); ok {
		return nil
	}
	return appErrors.Error("Invalid value.",
		fmt.Sprintf("%q is not a valid %s for %s. Use the format %s.", v.Text, strings.ToLower(col.DBType), col.Name, examples[kind]),
		fmt.Errorf("invalid %s value %q", col.DBType, v.Text))
}

// ParseTemporal parses a date/time value of a column of type dbType.
func ParseTemporal(dbType, text string) (time.Time, bool) {
	for _, l := range layouts[TemporalKind(dbType)] {
		if tm, err := time.Parse(l, strings.TrimSpace(text)); err == nil {
			return tm, true
		}
	}
	return time.Time{}, false
}

// TemporalKind returns the kind of a column type (NotTemporal for the
// rest).
func TemporalKind(dbType string) Temporal {
	t := strings.ToLower(dbType)
	switch {
	case t == "date":
		return DateKind
	case strings.Contains(t, "timestamp") || strings.HasPrefix(t, "datetime"):
		return DateTimeKind
	case strings.HasPrefix(t, "time"):
		return TimeKind
	}
	return NotTemporal
}

// HasTimeZone reports whether a date/time type keeps the offset (PG
// timestamptz / time with time zone).
func HasTimeZone(dbType string) bool {
	t := strings.ToLower(dbType)
	return strings.Contains(t, "with time zone") || strings.HasSuffix(t, "tz")
}

// FormatTemporal formats tm as the value of a date/time column: the date
// only, the time only, or both with the offset if the type keeps it.
func FormatTemporal(dbType string, tm time.Time) string {
	layout := "2006-01-02 15:04:05.999999999"
	switch TemporalKind(dbType) {
	case DateKind:
		return tm.Format("2006-01-02")
	case TimeKind:
		layout = "15:04:05.999999999"
	}
	if HasTimeZone(dbType) {
		layout += " -07:00"
	}
	return tm.Format(layout)
}
