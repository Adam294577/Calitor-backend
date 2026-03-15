package models

import "time"

// CostFormula 進貨成本轉換公式
type CostFormula struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Name       string    `gorm:"type:varchar(100);not null" json:"name"`
	Expression string    `gorm:"type:text;not null" json:"expression"`     // e.g. "{price} * {rate} + {shipping}"
	Variables  string    `gorm:"type:jsonb;default:'[]'" json:"variables"` // [{"key":"rate","label":"匯率","default":4.7}]
}
