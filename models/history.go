package models

import "gorm.io/gorm"

type QueryHistory struct {
	gorm.Model
	ConnID       uint   `json:"connId" gorm:"index"`
	SQL          string `json:"sql"`
	Duration     int64  `json:"duration"` // milliseconds
	RowsAffected int64  `json:"rowsAffected"`
	Error        string `json:"error"`
}
