package models

import (
	"time"

	"gorm.io/gorm"
)

// Transfer InputMode 來源
const (
	TransferInputModeKeyboard = 1 // 鍵盤輸入(controllers.CreateTransfer 單筆 endpoint 預設)
	TransferInputModeBarcode  = 2 // 條碼掃描(services/transfer.CreateBatch 批次建單預設)
)

// Transfer 店櫃調撥主表
type Transfer struct {
	ID               int64           `gorm:"primaryKey" json:"id"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	DeletedAt        gorm.DeletedAt  `gorm:"index" json:"deleted_at"`
	TransferNo       string          `gorm:"type:varchar(50);uniqueIndex;not null" json:"transfer_no"`
	TransferDate     string          `gorm:"type:varchar(20);not null;index" json:"transfer_date"`
	SourceStore      string          `gorm:"type:varchar(20);not null" json:"source_store"` // 調出庫點 (branch_code)
	SourceCustomerID int64           `gorm:"not null;index" json:"source_customer_id"`
	SourceCustomer   *RetailCustomer `gorm:"foreignKey:SourceCustomerID" json:"source_customer,omitempty"`
	FillPersonID     *int64          `gorm:"index" json:"fill_person_id"`
	FillPerson       *Admin          `gorm:"foreignKey:FillPersonID" json:"fill_person,omitempty"`
	RecorderID       int64           `gorm:"not null;index" json:"recorder_id"`
	Recorder         *Admin          `gorm:"foreignKey:RecorderID" json:"recorder,omitempty"`
	Remark           string          `gorm:"type:text" json:"remark"`
	Confirmed        bool            `gorm:"default:false" json:"confirmed"`           // 確認調轉
	InputMode        int             `gorm:"type:integer;default:1" json:"input_mode"` // 1=鍵盤 2=掃描器
	Items            []TransferItem  `gorm:"foreignKey:TransferID" json:"items,omitempty"`
}

// TransferItem 店櫃調撥明細行
type TransferItem struct {
	ID             int64              `gorm:"primaryKey" json:"id"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
	TransferID     int64              `gorm:"not null;index" json:"transfer_id"`
	ProductID      int64              `gorm:"not null;index" json:"product_id"`
	Product        *Product           `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	SizeGroupID    *int64             `gorm:"index" json:"size_group_id"`
	SizeGroup      *SizeGroup         `gorm:"foreignKey:SizeGroupID" json:"size_group,omitempty"`
	ItemOrder      int                `gorm:"default:0" json:"item_order"`
	TotalQty       int                `gorm:"default:0" json:"total_qty"`
	UnitPrice      float64            `gorm:"type:numeric(18,2);default:0" json:"unit_price"`
	TotalAmount    float64            `gorm:"type:numeric(18,2);default:0" json:"total_amount"`
	DestStore      string             `gorm:"type:varchar(20);not null" json:"dest_store"` // 調入庫點 (branch_code)
	DestCustomerID int64              `gorm:"not null;index" json:"dest_customer_id"`
	DestCustomer   *RetailCustomer    `gorm:"foreignKey:DestCustomerID" json:"dest_customer,omitempty"`
	ItemConfirmed  bool               `gorm:"default:false" json:"item_confirmed"` // 單筆確認
	Sizes          []TransferItemSize `gorm:"foreignKey:TransferItemID" json:"sizes,omitempty"`
}

// TransferItemSize 店櫃調撥尺碼數量
type TransferItemSize struct {
	ID             int64       `gorm:"primaryKey" json:"id"`
	TransferItemID int64       `gorm:"not null;uniqueIndex:idx_transfer_item_size" json:"transfer_item_id"`
	SizeOptionID   int64       `gorm:"not null;uniqueIndex:idx_transfer_item_size" json:"size_option_id"`
	SizeOption     *SizeOption `gorm:"foreignKey:SizeOptionID" json:"size_option,omitempty"`
	Qty            int         `gorm:"default:0" json:"qty"`
}
