package models

import (
	"time"

	"gorm.io/gorm"
)

// Modify 庫存調整主表
type Modify struct {
	ID           int64           `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	DeletedAt    gorm.DeletedAt  `gorm:"index" json:"deleted_at"`
	ModifyNo     string          `gorm:"type:varchar(50);uniqueIndex;not null" json:"modify_no"`
	ModifyDate   string          `gorm:"type:varchar(20);not null;index" json:"modify_date"`
	ModifyStore  string          `gorm:"type:varchar(20);not null" json:"modify_store"` // 調整庫點 (branch_code)
	CustomerID   int64           `gorm:"not null;index" json:"customer_id"`
	Customer     *RetailCustomer `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	FillPersonID *int64          `gorm:"index" json:"fill_person_id"`
	FillPerson   *Admin          `gorm:"foreignKey:FillPersonID" json:"fill_person,omitempty"`
	RecorderID   int64           `gorm:"not null;index" json:"recorder_id"`
	Recorder     *Admin          `gorm:"foreignKey:RecorderID" json:"recorder,omitempty"`
	Remark       string          `gorm:"type:text" json:"remark"`
	Items        []ModifyItem    `gorm:"foreignKey:ModifyID" json:"items,omitempty"`
}

// ModifyItem 庫存調整明細行
type ModifyItem struct {
	ID          int64            `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	ModifyID    int64            `gorm:"not null;index" json:"modify_id"`
	ProductID   int64            `gorm:"not null;index" json:"product_id"`
	Product     *Product         `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	SizeGroupID *int64           `gorm:"index" json:"size_group_id"`
	SizeGroup   *SizeGroup       `gorm:"foreignKey:SizeGroupID" json:"size_group,omitempty"`
	ItemOrder   int              `gorm:"default:0" json:"item_order"`
	TotalQty    int              `gorm:"default:0" json:"total_qty"`
	Sizes       []ModifyItemSize `gorm:"foreignKey:ModifyItemID" json:"sizes,omitempty"`
}

// ModifyItemSize 庫存調整尺碼數量
type ModifyItemSize struct {
	ID           int64       `gorm:"primaryKey" json:"id"`
	ModifyItemID int64       `gorm:"not null;uniqueIndex:idx_modify_item_size" json:"modify_item_id"`
	SizeOptionID int64       `gorm:"not null;uniqueIndex:idx_modify_item_size" json:"size_option_id"`
	SizeOption   *SizeOption `gorm:"foreignKey:SizeOptionID" json:"size_option,omitempty"`
	Qty          int         `gorm:"default:0" json:"qty"` // 可正可負
}
