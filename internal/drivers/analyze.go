package drivers

import "strings"

type Kind int

const (
	KindOther Kind = iota // SET, USE, …: rejected on read-only connections
	KindRead
	KindWrite // DML
	KindDDL
	KindTx // BEGIN, COMMIT, ROLLBACK, SAVEPOINT, …
)

func (k Kind) String() string {
	return [...]string{"other", "read", "write", "ddl", "tx"}[k]
}

// Statement is the result of analyzing a statement.
type Statement struct {
	Kind Kind
	// Keyword is the first keyword, upper-cased. In a data-modifying WITH it
	// is the main DML keyword, if that DML is at the top level.
	Keyword string
	// HasWhere reports a top-level WHERE (to warn about UPDATE/DELETE
	// without WHERE, SPEC §8).
	HasWhere bool
}

// ReturnsRows reports whether the statement can return rows to the client.
func (s Statement) ReturnsRows() bool {
	return s.Kind == KindRead
}

// IsReadOnly reports whether the statement is allowed on a read-only connection.
func (s Statement) IsReadOnly() bool {
	return s.Kind == KindRead || s.Kind == KindTx
}

var keywordKinds = map[string]Kind{
	"SELECT": KindRead, "SHOW": KindRead, "EXPLAIN": KindRead, "DESCRIBE": KindRead,
	"DESC": KindRead, "VALUES": KindRead, "TABLE": KindRead, "HELP": KindRead,
	"PRAGMA": KindRead, "WITH": KindRead,

	"INSERT": KindWrite, "UPDATE": KindWrite, "DELETE": KindWrite, "MERGE": KindWrite,
	"REPLACE": KindWrite, "UPSERT": KindWrite, "COPY": KindWrite, "CALL": KindWrite,
	"DO": KindWrite, "LOAD": KindWrite, "LOCK": KindWrite, "IMPORT": KindWrite,
	"EXEC": KindWrite, "EXECUTE": KindWrite,

	"CREATE": KindDDL, "ALTER": KindDDL, "DROP": KindDDL, "TRUNCATE": KindDDL,
	"RENAME": KindDDL, "GRANT": KindDDL, "REVOKE": KindDDL, "COMMENT": KindDDL,
	"VACUUM": KindDDL, "ANALYZE": KindDDL, "REINDEX": KindDDL, "CLUSTER": KindDDL,
	"REFRESH": KindDDL, "ATTACH": KindDDL, "DETACH": KindDDL, "OPTIMIZE": KindDDL,
	"REPAIR": KindDDL,

	"BEGIN": KindTx, "START": KindTx, "COMMIT": KindTx, "ROLLBACK": KindTx,
	"SAVEPOINT": KindTx, "RELEASE": KindTx, "END": KindTx, "ABORT": KindTx,
}

var dmlWords = map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true, "REPLACE": true}

func (d dialect) analyze(stmt string) Statement {
	var (
		first      string
		second     string
		depth      int
		hasWhere   bool
		hasInto    bool
		hasAnalyze bool
		hasEq      bool
		dml        string // first DML at any level
		topDML     string // first DML at the top level (after the first word)
	)

	for i := 0; i < len(stmt); {
		tok := d.lex(stmt, i)
		i = tok.end

		switch tok.kind {
		case tokPunct:
			switch stmt[tok.start] {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			case '=':
				if depth == 0 {
					hasEq = true
				}
			}
		case tokWord:
			w := strings.ToUpper(stmt[tok.start:tok.end])
			if first == "" {
				first = w
				continue
			}
			if second == "" {
				second = w
			}
			if dmlWords[w] {
				if dml == "" {
					dml = w
				}
				if depth == 0 && topDML == "" {
					topDML = w
				}
			}
			if depth == 0 {
				switch w {
				case "WHERE":
					hasWhere = true
				case "INTO":
					hasInto = true
				case "ANALYZE":
					hasAnalyze = true
				}
			}
		}
	}

	s := Statement{Kind: keywordKinds[first], Keyword: first, HasWhere: hasWhere}
	if first == "" {
		return s
	}

	switch first {
	case "WITH":
		if dml != "" {
			s.Kind = KindWrite
		}
		if topDML != "" {
			s.Keyword = topDML
		}
	case "SELECT":
		// SELECT … INTO creates a table (PG) or writes a file (MySQL).
		if hasInto {
			s.Kind = KindWrite
		}
	case "EXPLAIN":
		// EXPLAIN ANALYZE executes the statement.
		if hasAnalyze && dml != "" {
			s.Kind = KindWrite
		}
	case "PRAGMA":
		if hasEq {
			s.Kind = KindWrite
		}
	case "BEGIN", "DECLARE":
		// PL/SQL anonymous block; in T-SQL, BEGIN TRAN[SACTION] is a
		// transaction and any other BEGIN a block.
		if d.plsql || d.tsql && first == "BEGIN" && second != "TRAN" && second != "TRANSACTION" && second != "DISTRIBUTED" {
			s.Kind = KindWrite
		}
	case "REPLACE":
		// MySQL: REPLACE INTO is DML; PG: CREATE OR REPLACE does not start here.
		s.Kind = KindWrite
	}
	return s
}
