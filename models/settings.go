package models

const (
	ThemeDark  = "dark"
	ThemeLight = "light"

	DefaultPageSize = 100
	MinPageSize     = 20
	MaxPageSize     = 500
)

// Settings is a single row (ID = 1).
type Settings struct {
	ID              uint   `json:"-" gorm:"primaryKey"`
	Theme           string `json:"theme"`
	DefaultPageSize int    `json:"defaultPageSize"`
}
