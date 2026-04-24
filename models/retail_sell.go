package models

import (
	"time"

	"gorm.io/gorm"
)

// RetailSell 零售銷售單主表（店櫃銷售）
type RetailSell struct {
	ID           int64           `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	DeletedAt    gorm.DeletedAt  `gorm:"index" json:"deleted_at"`
	SellNo       string          `gorm:"type:varchar(50);uniqueIndex;not null" json:"sell_no"`
	SellDate     string          `gorm:"type:varchar(20);not null;index" json:"sell_date"`
	CustomerID   int64           `gorm:"not null;index" json:"customer_id"`
	Customer     *RetailCustomer `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	SellStore    string          `gorm:"type:varchar(20)" json:"sell_store"` // 分店=庫點 (branch_code)
	SellPersonID *int64          `gorm:"index" json:"sell_person_id"`
	SellPerson   *Admin          `gorm:"foreignKey:SellPersonID" json:"sell_person,omitempty"`
	RecorderID   int64           `gorm:"not null;index" json:"recorder_id"`
	Recorder     *Admin          `gorm:"foreignKey:RecorderID" json:"recorder,omitempty"`
	// 付款（整單統一）
	CashAmount float64 `gorm:"type:numeric(18,2);default:0" json:"cash_amount"`
	CardAmount float64 `gorm:"type:numeric(18,2);default:0" json:"card_amount"`
	GiftAmount float64 `gorm:"type:numeric(18,2);default:0" json:"gift_amount"`
	// 稅務
	TaxRate float64 `gorm:"type:numeric(5,2);default:5" json:"tax_rate"`
	// 發票區塊
	TaxID         string  `gorm:"type:varchar(20)" json:"tax_id"` // 統一編號
	InvoiceAmount float64 `gorm:"type:numeric(18,2);default:0" json:"invoice_amount"`
	CardType      string  `gorm:"type:varchar(50)" json:"card_type"`      // 信用卡別
	GiftType      string  `gorm:"type:varchar(50)" json:"gift_type"`      // 禮券別
	CreditCardNo  string  `gorm:"type:varchar(50)" json:"credit_card_no"` // 信用卡號
	// 其他
	IsAbnormal bool   `gorm:"default:false" json:"is_abnormal"` // 異常銷售
	Remark     string `gorm:"type:text" json:"remark"`
	// 明細
	Items []RetailSellItem `gorm:"foreignKey:RetailSellID" json:"items,omitempty"`
}

// RetailSellItem 零售銷售明細
type RetailSellItem struct {
	ID           int64       `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	RetailSellID int64       `gorm:"not null;index" json:"retail_sell_id"`
	RetailSell   *RetailSell `gorm:"foreignKey:RetailSellID" json:"retail_sell,omitempty"`
	ItemOrder    int         `gorm:"default:0" json:"item_order"`
	ProductID    int64       `gorm:"not null;index" json:"product_id"`
	Product      *Product    `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	SizeGroupID  *int64      `gorm:"index" json:"size_group_id"`
	SizeGroup    *SizeGroup  `gorm:"foreignKey:SizeGroupID" json:"size_group,omitempty"`
	// 會員（明細層級）
	MemberID *int64  `gorm:"index" json:"member_id"`
	Member   *Member `gorm:"foreignKey:MemberID" json:"member,omitempty"`
	// 價格
	SellPrice   float64 `gorm:"type:numeric(18,2)" json:"sell_price"`        // 單價
	Discount    float64 `gorm:"type:numeric(5,2);default:0" json:"discount"` // 折數
	TotalQty    int     `gorm:"default:0" json:"total_qty"`
	TotalAmount float64 `gorm:"type:numeric(18,2);default:0" json:"total_amount"`
	// 付款（明細層級）
	CashAmount float64 `gorm:"type:numeric(18,2);default:0" json:"cash_amount"`
	CardAmount float64 `gorm:"type:numeric(18,2);default:0" json:"card_amount"`
	GiftAmount float64 `gorm:"type:numeric(18,2);default:0" json:"gift_amount"`
	// 異動類型: 1=銷貨 2=退貨 7=贈品
	SellMode int                  `gorm:"default:1" json:"sell_mode"`
	Sizes    []RetailSellItemSize `gorm:"foreignKey:RetailSellItemID" json:"sizes,omitempty"`
}

// RetailSellItemSize 零售銷售明細尺碼數量
type RetailSellItemSize struct {
	ID               int64       `gorm:"primaryKey" json:"id"`
	RetailSellItemID int64       `gorm:"not null;uniqueIndex:idx_retail_sell_item_size" json:"retail_sell_item_id"`
	SizeOptionID     int64       `gorm:"not null;uniqueIndex:idx_retail_sell_item_size" json:"size_option_id"`
	SizeOption       *SizeOption `gorm:"foreignKey:SizeOptionID" json:"size_option,omitempty"`
	Qty              int         `gorm:"default:0" json:"qty"`
}
