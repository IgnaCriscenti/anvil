package drivers

import "strings"

// split cuts sql into statements according to the dialect (SPEC §5.2). It
// skips statements that are empty or contain only comments.
func (d dialect) split(src string) []Span {
	var spans []Span
	delim := ";"

	start, lastEnd := -1, 0
	var words []string // first words of the statement, upper-cased
	blockDepth := 0

	flush := func() {
		if start >= 0 {
			spans = append(spans, Span{Start: start, End: lastEnd, Text: src[start:lastEnd]})
		}
		start, words, blockDepth = -1, words[:0], 0
	}

	for i := 0; i < len(src); {
		inBlock := d.block != nil && d.block(words)
		if strings.HasPrefix(src[i:], delim) && blockDepth == 0 && !inBlock {
			flush()
			i += len(delim)
			continue
		}

		tok := d.lex(src, i)
		i = tok.end
		if !tok.code() {
			continue
		}

		// GO / "/" alone on a line is a client command that ends the batch.
		if d.batchSep != "" && strings.EqualFold(src[tok.start:tok.end], d.batchSep) && aloneOnLine(src, tok.start, tok.end) {
			flush()
			continue
		}

		if tok.kind == tokWord {
			w := strings.ToUpper(src[tok.start:tok.end])

			// DELIMITER is a client command: it is not sent to the server.
			if d.delimiterCmd && start < 0 && w == "DELIMITER" {
				if nd, next := readDelimiter(src, i); nd != "" {
					delim = nd
					i = next
					continue
				}
			}

			if len(words) < 6 {
				words = append(words, w)
			}
			if d.triggerBlocks && isCreateTrigger(words) {
				switch w {
				case "BEGIN", "CASE":
					blockDepth++
				case "END":
					if blockDepth > 0 {
						blockDepth--
					}
				}
			}
		}

		if start < 0 {
			start = tok.start
		}
		lastEnd = tok.end
	}
	flush()
	return spans
}

// readDelimiter reads the new delimiter after the DELIMITER word and returns
// where the SQL continues (after the end of the line).
func readDelimiter(src string, i int) (string, int) {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	j := i
	for j < len(src) && !isSpace(src[j]) {
		j++
	}
	if j == i {
		return "", i
	}
	next := j
	if k := strings.IndexByte(src[j:], '\n'); k >= 0 {
		next = j + k + 1
	} else {
		next = len(src)
	}
	return src[i:j], next
}

// aloneOnLine reports whether src[start:end] is the only thing on its line,
// apart from spaces.
func aloneOnLine(src string, start, end int) bool {
	for i := start - 1; i >= 0 && src[i] != '\n'; i-- {
		if !isSpace(src[i]) {
			return false
		}
	}
	for i := end; i < len(src) && src[i] != '\n'; i++ {
		if !isSpace(src[i]) {
			return false
		}
	}
	return true
}

// skipCreate drops CREATE [OR REPLACE | OR ALTER] and Oracle's
// [NON]EDITIONABLE from words. ok is false if words is not a CREATE.
func skipCreate(words []string) (rest []string, ok bool) {
	if len(words) == 0 || words[0] != "CREATE" {
		return nil, false
	}
	rest = words[1:]
	if len(rest) >= 2 && rest[0] == "OR" && (rest[1] == "REPLACE" || rest[1] == "ALTER") {
		rest = rest[2:]
	}
	if len(rest) > 0 && (rest[0] == "EDITIONABLE" || rest[0] == "NONEDITIONABLE") {
		rest = rest[1:]
	}
	return rest, true
}

// isPLSQLBlock: anonymous blocks and stored PL/SQL units, ended by "/".
func isPLSQLBlock(words []string) bool {
	if len(words) == 0 {
		return false
	}
	if words[0] == "BEGIN" || words[0] == "DECLARE" {
		return true
	}
	rest, ok := skipCreate(words)
	if !ok || len(rest) == 0 {
		return false
	}
	switch rest[0] {
	case "PROCEDURE", "FUNCTION", "PACKAGE", "TRIGGER", "TYPE":
		return true
	}
	return false
}

// isTSQLRoutine: CREATE/ALTER of procedures, functions and triggers, which
// must be alone in their batch (ended by GO).
func isTSQLRoutine(words []string) bool {
	if len(words) == 0 {
		return false
	}
	rest, ok := skipCreate(words)
	if !ok {
		if words[0] != "ALTER" {
			return false
		}
		rest = words[1:]
	}
	if len(rest) == 0 {
		return false
	}
	switch rest[0] {
	case "PROCEDURE", "PROC", "FUNCTION", "TRIGGER":
		return true
	}
	return false
}

func isCreateTrigger(words []string) bool {
	if len(words) < 2 || words[0] != "CREATE" {
		return false
	}
	if words[1] == "TRIGGER" {
		return true
	}
	return len(words) >= 3 && (words[1] == "TEMP" || words[1] == "TEMPORARY") && words[2] == "TRIGGER"
}

// StatementAt returns the statement that contains offset. If offset falls
// between statements (for example right after a ";"), it returns the previous
// one; if it is before the first one, the first one.
func StatementAt(spans []Span, offset int) (Span, bool) {
	if len(spans) == 0 {
		return Span{}, false
	}
	best := spans[0]
	for _, s := range spans {
		if s.Start > offset {
			break
		}
		best = s
	}
	return best, true
}
