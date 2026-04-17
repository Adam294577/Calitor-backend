package models

import (
	"time"

	"gorm.io/gorm"
)

// Shipment 客戶出貨單主表
type Shipment struct {
	ID              int64           `gorm:"primaryKey" json:"id"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	DeletedAt       gorm.DeletedAt  `gorm:"index" json:"deleted_at"`
	ShipmentNo      string          `gorm:"type:varchar(50);uniqueIndex;not null" json:"shipment_no"`
	ShipmentDate    string          `gorm:"type:varchar(20);not null" json:"shipment_date"`
	CustomerID      int64           `gorm:"not null;index" json:"customer_id"`
	Customer        *RetailCustomer `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	ShipmentMode    int             `gorm:"default:3" json:"shipment_mode"`                          // 3=出貨 4=退貨
	DealMode        int             `gorm:"default:1" json:"deal_mode"`                              // 1=買斷 2=寄賣
	ShipStore       string          `gorm:"type:varchar(20)" json:"ship_store"`                      // 出貨庫點 (branch_code)
	FillPersonID    *int64          `gorm:"index" json:"fill_person_id"`
	FillPerson      *Admin          `gorm:"foreignKey:FillPersonID" json:"fill_person,omitempty"`
	SalesmanID      *int64          `gorm:"index" json:"salesman_id"`
	Salesman        *Admin          `gorm:"foreignKey:SalesmanID" json:"salesman,omitempty"`
	RecorderID      int64           `gorm:"not null;index" json:"recorder_id"`
	Recorder        *Admin          `gorm:"foreignKey:RecorderID" json:"recorder,omitempty"`
	CloseMonth      string          `gorm:"type:varchar(10)" json:"close_month"`
	Remark          string          `gorm:"type:text" json:"remark"`
	TaxMode         int             `gorm:"default:2" json:"tax_mode"`                               // 1=含稅 2=應稅
	TaxRate         float64         `gorm:"type:numeric(5,2);default:5" json:"tax_rate"`
	TaxAmount       float64         `gorm:"type:numeric(18,2);default:0" json:"tax_amount"`
	DiscountPercent float64         `gorm:"type:numeric(5,2);default:100" json:"discount_percent"`
	DiscountAmount  float64         `gorm:"type:numeric(18,2);default:0" json:"discount_amount"`
	InvoiceDate     string          `gorm:"type:varchar(20)" json:"invoice_date"`
	InvoiceNo       string          `gorm:"type:varchar(50)" json:"invoice_no"`
	InvoiceAmount   float64         `gorm:"type:numeric(18,2);default:0" json:"invoice_amount"`
	DealAmount      float64         `gorm:"type:numeric(18,2);default:0" json:"deal_amount"`         // 應收金額（含稅合計）
	ChargeAmount    float64         `gorm:"type:numeric(18,2);default:0" json:"charge_amount"`       // 已收金額 (由 Gather 沖銷回寫)
	ClientGoodID    string          `gorm:"type:varchar(50)" json:"client_good_id"`                 // 客戶貨號
	InputMode       int             `gorm:"type:integer;default:1" json:"input_mode"`               // 1=鍵盤 2=掃描器
	Items           []ShipmentItem  `gorm:"foreignKey:ShipmentID" json:"items,omitempty"`
}

// ShipmentItem 客戶出貨明細行
type ShipmentItem struct {
	ID           int64              `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	ShipmentID   int64              `gorm:"not null;index" json:"shipment_id"`
	ProductID    int64              `gorm:"not null;index" json:"product_id"`
	Product      *Product           `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	SizeGroupID  *int64             `gorm:"index" json:"size_group_id"`
	SizeGroup    *SizeGroup         `gorm:"foreignKey:SizeGroupID" json:"size_group,omitempty"`
	OrderItemID  *int64             `gorm:"index" json:"order_item_id"`
	OrderItem    *OrderItem         `gorm:"foreignKey:OrderItemID" json:"order_item,omitempty"`
	ItemOrder    int                `gorm:"default:0" json:"item_order"`
	SellPrice    float64            `gorm:"type:numeric(18,2)" json:"sell_price"`                // 售價
	Discount     float64            `gorm:"type:numeric(5,2);default:0" json:"discount"`         // 折數
	ShipPrice    float64            `gorm:"type:numeric(18,2)" json:"ship_price"`                // 單價
	NonTaxPrice  float64            `gorm:"type:numeric(18,2)" json:"non_tax_price"`
	TotalQty     int                `gorm:"default:0" json:"total_qty"`
	TotalAmount  float64            `gorm:"type:numeric(18,2);default:0" json:"total_amount"`
	ShipCost     float64            `gorm:"type:numeric(18,2);default:0" json:"ship_cost"`
	Supplement   int                `gorm:"type:integer;default:1" json:"supplement"`             // 1:舖 2:補
	Sizes        []ShipmentItemSize `gorm:"foreignKey:ShipmentItemID" json:"sizes,omitempty"`
}

// ShipmentItemSize 客戶出貨明細尺碼數量
type ShipmentItemSize struct {
	ID             int64       `gorm:"primaryKey" json:"id"`
	ShipmentItemID int64       `gorm:"not null;uniqueIndex:idx_shipment_item_size" json:"shipment_item_id"`
	SizeOptionID   int64       `gorm:"not null;uniqueIndex:idx_shipment_item_size" json:"size_option_id"`
	SizeOption     *SizeOption `gorm:"foreignKey:SizeOptionID" json:"size_option,omitempty"`
	Qty            int         `gorm:"default:0" json:"qty"`
}
