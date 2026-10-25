package drivers

import "strings"

// dialect describes each engine's lexical rules that affect splitting and
// statement classification.
type dialect struct {
	backtickIdent     bool // MySQL: `ident`
	hashComment       bool // MySQL: # comment
	dashNeedsSpace    bool // MySQL: "-- " requires a space after it
	backslashStrings  bool // MySQL: \ escapes inside '...' and "..."
	doubleQuoteString bool // MySQL: "..." is a string
	execComments      bool // MySQL: /*! ... */ is executed
	dollarQuote       bool // PG: $tag$ ... $tag$
	eStrings          bool // PG: E'...' with \ escapes
	nestedComments    bool // PG: /* /* */ */
	delimiterCmd      bool // MySQL: DELIMITER xx
	triggerBlocks     bool // SQLite: BEGIN ... END of CREATE TRIGGER
}

var (
	sqliteDialect = dialect{triggerBlocks: true}
	pgDialect     = dialect{dollarQuote: true, eStrings: true, nestedComments: true}
	mysqlDialect  = dialect{
		backtickIdent: true, hashComment: true, dashNeedsSpace: true,
		backslashStrings: true, doubleQuoteString: true, execComments: true,
		delimiterCmd: true,
	}
)

type tokKind int

const (
	tokSpace tokKind = iota
	tokComment
	tokWord   // keyword, identifier or number
	tokString // string literal (includes dollar-quoted)
	tokIdent  // quoted identifier
	tokExec   // MySQL executable comment
	tokPunct  // any single byte
)

type token struct {
	kind       tokKind
	start, end int
}

func (t token) code() bool { return t.kind != tokSpace && t.kind != tokComment }

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// lex returns the token that starts at src[i]. Unterminated strings and
// comments run to the end.
func (d dialect) lex(src string, i int) token {
	n := len(src)
	c := src[i]
	at := func(j int) byte {
		if j < n {
			return src[j]
		}
		return 0
	}

	switch {
	case isSpace(c):
		j := i + 1
		for j < n && isSpace(src[j]) {
			j++
		}
		return token{tokSpace, i, j}

	case c == '-' && at(i+1) == '-' && (!d.dashNeedsSpace || i+2 >= n || isSpace(src[i+2])),
		c == '#' && d.hashComment:
		j := strings.IndexByte(src[i:], '\n')
		if j < 0 {
			return token{tokComment, i, n}
		}
		return token{tokComment, i, i + j}

	case c == '/' && at(i+1) == '*':
		kind := tokComment
		if d.execComments && at(i+2) == '!' {
			kind = tokExec
		}
		depth := 1
		j := i + 2
		for j < n {
			if src[j] == '*' && at(j+1) == '/' {
				depth--
				j += 2
				if depth == 0 || !d.nestedComments {
					return token{kind, i, j}
				}
				continue
			}
			if d.nestedComments && src[j] == '/' && at(j+1) == '*' {
				depth++
				j += 2
				continue
			}
			j++
		}
		return token{kind, i, n}

	case c == '\'':
		return token{tokString, i, d.quoted(src, i, '\'', d.backslashStrings)}

	case c == '"':
		if d.doubleQuoteString {
			return token{tokString, i, d.quoted(src, i, '"', d.backslashStrings)}
		}
		return token{tokIdent, i, d.quoted(src, i, '"', false)}

	case c == '`' && d.backtickIdent:
		return token{tokIdent, i, d.quoted(src, i, '`', false)}

	case c == '$' && d.dollarQuote && (i == 0 || !isWordByte(src[i-1])):
		if end, ok := dollarString(src, i); ok {
			return token{tokString, i, end}
		}
		return token{tokPunct, i, i + 1}

	case isWordByte(c):
		j := i + 1
		for j < n && isWordByte(src[j]) {
			j++
		}
		// PG: E'...' allows backslash escapes.
		if d.eStrings && j == i+1 && (c == 'e' || c == 'E') && at(j) == '\'' {
			return token{tokString, i, d.quoted(src, j, '\'', true)}
		}
		return token{tokWord, i, j}
	}
	return token{tokPunct, i, i + 1}
}

// quoted returns the end of the literal opened by src[i] == q. The quote is
// escaped by doubling it and, if backslash, with \.
func (d dialect) quoted(src string, i int, q byte, backslash bool) int {
	n := len(src)
	j := i + 1
	for j < n {
		switch src[j] {
		case '\\':
			if backslash {
				j += 2
				continue
			}
		case q:
			if j+1 < n && src[j+1] == q {
				j += 2
				continue
			}
			return j + 1
		}
		j++
	}
	return n
}

// dollarString recognizes $tag$ ... $tag$ in src[i:].
func dollarString(src string, i int) (int, bool) {
	j := i + 1
	for j < len(src) && isWordByte(src[j]) {
		j++
	}
	if j >= len(src) || src[j] != '$' {
		return 0, false
	}
	if j > i+1 && src[i+1] >= '0' && src[i+1] <= '9' {
		return 0, false // $1 is a parameter
	}
	tag := src[i : j+1]
	end := strings.Index(src[j+1:], tag)
	if end < 0 {
		return len(src), true
	}
	return j + 1 + end + len(tag), true
}
