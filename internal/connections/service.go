package connections

import (
	"strconv"

	"gorm.io/gorm"
)

type Service struct {
	DB *gorm.DB
}

const service = "ANVIL-DBM"

// keyringKey is the connection's key in the OS keyring.
func keyringKey(connID uint) string {
	return strconv.FormatUint(uint64(connID), 10)
}
