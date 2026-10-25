package connections

import (
	"context"
	"errors"

	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
	"gorm.io/gorm"
)

// List returns every connection, ordered by name.
func (s *Service) List(ctx context.Context) ([]models.DBConn, error) {
	conns, err := gorm.G[models.DBConn](s.DB).Order("name COLLATE NOCASE").Find(ctx)
	if err != nil {
		return nil, appErrors.Error(
			"Failed to load connections.",
			"The saved connections could not be read from the local configuration.",
			err,
		)
	}
	return conns, nil
}

func (s *Service) Get(ctx context.Context, connID uint) (models.DBConn, error) {
	conn, err := gorm.G[models.DBConn](s.DB).Where("id = ?", connID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return conn, appErrors.Error("Connection not found.", "The connection wasn't found.", err)
		}
		return conn, appErrors.Error(
			"Failed to load connection.",
			"The connection could not be read from the local configuration.",
			err,
		)
	}
	return conn, nil
}
