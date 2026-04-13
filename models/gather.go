package models

import (
	"time"

	"gorm.io/gorm"
)

// Gather 收款對帳單主表
type Gather struct {
	ID              int64           `gorm:"primaryKey" json:"id"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	DeletedAt       gorm.DeletedAt  `gorm:"index" json:"deleted_at"`
	GatherNo        string          `gorm:"type:varchar(50);uniqueIndex;not null" json:"gather_no"`  // 收款單號
	GatherDate      string          `gorm:"type:varchar(20);not null" json:"gather_date"`            // 收款日期
	CustomerID      int64           `gorm:"not null;index" json:"customer_id"`
	Customer        *RetailCustomer `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	CheckNo         string          `gorm:"type:varchar(50)" json:"check_no"`                        // 支票號碼
	CheckDueDate    string          `gorm:"type:varchar(20)" json:"check_due_date"`                  // 到期日
	GatherAmount    float64         `gorm:"type:numeric(18,2);default:0" json:"gather_amount"`       // 收款金額
	DiscountAmount  float64         `gorm:"type:numeric(18,2);default:0" json:"discount_amount"`     // 折讓金額
	OtherDeduct     float64         `gorm:"type:numeric(18,2);default:0" json:"other_deduct"`        // 其它扣額
	ShipTotal       float64         `gorm:"type:numeric(18,2);default:0" json:"ship_total"`          // 出貨總額
	ActualAmount    float64         `gorm:"type:numeric(18,2);default:0" json:"actual_amount"`       // 實收金額
	PrepaidCreditUsed float64       `gorm:"type:numeric(18,2);default:0" json:"prepaid_credit_used"` // 取用預收貸款
	GatherPersonID  *int64          `gorm:"index" json:"gather_person_id"`
	GatherPerson    *Admin          `gorm:"foreignKey:GatherPersonID" json:"gather_person,omitempty"`
	RecorderID      int64           `gorm:"not null;index" json:"recorder_id"`
	Recorder        *Admin          `gorm:"foreignKey:RecorderID" json:"recorder,omitempty"`
	BankAccountNo   string          `gorm:"type:varchar(50)" json:"bank_account_no"`                 // 入票帳號 (Bank.AccountNo)
	StartBrandID    string          `gorm:"type:varchar(20)" json:"start_brand_id"`                  // 起始品牌
	EndBrandID      string          `gorm:"type:varchar(20)" json:"end_brand_id"`                    // 結束品牌
	Remark          string          `gorm:"type:text" json:"remark"`
	Items           []GatherDetail  `gorm:"foreignKey:GatherID" json:"items,omitempty"`
}

// GatherDetail 收款對帳明細
type GatherDetail struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	GatherID       int64     `gorm:"not null;index" json:"gather_id"`
	ShipmentID     int64     `gorm:"not null;index" json:"shipment_id"`
	Shipment       *Shipment `gorm:"foreignKey:ShipmentID" json:"shipment,omitempty"`
	GatherMode     int       `gorm:"default:3" json:"gather_mode"`                            // 3=出貨沖銷
	ShipDate       string    `gorm:"type:varchar(20)" json:"ship_date"`                       // 出貨日期
	ShipAmount     float64   `gorm:"type:numeric(18,2);default:0" json:"ship_amount"`         // 出貨金額
	DiscountAmount float64   `gorm:"type:numeric(18,2);default:0" json:"discount_amount"`     // 折讓金額
	OtherDeduct    float64   `gorm:"type:numeric(18,2);default:0" json:"other_deduct"`        // 其他扣額
	WriteOffAmount float64   `gorm:"type:numeric(18,2);default:0" json:"write_off_amount"`    // 沖銷金額
	Brand          string    `gorm:"type:varchar(20)" json:"brand"`                           // 對帳品牌
	ItemOrder      int       `gorm:"default:0" json:"item_order"`
	Remark         string    `gorm:"type:text" json:"remark"`
}
