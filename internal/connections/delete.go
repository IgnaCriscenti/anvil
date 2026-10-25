package connections

import (
	"context"
	"errors"
	"fmt"

	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
	"github.com/zalando/go-keyring"
	"gorm.io/gorm"
)

// Delete removes the connection and its password from the keyring.
func (s *Service) Delete(ctx context.Context, connID uint) error {
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conn, err := gorm.G[models.DBConn](tx).Where("id = ?", connID).First(ctx)
		if err != nil {
			return err
		}

		if _, err := gorm.G[models.DBConn](tx).Where("id = ?", connID).Delete(ctx); err != nil {
			return err
		}

		err = keyring.Delete(service, keyringKey(connID))
		if err != nil && !errors.Is(err, keyring.ErrNotFound) && conn.SavePassword {
			return fmt.Errorf("delete password from keyring: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return appErrors.Error(
				"Connection not found.",
				"The connection wasn't found. It may have been already deleted.",
				err,
			)
		}

		return appErrors.Error(
			"Failed to delete connection.",
			"An error occurred while trying to delete the connection.",
			err,
		)
	}

	return nil
}
