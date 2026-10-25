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
		if strings.HasPrefix(src[i:], delim) && blockDepth == 0 {
			flush()
			i += len(delim)
			continue
		}

		tok := d.lex(src, i)
		i = tok.end
		if !tok.code() {
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

			if len(words) < 4 {
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
