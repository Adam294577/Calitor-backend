package models

import (
	"time"

	"gorm.io/gorm"
)

// Product 商品基本資料
type Product struct {
	ID                 int64             `gorm:"primaryKey" json:"id"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
	DeletedAt          gorm.DeletedAt    `gorm:"index" json:"deleted_at"`
	ModelCode          string            `gorm:"type:varchar(50);uniqueIndex;not null" json:"model_code"`
	CurrencyId         *int64            `gorm:"index" json:"currency_id"`
	Currency           *Currency         `gorm:"foreignKey:CurrencyId" json:"currency,omitempty"`
	NameSpec           string            `gorm:"type:varchar(300)" json:"name_spec"`
	VendorId           *int64            `gorm:"index" json:"vendor_id"`
	Vendor             *Vendor           `gorm:"foreignKey:VendorId" json:"vendor,omitempty"`
	CreatedDate        string            `gorm:"type:varchar(20)" json:"created_date"`
	MSRP               float64           `gorm:"type:decimal(12,2)" json:"msrp"`
	PurchaseDiscount   int               `gorm:"default:0" json:"purchase_discount"`
	BrandId            *int64            `gorm:"index" json:"brand_id"`
	Brand              *Brand            `gorm:"foreignKey:BrandId" json:"brand,omitempty"`
	StartPrice         float64           `gorm:"type:decimal(12,2)" json:"start_price"`
	LastPrice          float64           `gorm:"type:decimal(12,2)" json:"last_price"`
	TradeMode          string            `gorm:"type:varchar(20);default:'買斷'" json:"trade_mode"`
	WholesaleDiscount  int               `gorm:"default:0" json:"wholesale_discount"`
	WholesalePrice     float64           `gorm:"type:decimal(12,2)" json:"wholesale_price"`
	IsVisible          bool              `gorm:"default:true" json:"is_visible"`
	Note               string            `gorm:"type:text" json:"note"`
	Material           string            `gorm:"type:varchar(200)" json:"material"`
	InnerMaterial      string            `gorm:"type:varchar(200)" json:"inner_material"`
	ToeCapEdge         string            `gorm:"type:varchar(200)" json:"toe_cap_edge"`
	Lining             string            `gorm:"type:varchar(200)" json:"lining"`
	Sock               string            `gorm:"type:varchar(200)" json:"sock"`
	Sole               string            `gorm:"type:varchar(200)" json:"sole"`
	Categories         []ProductCategory `gorm:"many2many:product_product_categories" json:"categories,omitempty"`
}
