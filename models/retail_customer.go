package models

import (
	"time"

	"gorm.io/gorm"
)

// RetailCustomer 客戶基本資料
type RetailCustomer struct {
	ID                 int64          `gorm:"primaryKey" json:"id"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	Code               string         `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	BranchCode         string         `gorm:"type:varchar(50)" json:"branch_code"`
	ChainNo            string         `gorm:"type:varchar(50)" json:"chain_no"`
	Name               string         `gorm:"type:varchar(200);not null" json:"name"`
	ShortName          string         `gorm:"type:varchar(100)" json:"short_name"`
	Category           string         `gorm:"type:varchar(100)" json:"category"`
	Month              string         `gorm:"type:varchar(20)" json:"month"`
	ClosingDate        int            `gorm:"default:26" json:"closing_date"`
	TaxId              string         `gorm:"type:varchar(20)" json:"tax_id"`
	InvoiceName        string         `gorm:"type:varchar(200)" json:"invoice_name"`
	TaxRate            float64        `gorm:"type:decimal(5,2);default:5" json:"tax_rate"`
	Discount           int            `gorm:"default:100" json:"discount"`
	CreatedDate        string         `gorm:"type:varchar(20)" json:"created_date"`
	CreditLimit        float64        `gorm:"type:decimal(12,2)" json:"credit_limit"`
	IsVisible          bool           `gorm:"default:true" json:"is_visible"`
	IsCreditRestricted bool           `gorm:"default:true" json:"is_credit_restricted"`
	Owner              string         `gorm:"type:varchar(100)" json:"owner"`
	ContactPerson      string         `gorm:"type:varchar(100)" json:"contact_person"`
	Phone1             string         `gorm:"type:varchar(50)" json:"phone1"`
	Phone2             string         `gorm:"type:varchar(50)" json:"phone2"`
	Fax                string         `gorm:"type:varchar(50)" json:"fax"`
	Email              string         `gorm:"type:varchar(200)" json:"email"`
	InvoiceAddress     string         `gorm:"type:varchar(500)" json:"invoice_address"`
	BillingAddress     string         `gorm:"type:varchar(500)" json:"billing_address"`
	ShippingAddress    string         `gorm:"type:varchar(500)" json:"shipping_address"`
	LocationId         *int64         `gorm:"index" json:"location_id"`
	Location           *Location      `gorm:"foreignKey:LocationId" json:"location,omitempty"`
	District           string         `gorm:"type:varchar(100)" json:"district"`
	Note               string         `gorm:"type:text" json:"note"`
	HasStockLocation   bool           `gorm:"default:false" json:"has_stock_location"`
}
