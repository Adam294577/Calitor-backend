package models

import (
	"time"

	"gorm.io/gorm"
)

// Bank 銀行帳號主檔
type Bank struct {
	ID            int64          `gorm:"primaryKey" json:"id"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	AccountNo     string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"account_no"` // 銀行帳號
	Name          string         `gorm:"type:varchar(100);not null" json:"name"`                  // 銀行名稱
	Phone         string         `gorm:"type:varchar(20)" json:"phone"`                           // 銀行電話
	ContactPerson string         `gorm:"type:varchar(50)" json:"contact_person"`                  // 承辦人
	Balance       float64        `gorm:"type:numeric(18,2);default:0" json:"balance"`             // 結餘金額
	BalanceDate   string         `gorm:"type:varchar(20)" json:"balance_date"`                    // 結餘日期
}

// SeedBanks 初始化銀行帳號資料
func SeedBanks(db *DBManager) {
	banks := []Bank{
		{AccountNo: "001", Name: "臺灣土地銀行(太平)"},
		{AccountNo: "02562471556", Name: "台企-新店"},
		{AccountNo: "055508027454", Name: "聯邦-北中和"},
		{AccountNo: "0595811", Name: "郵局-圓環"},
		{AccountNo: "1211260143", Name: "台企-雙和"},
		{AccountNo: "301", Name: "吳郵"},
		{AccountNo: "592500", Name: "郵局"},
		{AccountNo: "595811", Name: "郵局"},
	}
	for _, b := range banks {
		db.GetWrite().Where("account_no = ?", b.AccountNo).FirstOrCreate(&b)
	}
}
