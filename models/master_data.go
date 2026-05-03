package models

import (
	"time"

	"gorm.io/gorm"
)

// ProductBrand 品牌
type ProductBrand struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
}

// Brand 對帳品牌
type Brand struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
}

// Location 地理位置
type Location struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
}

// TWPostalArea 郵遞區號
type TWPostalArea struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(10);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
}

// MemberTier 會員卡別
type MemberTier struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code      string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
}

// VendorCategory 廠商類別
type VendorCategory struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
}

// Currency 幣別
type Currency struct {
	ID           int64          `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code         string         `gorm:"type:varchar(10);uniqueIndex;not null" json:"code"`
	Name         string         `gorm:"type:varchar(50);not null" json:"name"`
	Symbol       string         `gorm:"type:varchar(10)" json:"symbol"`
	ExchangeRate float64        `gorm:"type:numeric(10,4);default:0" json:"exchange_rate"`
	IsActive     bool           `gorm:"default:true" json:"is_active"`
}
