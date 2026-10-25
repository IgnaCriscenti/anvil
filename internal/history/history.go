// Package history stores the per-connection query history (SPEC §5.1).
package history

import (
	"context"

	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
	"gorm.io/gorm"
)

// MaxEntries is the number of entries kept per connection.
const MaxEntries = 500

type Service struct {
	DB *gorm.DB
}

// Add stores an execution and drops the connection's oldest entries.
func (s *Service) Add(ctx context.Context, h models.QueryHistory) error {
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[models.QueryHistory](tx).Create(ctx, &h); err != nil {
			return err
		}
		keep := tx.Model(&models.QueryHistory{}).Select("id").
			Where("conn_id = ?", h.ConnID).Order("id DESC").Limit(MaxEntries)
		return tx.Unscoped().Where("conn_id = ? AND id NOT IN (?)", h.ConnID, keep).
			Delete(&models.QueryHistory{}).Error
	})
	if err != nil {
		return appErrors.Error("Failed to save query history.", "The query was executed but could not be added to the history.", err)
	}
	return nil
}

// List returns the connection's latest entries, newest first,
// optionally filtered by text.
func (s *Service) List(ctx context.Context, connID uint, search string, limit int) ([]models.QueryHistory, error) {
	q := gorm.G[models.QueryHistory](s.DB).Where("conn_id = ?", connID)
	if search != "" {
		q = q.Where("sql LIKE ?", "%"+search+"%")
	}
	entries, err := q.Order("id DESC").Limit(limit).Find(ctx)
	if err != nil {
		return nil, appErrors.Error("Failed to load query history.", "The query history could not be read.", err)
	}
	return entries, nil
}
