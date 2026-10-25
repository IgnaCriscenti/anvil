package connections

import (
	"context"

	"github.com/Watts-Argentina/anvil/models"
	"github.com/Watts-Argentina/anvil/pkg/appErrors"
	"github.com/zalando/go-keyring"
	"gorm.io/gorm"
)

// SaveResult is the result of Save. Warning is not nil when the connection
// was saved but the password could not be stored in the keyring (SPEC §3.2):
// in that case Conn.SavePassword is false and the user must be warned.
type SaveResult struct {
	Conn    models.DBConn
	Warning error
}

// Save creates (ID == 0) or updates the connection. When editing, an empty
// password with SavePassword keeps the one already in the keyring.
func (s *Service) Save(ctx context.Context, conn models.DBConn, password string) (SaveResult, error) {
	if err := Validate(conn); err != nil {
		return SaveResult{}, err
	}
	if conn.Engine == models.EngineSQLite {
		conn.Host, conn.User, conn.Port, conn.SavePassword = "", "", 0, false
	}

	var warning error
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if conn.ID == 0 {
			if err := gorm.G[models.DBConn](tx).Create(ctx, &conn); err != nil {
				return err
			}
		} else if err := tx.Save(&conn).Error; err != nil {
			return err
		}

		key := keyringKey(conn.ID)
		if !conn.SavePassword {
			// Best effort: if there is no entry or the keyring is unavailable,
			// there is nothing to delete.
			_ = keyring.Delete(service, key)
			return nil
		}
		if password == "" {
			return nil
		}

		if err := keyring.Set(service, key, password); err != nil {
			// Fallback: the connection is saved anyway, without a stored password.
			conn.SavePassword = false
			warning = appErrors.Error(
				"Password not saved.",
				"The system password manager is not available. The password will be asked every time you connect.",
				err,
			)
			return tx.Model(&conn).Update("save_password", false).Error
		}
		return nil
	})
	if err != nil {
		return SaveResult{}, appErrors.Error(
			"Failed to save database connection.",
			"An unknown error occurred while saving the connection.",
			err,
		)
	}

	return SaveResult{Conn: conn, Warning: warning}, nil
}
