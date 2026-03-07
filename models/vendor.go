package models

import (
	"time"

	"gorm.io/gorm"
)

// Vendor 廠商基本資料
type Vendor struct {
	ID             int64           `gorm:"primaryKey" json:"id"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	DeletedAt      gorm.DeletedAt  `gorm:"index" json:"deleted_at"`
	Code           string          `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	TaxId          string          `gorm:"type:varchar(20)" json:"tax_id"`
	CreatedDate    string          `gorm:"type:varchar(20)" json:"created_date"`
	Name           string          `gorm:"type:varchar(200);not null" json:"name"`
	ShortName      string          `gorm:"type:varchar(100)" json:"short_name"`
	CategoryId     *int64          `gorm:"index" json:"category_id"`
	Category       *VendorCategory `gorm:"foreignKey:CategoryId" json:"category,omitempty"`
	ClosingDate    int             `gorm:"default:26" json:"closing_date"`
	IsVisible      bool            `gorm:"default:true" json:"is_visible"`
	Owner          string          `gorm:"type:varchar(100)" json:"owner"`
	ContactPerson  string          `gorm:"type:varchar(100)" json:"contact_person"`
	Phone1         string          `gorm:"type:varchar(50)" json:"phone1"`
	Phone2         string          `gorm:"type:varchar(50)" json:"phone2"`
	Fax            string          `gorm:"type:varchar(50)" json:"fax"`
	InvoiceAddress string          `gorm:"type:varchar(500)" json:"invoice_address"`
	CompanyAddress string          `gorm:"type:varchar(500)" json:"company_address"`
	Email          string          `gorm:"type:varchar(200)" json:"email"`
	Discount       int             `gorm:"default:100" json:"discount"`
	Note           string          `gorm:"type:text" json:"note"`
	PaymentTerm    int             `gorm:"default:0" json:"payment_term"`
	TaxRate        float64         `gorm:"type:decimal(5,2);default:5" json:"tax_rate"`
	PriorPayable   float64         `gorm:"type:decimal(12,2)" json:"prior_payable"`
	PrepaidAmount  float64         `gorm:"type:decimal(12,2)" json:"prepaid_amount"`
}
