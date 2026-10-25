package connections

import (
	"context"
	"errors"

	"github.com/Watts-Argentina/anvil/pkg/appErrors"
	"github.com/zalando/go-keyring"
)

// ErrPasswordNotFound means there is no stored password: it must be asked for.
var ErrPasswordNotFound = errors.New("connection password not found")

// GetPassword reads the password from the keyring. It may block (OS unlock
// prompt): do not call it from the UI thread.
func (s *Service) GetPassword(ctx context.Context, connID uint) (string, error) {
	p, err := keyring.Get(service, keyringKey(connID))
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", appErrors.Error(
				"Connection password not found.",
				"The connection password does not exist or cannot be found. Enter it to connect.",
				ErrPasswordNotFound,
			)
		}

		return "", appErrors.Error(
			"Failed to obtain connection password.",
			"The system password manager failed to return the password. Enter it to connect.",
			err,
		)
	}

	return p, nil
}
