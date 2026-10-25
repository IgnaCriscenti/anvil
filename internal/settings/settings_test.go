package settings

import (
	"context"
	"testing"

	"github.com/Watts-Argentina/anvil/internal/storage"
	"github.com/Watts-Argentina/anvil/models"
)

func TestLoadSave(t *testing.T) {
	db, err := storage.Open("file:settings_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: db}
	ctx := context.Background()

	st, err := s.Load(ctx)
	if err != nil || st.Theme != models.ThemeDark || st.DefaultPageSize != 100 {
		t.Fatalf("defaults = %+v, %v", st, err)
	}

	st.Theme = models.ThemeLight
	st.DefaultPageSize = 5000 // out of range
	if err := s.Save(ctx, st); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Load(ctx)
	if st.Theme != models.ThemeLight || st.DefaultPageSize != 100 {
		t.Fatalf("loaded = %+v", st)
	}
}
