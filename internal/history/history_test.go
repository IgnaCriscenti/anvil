package history

import (
	"context"
	"testing"

	"github.com/Watts-Argentina/anvil/internal/storage"
	"github.com/Watts-Argentina/anvil/models"
)

func TestAddListTrim(t *testing.T) {
	db, err := storage.Open("file:history_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: db}
	ctx := context.Background()

	for i := range MaxEntries + 5 {
		sql := "select 1"
		if i == MaxEntries+4 {
			sql = "select needle"
		}
		if err := s.Add(ctx, models.QueryHistory{ConnID: 1, SQL: sql}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Add(ctx, models.QueryHistory{ConnID: 2, SQL: "other"}); err != nil {
		t.Fatal(err)
	}

	all, err := s.List(ctx, 1, "", 1000)
	if err != nil || len(all) != MaxEntries {
		t.Fatalf("entries = %d, %v", len(all), err)
	}
	if all[0].SQL != "select needle" {
		t.Fatalf("newest first: %q", all[0].SQL)
	}
	found, _ := s.List(ctx, 1, "needle", 10)
	if len(found) != 1 {
		t.Fatalf("search = %d", len(found))
	}
}
