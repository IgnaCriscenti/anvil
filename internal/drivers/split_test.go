package drivers

import (
	"reflect"
	"testing"
)

func texts(spans []Span) []string {
	out := []string{}
	for _, s := range spans {
		out = append(out, s.Text)
	}
	return out
}

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		name string
		d    Driver
		sql  string
		want []string
	}{
		{"simple", SQLite{}, "select 1; select 2;", []string{"select 1", "select 2"}},
		{"no trailing delimiter", Postgres{}, "select 1;\n  select 2  ", []string{"select 1", "select 2"}},
		{"empty and comment only", Postgres{}, " ; -- nada\n; /* x */ ;select 1", []string{"select 1"}},
		{"string with delimiter", SQLite{}, "select 'a;b''c'; select 2", []string{"select 'a;b''c'", "select 2"}},
		{"quoted ident", Postgres{}, `select "a;b" from t; select 2`, []string{`select "a;b" from t`, "select 2"}},
		{"line comment", Postgres{}, "select 1 -- ; no\n, 2; select 3", []string{"select 1 -- ; no\n, 2", "select 3"}},
		{"leading comment skipped", SQLite{}, "-- hola\nselect 1;", []string{"select 1"}},
		{"block comment", SQLite{}, "select /* ; */ 1; select 2", []string{"select /* ; */ 1", "select 2"}},
		{"pg nested comment", Postgres{}, "select /* a /* ; */ ; */ 1; select 2", []string{"select /* a /* ; */ ; */ 1", "select 2"}},
		{"pg dollar quote", Postgres{},
			"create function f() returns int as $$ begin; return 1; end $$ language plpgsql; select 2",
			[]string{"create function f() returns int as $$ begin; return 1; end $$ language plpgsql", "select 2"}},
		{"pg tagged dollar quote", Postgres{},
			"do $body$ begin perform 1; end $body$; select $1",
			[]string{"do $body$ begin perform 1; end $body$", "select $1"}},
		{"pg E string", Postgres{}, `select E'it\'s; ok'; select 2`, []string{`select E'it\'s; ok'`, "select 2"}},
		{"pg standard string backslash", Postgres{}, `select 'a\'; select 2`, []string{`select 'a\'`, "select 2"}},
		{"mysql backslash string", MySQL{}, `select 'it\'s; ok'; select 2`, []string{`select 'it\'s; ok'`, "select 2"}},
		{"mysql double quoted string", MySQL{}, `select "a;\"b"; select 2`, []string{`select "a;\"b"`, "select 2"}},
		{"mysql backtick", MySQL{}, "select `a;b` from t; select 2", []string{"select `a;b` from t", "select 2"}},
		{"mysql hash comment", MySQL{}, "select 1 # ; x\n; select 2", []string{"select 1", "select 2"}},
		{"mysql dash without space", MySQL{}, "select 1--1; select 2", []string{"select 1--1", "select 2"}},
		{"mysql exec comment kept", MySQL{}, "/*!40101 SET NAMES utf8 */; select 1", []string{"/*!40101 SET NAMES utf8 */", "select 1"}},
		{"mysql delimiter", MySQL{},
			"DELIMITER //\nCREATE PROCEDURE p() BEGIN SELECT 1; SELECT 2; END //\nDELIMITER ;\nselect 3;",
			[]string{"CREATE PROCEDURE p() BEGIN SELECT 1; SELECT 2; END", "select 3"}},
		{"sqlite trigger", SQLite{},
			"CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET x = CASE WHEN 1 THEN 2 END; DELETE FROM c; END; select 1",
			[]string{"CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET x = CASE WHEN 1 THEN 2 END; DELETE FROM c; END", "select 1"}},
		{"sqlite temp trigger", SQLite{},
			"create temp trigger t before delete on a begin select 1; end;begin;commit",
			[]string{"create temp trigger t before delete on a begin select 1; end", "begin", "commit"}},
		{"unterminated string", SQLite{}, "select 'abc; select 2", []string{"select 'abc; select 2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := texts(tc.d.SplitStatements(tc.sql))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestSplitOffsets(t *testing.T) {
	sql := "select 1;\n\nselect 2"
	for _, s := range (Postgres{}).SplitStatements(sql) {
		if sql[s.Start:s.End] != s.Text {
			t.Fatalf("span %+v does not match source", s)
		}
	}
}

func TestStatementAt(t *testing.T) {
	sql := "select 1;\nselect 2;\n"
	spans := (SQLite{}).SplitStatements(sql)
	cases := []struct {
		offset int
		want   string
	}{
		{0, "select 1"},
		{4, "select 1"},
		{9, "select 1"}, // right after the ;
		{10, "select 2"},
		{len(sql), "select 2"},
	}
	for _, tc := range cases {
		s, ok := StatementAt(spans, tc.offset)
		if !ok || s.Text != tc.want {
			t.Errorf("offset %d: got %q", tc.offset, s.Text)
		}
	}
	if _, ok := StatementAt(nil, 0); ok {
		t.Error("expected no statement")
	}
}

func TestAnalyze(t *testing.T) {
	cases := []struct {
		d        Driver
		sql      string
		kind     Kind
		keyword  string
		hasWhere bool
	}{
		{Postgres{}, "select * from t where a = 1", KindRead, "SELECT", true},
		{Postgres{}, "/* c */ -- x\n (select 1)", KindRead, "SELECT", false},
		{Postgres{}, "select * into t2 from t", KindWrite, "SELECT", false},
		{Postgres{}, "update t set a = 1", KindWrite, "UPDATE", false},
		{Postgres{}, "update t set a = (select 1 from x where y)", KindWrite, "UPDATE", false},
		{Postgres{}, "delete from t where id = 1", KindWrite, "DELETE", true},
		{Postgres{}, "with x as (select 1) select * from x", KindRead, "WITH", false},
		{Postgres{}, "with x as (delete from t returning *) select * from x", KindWrite, "WITH", false},
		{Postgres{}, "with x as (select 1) delete from t", KindWrite, "DELETE", false},
		{Postgres{}, "explain select 1", KindRead, "EXPLAIN", false},
		{Postgres{}, "explain analyze delete from t", KindWrite, "EXPLAIN", false},
		{Postgres{}, "select 'delete' from t", KindRead, "SELECT", false},
		{Postgres{}, "begin", KindTx, "BEGIN", false},
		{Postgres{}, "set default_transaction_read_only = off", KindOther, "SET", false},
		{Postgres{}, "create table t (a int)", KindDDL, "CREATE", false},
		{SQLite{}, "pragma table_info(t)", KindRead, "PRAGMA", false},
		{SQLite{}, "pragma foreign_keys = off", KindWrite, "PRAGMA", false},
		{MySQL{}, "replace into t values (1)", KindWrite, "REPLACE", false},
		{MySQL{}, "show tables", KindRead, "SHOW", false},
		{MySQL{}, "# c\nselect 1", KindRead, "SELECT", false},
		{MySQL{}, "start transaction", KindTx, "START", false},
		{MySQL{}, "", KindOther, "", false},
	}
	for _, tc := range cases {
		s := tc.d.Analyze(tc.sql)
		if s.Kind != tc.kind || s.Keyword != tc.keyword || s.HasWhere != tc.hasWhere {
			t.Errorf("%q: got %+v (kind %s)", tc.sql, s, s.Kind)
		}
	}
}

func TestQuoteAndPlaceholder(t *testing.T) {
	if got := (Postgres{}).QuoteIdent(`a"b`); got != `"a""b"` {
		t.Error(got)
	}
	if got := (MySQL{}).QuoteIdent("a`b"); got != "`a``b`" {
		t.Error(got)
	}
	if got := QualifiedName(Postgres{}, Table{Schema: "public", Name: "t"}); got != `"public"."t"` {
		t.Error(got)
	}
	if (Postgres{}).Placeholder(3) != "$3" || (MySQL{}).Placeholder(3) != "?" {
		t.Error("placeholder")
	}
}
