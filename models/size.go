package models

import (
	"time"

	"gorm.io/gorm"
)

// SizeGroup 尺碼組別
type SizeGroup struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(20);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
	Options   []SizeOption   `gorm:"foreignKey:SizeGroupID" json:"options,omitempty"`
}

// SizeOption 尺碼選項
type SizeOption struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	SizeGroupID int64      `gorm:"not null;index" json:"size_group_id"`
	SizeGroup   *SizeGroup `gorm:"foreignKey:SizeGroupID" json:"size_group,omitempty"`
	Code        string     `gorm:"type:varchar(20);not null" json:"code"`
	Label       string     `gorm:"type:varchar(50);not null" json:"label"`
	SortOrder   int        `gorm:"default:0" json:"sort_order"`
}
