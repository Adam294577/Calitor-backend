package models

import (
	"time"

	"gorm.io/gorm"
)

// Order 客戶訂貨單主表
type Order struct {
	ID             int64           `gorm:"primaryKey" json:"id"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	DeletedAt      gorm.DeletedAt  `gorm:"index" json:"deleted_at"`
	OrderNo        string          `gorm:"type:varchar(50);uniqueIndex;not null" json:"order_no"`
	OrderDate      string          `gorm:"type:varchar(20);not null;index" json:"order_date"`
	CustomerID     int64           `gorm:"not null;index" json:"customer_id"`
	Customer       *RetailCustomer `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	FillPersonID   *int64          `gorm:"index" json:"fill_person_id"`
	FillPerson     *Admin          `gorm:"foreignKey:FillPersonID" json:"fill_person,omitempty"`
	SalesmanID     *int64          `gorm:"index" json:"salesman_id"`
	Salesman       *Admin          `gorm:"foreignKey:SalesmanID" json:"salesman,omitempty"`
	RecorderID     int64           `gorm:"not null;index" json:"recorder_id"`
	Recorder       *Admin          `gorm:"foreignKey:RecorderID" json:"recorder,omitempty"`
	DealMode       int             `gorm:"default:1" json:"deal_mode"`                         // 1=買斷 2=寄賣
	ClientOrderID  string          `gorm:"type:varchar(50)" json:"client_order_id"`            // 客戶單號
	BrandID        *int64          `gorm:"index" json:"brand_id"`                              // 對帳品牌 ID
	Brand          *Brand          `gorm:"foreignKey:BrandID" json:"brand,omitempty"`
	OrderStore     string          `gorm:"type:varchar(20)" json:"order_store"`                // 訂貨庫點
	Remark         string          `gorm:"type:text" json:"remark"`
	TaxMode        int             `gorm:"default:2" json:"tax_mode"`                          // 1=含稅 2=應稅
	TaxRate        float64         `gorm:"type:numeric(5,2);default:5" json:"tax_rate"`
	DeliveryStatus int             `gorm:"default:0" json:"delivery_status"`                   // 0=未交 1=部分交貨 2=已交齊
	IsStopped      bool            `gorm:"default:false" json:"is_stopped"`                   // 停貨標記
	Items          []OrderItem     `gorm:"foreignKey:OrderID" json:"items,omitempty"`
}

// OrderItem 客戶訂貨明細行（每個商品一列）
type OrderItem struct {
	ID            int64           `gorm:"primaryKey" json:"id"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	OrderID       int64           `gorm:"not null;index" json:"order_id"`
	ProductID     int64           `gorm:"not null;index" json:"product_id"`
	Product       *Product        `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	SizeGroupID   *int64          `gorm:"index" json:"size_group_id"`
	SizeGroup     *SizeGroup      `gorm:"foreignKey:SizeGroupID" json:"size_group,omitempty"`
	ItemOrder     int             `gorm:"default:0" json:"item_order"`
	AdvicePrice   float64         `gorm:"type:numeric(18,2)" json:"advice_price"`              // 訂價
	Discount      float64         `gorm:"type:numeric(5,2);default:0" json:"discount"`         // 折扣
	OrderPrice    float64         `gorm:"type:numeric(18,2)" json:"order_price"`               // 單價
	NonTaxPrice   float64         `gorm:"type:numeric(18,2)" json:"non_tax_price"`
	TotalQty      int             `gorm:"default:0" json:"total_qty"`
	TotalAmount   float64         `gorm:"type:numeric(18,2);default:0" json:"total_amount"`
	Supplement    int             `gorm:"type:integer;default:0" json:"supplement"`             // 0:空 1:舖 2:補 3:停
	ExpectedDate  string          `gorm:"type:varchar(20)" json:"expected_date"`               // 預交日
	ClientGoodID  string          `gorm:"type:varchar(50)" json:"client_good_id"`              // 客戶貨號
	CancelFlag    int             `gorm:"type:integer;default:1" json:"cancel_flag"`           // 1:正常 2:清除
	Sizes         []OrderItemSize `gorm:"foreignKey:OrderItemID" json:"sizes,omitempty"`
}

// OrderItemSize 客戶訂貨明細尺碼數量
type OrderItemSize struct {
	ID           int64       `gorm:"primaryKey" json:"id"`
	OrderItemID  int64       `gorm:"not null;uniqueIndex:idx_order_item_size" json:"order_item_id"`
	SizeOptionID int64       `gorm:"not null;uniqueIndex:idx_order_item_size" json:"size_option_id"`
	SizeOption   *SizeOption `gorm:"foreignKey:SizeOptionID" json:"size_option,omitempty"`
	Qty          int         `gorm:"default:0" json:"qty"`
}
