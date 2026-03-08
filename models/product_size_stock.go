package models

import "time"

// ProductSizeStock 商品尺碼庫存
type ProductSizeStock struct {
	ID              int64          `gorm:"primaryKey" json:"id"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	ProductID       int64          `gorm:"not null;uniqueIndex:idx_product_location_size" json:"product_id"`
	StockLocationID int64          `gorm:"not null;uniqueIndex:idx_product_location_size" json:"stock_location_id"`
	StockLocation   *StockLocation `gorm:"foreignKey:StockLocationID" json:"stock_location,omitempty"`
	SizeOptionID    int64          `gorm:"not null;uniqueIndex:idx_product_location_size" json:"size_option_id"`
	SizeOption      *SizeOption    `gorm:"foreignKey:SizeOptionID" json:"size_option,omitempty"`
	Qty             int            `gorm:"default:0" json:"qty"`
}
