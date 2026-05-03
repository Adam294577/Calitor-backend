package models

import "time"

// ProductVendor 商品廠商關聯（一對多）
type ProductVendor struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ProductID     int64     `gorm:"not null;uniqueIndex:idx_product_vendor" json:"product_id"`
	VendorID      int64     `gorm:"not null;uniqueIndex:idx_product_vendor;index" json:"vendor_id"`
	Vendor        *Vendor   `gorm:"foreignKey:VendorID" json:"vendor,omitempty"`
	CostDiscount  float64   `gorm:"type:numeric(5,2)" json:"cost_discount"`
	CostStart     float64   `gorm:"type:numeric(18,2)" json:"cost_start"`
	CostLast      float64   `gorm:"type:numeric(18,2)" json:"cost_last"`
	OriginalPrice float64   `gorm:"type:numeric(18,2)" json:"original_price"` // 原幣價，使用者備註參考用，不影響進貨流程
	IsPrimary     bool      `gorm:"default:false" json:"is_primary"`
}
