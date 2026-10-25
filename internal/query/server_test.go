package query

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Watts-Argentina/anvil/internal/drivers"
	"github.com/Watts-Argentina/anvil/models"
)

// Type conversion against real servers (ANVIL_TEST_PG / ANVIL_TEST_MYSQL).
// timestamptz arrives in the local time zone.
func TestServerValues(t *testing.T) {
	cases := []struct {
		env, engine, sql string
		want             []string
	}{
		{"ANVIL_TEST_PG", models.EnginePostgres,
			`select 1.50::numeric, 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid, '{"a": 1}'::jsonb,
			        '\x00ff'::bytea, '2024-03-01 10:20:30.5+00'::timestamptz, '2024-03-01'::date,
			        true, null::text, ''::text, 42::bigint, array[1,2]`,
			[]string{"1.50", "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", `{"a": 1}`, "<BLOB 2 B> 00ff",
				time.Date(2024, 3, 1, 10, 20, 30, 5e8, time.UTC).Local().Format(timeLayout), "2024-03-01", "true", "NULL", "", "42", "{1,2}"}},
		{"ANVIL_TEST_MYSQL", models.EngineMySQL,
			`select cast(1.50 as decimal(5,2)), cast('{"a": 1}' as json), x'00ff',
			        timestamp('2024-03-01 10:20:30'), date('2024-03-01'), null, '', 42`,
			[]string{"1.50", `{"a": 1}`, "<BLOB 2 B> 00ff", "2024-03-01 10:20:30 +00:00", "2024-03-01", "NULL", "", "42"}},
	}
	for _, tc := range cases {
		t.Run(tc.engine, func(t *testing.T) {
			v := strings.Fields(os.Getenv(tc.env))
			if len(v) != 5 {
				t.Skipf("%s not set", tc.env)
			}
			port, _ := strconv.Atoi(v[1])
			ssl := "disable"
			if tc.engine == models.EngineMySQL {
				ssl = "preferred"
			}
			conn := models.DBConn{Engine: tc.engine, Host: v[0], Port: port, User: v[2], DB: v[4], SSLMode: ssl}
			ctx := context.Background()
			db, err := drivers.Connect(ctx, conn, v[3])
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			d, _ := drivers.For(tc.engine)

			p, err := NewPlan(d, tc.sql, false)
			if err != nil {
				t.Fatal(err)
			}
			res, err := Run(ctx, db, p, 10)
			if err != nil {
				t.Fatal(err)
			}
			row := res.Rows.Rows[0]
			for i, want := range tc.want {
				got := row[i].Text
				if row[i].Null {
					got = "NULL"
				}
				if got != want {
					t.Errorf("column %d (%s): got %q, want %q", i, res.Rows.Columns[i].DBType, got, want)
				}
			}
		})
	}
}
