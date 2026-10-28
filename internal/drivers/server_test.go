package drivers

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Watts-Argentina/anvil/models"
)

// Tests against real servers. They are skipped unless these variables are set:
//
//	ANVIL_TEST_PG="host port user password database"
//	ANVIL_TEST_MYSQL="host port user password database"
//	ANVIL_TEST_MSSQL="host port user password database"
//	ANVIL_TEST_ORACLE="host port user password service"
func serverConn(t *testing.T, env, engine string) (models.DBConn, string) {
	t.Helper()
	v := os.Getenv(env)
	if v == "" {
		t.Skipf("%s not set", env)
	}
	f := strings.Fields(v)
	if len(f) != 5 {
		t.Fatalf("%s: expected 5 fields, got %q", env, v)
	}
	port, err := strconv.Atoi(f[1])
	if err != nil {
		t.Fatalf("%s: %v", env, err)
	}
	host, user, pass, db := f[0], f[2], f[3], f[4]
	return models.DBConn{Name: engine, Engine: engine, Host: host, Port: port, User: user, DB: db, SSLMode: sslOff(engine)}, pass
}

func sslOff(engine string) string {
	switch engine {
	case models.EngineMySQL:
		return "preferred"
	case models.EngineMSSQL:
		return "false"
	}
	return "disable"
}

func execAll(t *testing.T, db *sql.DB, d Driver, ddl string) {
	t.Helper()
	for _, s := range d.SplitStatements(ddl) {
		if _, err := db.Exec(s.Text); err != nil {
			t.Fatalf("%s: %v", s.Text, err)
		}
	}
}

func TestPostgresServer(t *testing.T) {
	ctx := context.Background()
	conn, pass := serverConn(t, "ANVIL_TEST_PG", models.EnginePostgres)
	db, err := Connect(ctx, conn, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	d := Postgres{}
	execAll(t, db, d, `
		drop schema if exists anvil_test cascade;
		create schema anvil_test;
		create table anvil_test.users (id int generated always as identity primary key, name text not null, data jsonb);
		create table anvil_test.tags (user_id int, tag text, primary key (tag, user_id));
		create table anvil_test.legacy (id serial primary key, owner int references anvil_test.users);
		create view anvil_test.v as select 1 as x;
		create materialized view anvil_test.mv as select 1 as x;`)
	defer db.Exec("drop schema anvil_test cascade")

	dbs, err := d.ListDatabases(ctx, db)
	if err != nil || !slices.Contains(dbs, conn.DB) || slices.Contains(dbs, "template0") {
		t.Fatalf("databases = %v, %v", dbs, err)
	}

	tables, err := d.ListTables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var got []Table
	for _, tb := range tables {
		if tb.Schema == "anvil_test" {
			got = append(got, tb)
		}
	}
	want := []Table{
		{"anvil_test", "legacy", false}, {"anvil_test", "mv", true}, {"anvil_test", "tags", false},
		{"anvil_test", "users", false}, {"anvil_test", "v", true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tables = %+v", got)
	}

	cols, err := d.Columns(ctx, db, Table{Schema: "anvil_test", Name: "users"})
	if err != nil {
		t.Fatal(err)
	}
	wantCols := []Column{
		{Name: "id", DBType: "integer", PK: true, PKOrdinal: 1, AutoInc: true},
		{Name: "name", DBType: "text"},
		{Name: "data", DBType: "jsonb", Nullable: true},
	}
	if !reflect.DeepEqual(cols, wantCols) {
		t.Fatalf("users = %+v", cols)
	}
	cols, _ = d.Columns(ctx, db, Table{Schema: "anvil_test", Name: "tags"})
	if cols[0].PKOrdinal != 2 || cols[1].PKOrdinal != 1 {
		t.Fatalf("tags = %+v", cols)
	}
	cols, _ = d.Columns(ctx, db, Table{Schema: "anvil_test", Name: "legacy"})
	if !cols[0].AutoInc {
		t.Fatalf("serial not autoinc: %+v", cols)
	}
	fks, err := d.ForeignKeys(ctx, db, Table{Schema: "anvil_test", Name: "legacy"})
	wantFKs := []ForeignKey{{Name: "legacy_owner_fkey", Columns: []string{"owner"},
		RefTable: Table{"anvil_test", "users", false}, RefColumns: []string{"id"}}}
	if err != nil || !reflect.DeepEqual(fks, wantFKs) {
		t.Fatalf("legacy foreign keys = %+v, %v", fks, err)
	}

	// Session-level read-only.
	conn.ReadOnly = true
	ro, err := Connect(ctx, conn, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if _, err := ro.Exec("create table anvil_test.nope (a int)"); err == nil {
		t.Fatal("expected read-only error")
	}
}

func TestMySQLServer(t *testing.T) {
	ctx := context.Background()
	conn, pass := serverConn(t, "ANVIL_TEST_MYSQL", models.EngineMySQL)
	db, err := Connect(ctx, conn, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	d := MySQL{}
	execAll(t, db, d, `
		drop table if exists users, tags;
		drop view if exists v;
		create table users (id int auto_increment primary key, name varchar(50) not null, note text);
		create table tags (user_id int, tag varchar(20), primary key (tag, user_id),
			constraint tags_user foreign key (user_id) references users (id));
		create view v as select 1 as x;`)

	dbs, err := d.ListDatabases(ctx, db)
	if err != nil || !slices.Contains(dbs, conn.DB) || slices.Contains(dbs, "mysql") {
		t.Fatalf("databases = %v, %v", dbs, err)
	}

	tables, err := d.ListTables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	// Other packages' server tests may have tables in the same database.
	tables = slices.DeleteFunc(tables, func(tb Table) bool { return !slices.Contains([]string{"tags", "users", "v"}, tb.Name) })
	want := []Table{{conn.DB, "tags", false}, {conn.DB, "users", false}, {conn.DB, "v", true}}
	if !reflect.DeepEqual(tables, want) {
		t.Fatalf("tables = %+v", tables)
	}

	cols, err := d.Columns(ctx, db, Table{Schema: conn.DB, Name: "users"})
	if err != nil {
		t.Fatal(err)
	}
	wantCols := []Column{
		{Name: "id", DBType: "int", PK: true, PKOrdinal: 1, AutoInc: true},
		{Name: "name", DBType: "varchar(50)"},
		{Name: "note", DBType: "text", Nullable: true},
	}
	if !reflect.DeepEqual(cols, wantCols) {
		t.Fatalf("users = %+v", cols)
	}
	cols, _ = d.Columns(ctx, db, Table{Name: "tags"})
	if cols[0].PKOrdinal != 2 || cols[1].PKOrdinal != 1 {
		t.Fatalf("tags = %+v", cols)
	}
	fks, err := d.ForeignKeys(ctx, db, Table{Name: "tags"})
	wantFKs := []ForeignKey{{Name: "tags_user", Columns: []string{"user_id"},
		RefTable: Table{conn.DB, "users", false}, RefColumns: []string{"id"}}}
	if err != nil || !reflect.DeepEqual(fks, wantFKs) {
		t.Fatalf("tags foreign keys = %+v, %v", fks, err)
	}

	conn.ReadOnly = true
	ro, err := Connect(ctx, conn, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if _, err := ro.Exec("insert into users (name) values ('x')"); err == nil {
		t.Fatal("expected read-only error")
	}
}

func TestMSSQLServer(t *testing.T) {
	ctx := context.Background()
	conn, pass := serverConn(t, "ANVIL_TEST_MSSQL", models.EngineMSSQL)
	db, err := Connect(ctx, conn, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	d := MSSQL{}
	execAll(t, db, d, `
		drop view if exists anvil_v;
		drop table if exists anvil_tags;
		drop table if exists anvil_users;
		create table anvil_users (id int identity primary key, name nvarchar(50) not null, note nvarchar(max));
		create table anvil_tags (user_id int, tag varchar(20), primary key (tag, user_id),
			constraint anvil_tags_user foreign key (user_id) references anvil_users (id));
		GO
		create view anvil_v as select 1 as x;
		GO`)
	defer execAll(t, db, d, "drop view anvil_v; drop table anvil_tags; drop table anvil_users")

	dbs, err := d.ListDatabases(ctx, db)
	if err != nil || !slices.Contains(dbs, conn.DB) || slices.Contains(dbs, "master") {
		t.Fatalf("databases = %v, %v", dbs, err)
	}

	tables, err := d.ListTables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	tables = slices.DeleteFunc(tables, func(tb Table) bool { return !strings.HasPrefix(tb.Name, "anvil_") })
	want := []Table{{"dbo", "anvil_tags", false}, {"dbo", "anvil_users", false}, {"dbo", "anvil_v", true}}
	if !reflect.DeepEqual(tables, want) {
		t.Fatalf("tables = %+v", tables)
	}

	cols, err := d.Columns(ctx, db, Table{Schema: "dbo", Name: "anvil_users"})
	if err != nil {
		t.Fatal(err)
	}
	wantCols := []Column{
		{Name: "id", DBType: "int", PK: true, PKOrdinal: 1, AutoInc: true},
		{Name: "name", DBType: "nvarchar"},
		{Name: "note", DBType: "nvarchar", Nullable: true},
	}
	if !reflect.DeepEqual(cols, wantCols) {
		t.Fatalf("users = %+v", cols)
	}
	cols, _ = d.Columns(ctx, db, Table{Name: "anvil_tags"})
	if len(cols) != 2 || cols[0].PKOrdinal != 2 || cols[1].PKOrdinal != 1 {
		t.Fatalf("tags = %+v", cols)
	}
	fks, err := d.ForeignKeys(ctx, db, Table{Name: "anvil_tags"})
	wantFKs := []ForeignKey{{Name: "anvil_tags_user", Columns: []string{"user_id"},
		RefTable: Table{"dbo", "anvil_users", false}, RefColumns: []string{"id"}}}
	if err != nil || !reflect.DeepEqual(fks, wantFKs) {
		t.Fatalf("tags foreign keys = %+v, %v", fks, err)
	}
}

func TestOracleServer(t *testing.T) {
	ctx := context.Background()
	conn, pass := serverConn(t, "ANVIL_TEST_ORACLE", models.EngineOracle)
	db, err := Connect(ctx, conn, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	d := Oracle{}
	execAll(t, db, d, `
		begin
		  for o in (select object_name, object_type from user_objects
		            where object_name like 'ANVIL\_%' escape '\' and object_type in ('TABLE', 'VIEW')) loop
		    execute immediate 'drop ' || o.object_type || ' "' || o.object_name || '" cascade constraints';
		  end loop;
		end;
		/
		create table anvil_users (id number generated by default as identity primary key,
			name varchar2(50) not null, note clob);
		create table anvil_tags (user_id number, tag varchar2(20), primary key (tag, user_id),
			constraint anvil_tags_user foreign key (user_id) references anvil_users (id));
		create view anvil_v as select 1 as x from dual;`)
	defer execAll(t, db, d, "drop view anvil_v; drop table anvil_tags; drop table anvil_users")

	if dbs, err := d.ListDatabases(ctx, db); err != nil || dbs != nil {
		t.Fatalf("databases = %v, %v", dbs, err)
	}

	tables, err := d.ListTables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	tables = slices.DeleteFunc(tables, func(tb Table) bool { return !strings.HasPrefix(tb.Name, "ANVIL_") })
	owner := strings.ToUpper(conn.User)
	want := []Table{{owner, "ANVIL_TAGS", false}, {owner, "ANVIL_USERS", false}, {owner, "ANVIL_V", true}}
	if !reflect.DeepEqual(tables, want) {
		t.Fatalf("tables = %+v", tables)
	}

	cols, err := d.Columns(ctx, db, Table{Name: "ANVIL_USERS"})
	if err != nil {
		t.Fatal(err)
	}
	wantCols := []Column{
		{Name: "ID", DBType: "NUMBER", PK: true, PKOrdinal: 1, AutoInc: true},
		{Name: "NAME", DBType: "VARCHAR2"},
		{Name: "NOTE", DBType: "CLOB", Nullable: true},
	}
	if !reflect.DeepEqual(cols, wantCols) {
		t.Fatalf("users = %+v", cols)
	}
	cols, _ = d.Columns(ctx, db, Table{Schema: owner, Name: "ANVIL_TAGS"})
	if len(cols) != 2 || cols[0].PKOrdinal != 2 || cols[1].PKOrdinal != 1 {
		t.Fatalf("tags = %+v", cols)
	}
	fks, err := d.ForeignKeys(ctx, db, Table{Name: "ANVIL_TAGS"})
	wantFKs := []ForeignKey{{Name: "ANVIL_TAGS_USER", Columns: []string{"USER_ID"},
		RefTable: Table{owner, "ANVIL_USERS", false}, RefColumns: []string{"ID"}}}
	if err != nil || !reflect.DeepEqual(fks, wantFKs) {
		t.Fatalf("tags foreign keys = %+v, %v", fks, err)
	}
}
