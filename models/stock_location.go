package models

import (
	"time"

	"gorm.io/gorm"
)

// StockLocation 庫點
type StockLocation struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
}
