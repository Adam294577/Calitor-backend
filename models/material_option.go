package models

import (
	"time"

	"gorm.io/gorm"
)

// MaterialOption 材質選項
// Kind 值: toe_cap_trim, lining, sock, sole
type MaterialOption struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Kind      string         `gorm:"type:varchar(30);not null;index" json:"kind"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
}
