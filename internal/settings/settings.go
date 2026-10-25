// Package settings stores the local preferences (SPEC §6.3) in a single
// row of the settings table.
package settings

import (
	"context"

	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
	"gorm.io/gorm"
)

const rowID = 1

type Service struct {
	DB *gorm.DB
}

// Defaults are the values for a new install.
func Defaults() models.Settings {
	return models.Settings{ID: rowID, Theme: models.ThemeDark, DefaultPageSize: models.DefaultPageSize}
}

// Load returns the settings, with the default values for empty or
// out-of-range fields.
func (s *Service) Load(ctx context.Context) (models.Settings, error) {
	st := Defaults()
	err := s.DB.WithContext(ctx).Where(models.Settings{ID: rowID}).Attrs(st).FirstOrCreate(&st).Error
	if err != nil {
		return Defaults(), appErrors.Error(
			"Failed to load settings.",
			"The local settings could not be read; the defaults are used.",
			err,
		)
	}
	return normalize(st), nil
}

func (s *Service) Save(ctx context.Context, st models.Settings) error {
	st = normalize(st)
	st.ID = rowID
	if err := s.DB.WithContext(ctx).Save(&st).Error; err != nil {
		return appErrors.Error("Failed to save settings.", "The local settings could not be written.", err)
	}
	return nil
}

func normalize(st models.Settings) models.Settings {
	if st.Theme != models.ThemeLight {
		st.Theme = models.ThemeDark
	}
	if st.DefaultPageSize < models.MinPageSize || st.DefaultPageSize > models.MaxPageSize {
		st.DefaultPageSize = models.DefaultPageSize
	}
	return st
}
