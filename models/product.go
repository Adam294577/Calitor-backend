package models

import (
	"time"

	"gorm.io/gorm"
)

// Product 商品基本資料
type Product struct {
	ID                int64                `gorm:"primaryKey" json:"id"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
	DeletedAt         gorm.DeletedAt       `gorm:"index" json:"deleted_at"`
	ModelCode         string               `gorm:"type:varchar(50);uniqueIndex;not null" json:"model_code"` // 型號
	NameSpec          string               `gorm:"type:varchar(300)" json:"name_spec"`                      // 品名規格
	Currency          string               `gorm:"type:varchar(20)" json:"currency"`                        // 幣別
	ProductBrandId    *int64               `gorm:"index" json:"product_brand_id"`                           // 品牌 ID
	ProductBrand      *ProductBrand        `gorm:"foreignKey:ProductBrandId" json:"product_brand,omitempty"`
	BrandId           *int64               `gorm:"index" json:"brand_id"` // 對帳品牌 ID
	Brand             *Brand               `gorm:"foreignKey:BrandId" json:"brand,omitempty"`
	CreatedOn         *time.Time           `json:"created_on"`                                   // 建檔日（後端自動填入）
	MSRP              float64              `gorm:"type:numeric(18,2)" json:"msrp"`               // 建議售價
	SpecialPrice      float64              `gorm:"type:numeric(18,2)" json:"special_price"`      // 特價
	OriginalPrice     float64              `gorm:"type:numeric(18,2)" json:"original_price"`     // 原幣價
	Wholesale         float64              `gorm:"type:numeric(18,2)" json:"wholesale"`          // 批價（未稅）
	WholesaleTaxIncl  float64              `gorm:"type:numeric(18,2)" json:"wholesale_tax_incl"` // 批價（含稅，使用者輸入）
	WholesaleDiscount float64              `gorm:"type:numeric(5,2)" json:"wholesale_discount"`  // 批價折扣
	BillingBrand      string               `gorm:"type:varchar(100)" json:"billing_brand"`       // 對帳品牌
	TradeMode         int64                `gorm:"type:bigint;default:1" json:"trade_mode"`      // 買賣方式 1:買斷 2:寄賣
	IsVisible         bool                 `gorm:"default:true" json:"is_visible"`               // 顯示方式
	Remark            string               `gorm:"type:text" json:"remark"`                      // 備註
	MaterialOuter     string               `gorm:"type:varchar(200)" json:"material_outer"`      // 材質（外）
	MaterialInner     string               `gorm:"type:varchar(200)" json:"material_inner"`      // 內裡材質
	ToeCapTrim        string               `gorm:"type:varchar(200)" json:"toe_cap_trim"`        // 包頭包邊
	Lining            string               `gorm:"type:varchar(200)" json:"lining"`              // Lining
	Sock              string               `gorm:"type:varchar(200)" json:"sock"`                // Sock
	Sole              string               `gorm:"type:varchar(200)" json:"sole"`                // Sole
	Season            string               `gorm:"type:varchar(50)" json:"season"`               // 季別
	ImageURL          string               `gorm:"type:varchar(500)" json:"image_url"`           // 商品圖片 URL
	Size1GroupID      *int64               `gorm:"index" json:"size1_group_id"`                  // 尺碼組 1
	Size1Group        *SizeGroup           `gorm:"foreignKey:Size1GroupID" json:"size1_group,omitempty"`
	Size2GroupID      *int64               `gorm:"index" json:"size2_group_id"` // 尺碼組 2
	Size2Group        *SizeGroup           `gorm:"foreignKey:Size2GroupID" json:"size2_group,omitempty"`
	Size3GroupID      *int64               `gorm:"index" json:"size3_group_id"` // 尺碼組 3
	Size3Group        *SizeGroup           `gorm:"foreignKey:Size3GroupID" json:"size3_group,omitempty"`
	CategoryMaps      []ProductCategoryMap `gorm:"foreignKey:ProductID" json:"category_maps,omitempty"`   // 分類對應
	ProductVendors    []ProductVendor      `gorm:"foreignKey:ProductID" json:"product_vendors,omitempty"` // 廠商/價格
}
