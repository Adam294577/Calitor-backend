package models

import (
	"time"

	"gorm.io/gorm"
)

// Member 會員基本資料
type Member struct {
	ID               int64          `gorm:"primaryKey" json:"id"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code             string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	VipCardNo        string         `gorm:"type:varchar(50)" json:"vip_card_no"`
	Gender           string         `gorm:"type:varchar(10)" json:"gender"`
	Name             string         `gorm:"type:varchar(200);not null" json:"name"`
	IdNumber         string         `gorm:"type:varchar(20)" json:"id_number"`
	Birthday         string         `gorm:"type:varchar(20)" json:"birthday"`
	JoinStore        string         `gorm:"type:varchar(100)" json:"join_store"`
	CardTypeId       *int64         `gorm:"index" json:"card_type_id"`
	CardType         *MemberTier    `gorm:"foreignKey:CardTypeId" json:"card_type,omitempty"`
	HomePhone        string         `gorm:"type:varchar(50)" json:"home_phone"`
	MobilePhone      string         `gorm:"type:varchar(50)" json:"mobile_phone"`
	OfficePhone      string         `gorm:"type:varchar(50)" json:"office_phone"`
	ContactAddress   string         `gorm:"type:varchar(500)" json:"contact_address"`
	ResidenceAddress string         `gorm:"type:varchar(500)" json:"residence_address"`
	CreatedDate      string         `gorm:"type:varchar(20)" json:"created_date"`
	ExpiryDate       string         `gorm:"type:varchar(20)" json:"expiry_date"`
	TotalSpending    float64        `gorm:"type:decimal(12,2)" json:"total_spending"`
	Points           float64        `gorm:"type:decimal(12,2)" json:"points"`
	SpendingDiscount float64        `gorm:"type:numeric(5,2);default:100" json:"spending_discount"`
	Email            string         `gorm:"type:varchar(200)" json:"email"`
	Note             string         `gorm:"type:text" json:"note"`
	PrintFlag        int            `gorm:"default:1" json:"print_flag"`
	CreditLimit      float64        `gorm:"type:decimal(12,2)" json:"credit_limit"`
	IsVisible        bool           `gorm:"default:true" json:"is_visible"`
	Brands           []Brand        `gorm:"many2many:member_brands" json:"brands,omitempty"`
}
