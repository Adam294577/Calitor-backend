package models

import (
	"time"

	"gorm.io/gorm"
)

// Stock InputMode 來源
const (
	StockInputModeKeyboard = 1 // 鍵盤輸入(controllers.CreateStock 單筆 endpoint 預設)
	StockInputModeBarcode  = 2 // 條碼掃描(services/stock.CreateBatch 批次建單預設)
)

// Stock 進貨主表
type Stock struct {
	ID              int64           `gorm:"primaryKey" json:"id"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	DeletedAt       gorm.DeletedAt  `gorm:"index" json:"deleted_at"`
	StockNo         string          `gorm:"type:varchar(50);uniqueIndex;not null" json:"stock_no"`
	StockDate       string          `gorm:"type:varchar(20);not null" json:"stock_date"`
	CustomerID      int64           `gorm:"not null;index" json:"customer_id"`
	Customer        *RetailCustomer `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	VendorID        int64           `gorm:"not null;index" json:"vendor_id"`
	Vendor          *Vendor         `gorm:"foreignKey:VendorID" json:"vendor,omitempty"`
	PurchaseID      *int64          `gorm:"index" json:"purchase_id"`
	Purchase        *Purchase       `gorm:"foreignKey:PurchaseID" json:"purchase,omitempty"`
	VendorStockNo   string          `gorm:"type:varchar(50)" json:"vendor_stock_no"` // 廠商單號（備註用，非綁定）
	StockMode       int             `gorm:"default:1" json:"stock_mode"`             // 1=進貨 2=退貨
	DealMode        int             `gorm:"default:1" json:"deal_mode"`              // 1=買斷 2=寄賣
	FillPersonID    *int64          `gorm:"index" json:"fill_person_id"`
	FillPerson      *Admin          `gorm:"foreignKey:FillPersonID" json:"fill_person,omitempty"`
	RecorderID      int64           `gorm:"not null;index" json:"recorder_id"`
	Recorder        *Admin          `gorm:"foreignKey:RecorderID" json:"recorder,omitempty"`
	CloseMonth      string          `gorm:"type:varchar(10)" json:"close_month"`
	Remark          string          `gorm:"type:text" json:"remark"`
	TaxMode         int             `gorm:"default:2" json:"tax_mode"`                      // 1=含稅 2=應稅
	TaxRate         float64         `gorm:"type:numeric(5,2);default:5" json:"tax_rate"`    // 稅率%
	TaxAmount       float64         `gorm:"type:numeric(18,2);default:0" json:"tax_amount"` // 稅金金額
	DiscountPercent float64         `gorm:"type:numeric(5,2);default:100" json:"discount_percent"`
	DiscountAmount  float64         `gorm:"type:numeric(18,2);default:0" json:"discount_amount"`
	InvoiceDate     string          `gorm:"type:varchar(20)" json:"invoice_date"`
	InvoiceNo       string          `gorm:"type:varchar(50)" json:"invoice_no"`
	InvoiceAmount   float64         `gorm:"type:numeric(18,2);default:0" json:"invoice_amount"`
	ChargeAmount    float64         `gorm:"type:numeric(18,2);default:0" json:"charge_amount"`
	InputMode       int             `gorm:"type:integer;default:1" json:"input_mode"` // 1=鍵盤 2=掃描器
	Items           []StockItem     `gorm:"foreignKey:StockID" json:"items,omitempty"`
}

// StockItem 進貨明細行
type StockItem struct {
	ID             int64           `gorm:"primaryKey" json:"id"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	StockID        int64           `gorm:"not null;index" json:"stock_id"`
	ProductID      int64           `gorm:"not null;index" json:"product_id"`
	Product        *Product        `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	SizeGroupID    *int64          `gorm:"index" json:"size_group_id"`
	SizeGroup      *SizeGroup      `gorm:"foreignKey:SizeGroupID" json:"size_group,omitempty"`
	PurchaseItemID *int64          `gorm:"index" json:"purchase_item_id"`
	PurchaseItem   *PurchaseItem   `gorm:"foreignKey:PurchaseItemID" json:"purchase_item,omitempty"`
	ItemOrder      int             `gorm:"default:0" json:"item_order"`
	AdvicePrice    float64         `gorm:"type:numeric(18,2)" json:"advice_price"`
	Discount       float64         `gorm:"type:numeric(5,2);default:0" json:"discount"`
	PurchasePrice  float64         `gorm:"type:numeric(18,2)" json:"purchase_price"`
	NonTaxPrice    float64         `gorm:"type:numeric(18,2)" json:"non_tax_price"`
	TotalQty       int             `gorm:"default:0" json:"total_qty"`
	TotalAmount    float64         `gorm:"type:numeric(18,2);default:0" json:"total_amount"`
	Supplement     int             `gorm:"type:integer;default:0" json:"supplement"` // 0=空 1=舖 2=補 3=停
	Sizes          []StockItemSize `gorm:"foreignKey:StockItemID" json:"sizes,omitempty"`
}

// StockItemSize 進貨尺碼數量
type StockItemSize struct {
	ID           int64       `gorm:"primaryKey" json:"id"`
	StockItemID  int64       `gorm:"not null;uniqueIndex:idx_stock_item_size" json:"stock_item_id"`
	SizeOptionID int64       `gorm:"not null;uniqueIndex:idx_stock_item_size" json:"size_option_id"`
	SizeOption   *SizeOption `gorm:"foreignKey:SizeOptionID" json:"size_option,omitempty"`
	Qty          int         `gorm:"default:0" json:"qty"`
}
