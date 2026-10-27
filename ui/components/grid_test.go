package components

import (
	"strings"
	"testing"
)

func TestCellText(t *testing.T) {
	n := MaxCellChars
	long := strings.Repeat("a", 2*n)
	for in, want := range map[string]string{
		"short":                "short",
		strings.Repeat("a", n): strings.Repeat("a", n),
		long:                   strings.Repeat("a", n-1) + "…",
		"first\nsecond":        "first…",
		"ñ" + long:             "ñ" + strings.Repeat("a", n-2) + "…",
	} {
		if got := CellText(in); got != want {
			t.Errorf("CellText(%q) = %q, want %q", in, got, want)
		}
	}
}
