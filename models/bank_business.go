package models

import "time"

// BankBusiness 銀行交易紀錄（與 Gather 1:1 對應）
type BankBusiness struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	BankID       *int64    `gorm:"index" json:"bank_id"`                                  // FK → Bank
	Bank         *Bank     `gorm:"foreignKey:BankID" json:"bank,omitempty"`
	AccountNo    string    `gorm:"type:varchar(50);index" json:"account_no"`               // 銀行帳號 (= Bank.AccountNo)
	AccountDate  string    `gorm:"type:varchar(20)" json:"account_date"`                   // 交易日期
	AccountMode  int       `gorm:"default:1" json:"account_mode"`                          // 帳務模式 (1=收款)
	TicketMode   int       `gorm:"default:1" json:"ticket_mode"`                           // 票據模式 (1=現金)
	Maturity     string    `gorm:"type:varchar(20)" json:"maturity"`                       // 到期日
	ChequeID     string    `gorm:"type:varchar(50)" json:"cheque_id"`                      // 支票號碼
	Amount       float64   `gorm:"type:numeric(18,2);default:0" json:"amount"`             // 金額
	TransferMode int       `gorm:"default:0" json:"transfer_mode"`                         // 轉帳模式 (0=不轉帳)
	GatherID     int64     `gorm:"index" json:"gather_id"`                                 // FK → Gather
	Gather       *Gather   `gorm:"foreignKey:GatherID" json:"gather,omitempty"`
}
