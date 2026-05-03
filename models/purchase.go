package models

import (
	"time"

	"gorm.io/gorm"
)

// Purchase 採購單主表
type Purchase struct {
	ID               int64           `gorm:"primaryKey" json:"id"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	DeletedAt        gorm.DeletedAt  `gorm:"index" json:"deleted_at"`
	PurchaseNo       string          `gorm:"type:varchar(50);uniqueIndex;not null" json:"purchase_no"`
	PurchaseDate     string          `gorm:"type:varchar(20);not null;index" json:"purchase_date"`
	CustomerID       int64           `gorm:"not null;index" json:"customer_id"`
	Customer         *RetailCustomer `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	VendorID         int64           `gorm:"not null;index" json:"vendor_id"`
	Vendor           *Vendor         `gorm:"foreignKey:VendorID" json:"vendor,omitempty"`
	FillPersonID     *int64          `gorm:"index" json:"fill_person_id"`
	FillPerson       *Admin          `gorm:"foreignKey:FillPersonID" json:"fill_person,omitempty"`
	RecorderID       int64           `gorm:"not null;index" json:"recorder_id"`
	Recorder         *Admin          `gorm:"foreignKey:RecorderID" json:"recorder,omitempty"`
	DealMode         int             `gorm:"default:1" json:"deal_mode"`
	CurrencyCode     string          `gorm:"type:varchar(20)" json:"currency_code"` // 幣別 (RMB/TWD)
	ConfirmationDate string          `gorm:"type:varchar(20)" json:"confirmation_date"`
	Remark           string          `gorm:"type:text" json:"remark"`
	TaxMode          int             `gorm:"default:2" json:"tax_mode"`
	TaxRate          float64         `gorm:"type:numeric(5,2);default:5" json:"tax_rate"`
	DeliveryStatus   int             `gorm:"default:0" json:"delivery_status"` // 0=未交 1=部分交貨 2=已交齊
	IsStopped        bool            `gorm:"default:false" json:"is_stopped"`  // 停交標記
	Items            []PurchaseItem  `gorm:"foreignKey:PurchaseID" json:"items,omitempty"`
}

// PurchaseItem 採購明細行（每個商品一列）
type PurchaseItem struct {
	ID            int64              `gorm:"primaryKey" json:"id"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
	PurchaseID    int64              `gorm:"not null;index" json:"purchase_id"`
	Purchase      *Purchase          `gorm:"foreignKey:PurchaseID" json:"purchase,omitempty"`
	ProductID     int64              `gorm:"not null;index" json:"product_id"`
	Product       *Product           `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	SizeGroupID   *int64             `gorm:"index" json:"size_group_id"`
	SizeGroup     *SizeGroup         `gorm:"foreignKey:SizeGroupID" json:"size_group,omitempty"`
	ItemOrder     int                `gorm:"default:0" json:"item_order"`
	AdvicePrice   float64            `gorm:"type:numeric(18,2)" json:"advice_price"`
	Discount      float64            `gorm:"type:numeric(5,2);default:0" json:"discount"`
	PurchasePrice float64            `gorm:"type:numeric(18,2)" json:"purchase_price"`
	NonTaxPrice   float64            `gorm:"type:numeric(18,2)" json:"non_tax_price"`
	TotalQty      int                `gorm:"default:0" json:"total_qty"`
	TotalAmount   float64            `gorm:"type:numeric(18,2);default:0" json:"total_amount"`
	Supplement    int                `gorm:"type:integer;default:0" json:"supplement"`  // 0:空 1:舖 2:補 3:停
	CancelFlag    int                `gorm:"type:integer;default:1" json:"cancel_flag"` // 1:正常 2:清除(停交)
	ExpectedDate  string             `gorm:"type:varchar(20)" json:"expected_date"`
	Sizes         []PurchaseItemSize `gorm:"foreignKey:PurchaseItemID" json:"sizes,omitempty"`
}

// PurchaseItemSize 採購明細尺碼數量
type PurchaseItemSize struct {
	ID             int64       `gorm:"primaryKey" json:"id"`
	PurchaseItemID int64       `gorm:"not null;uniqueIndex:idx_item_size" json:"purchase_item_id"`
	SizeOptionID   int64       `gorm:"not null;uniqueIndex:idx_item_size" json:"size_option_id"`
	SizeOption     *SizeOption `gorm:"foreignKey:SizeOptionID" json:"size_option,omitempty"`
	Qty            int         `gorm:"default:0" json:"qty"`
}
