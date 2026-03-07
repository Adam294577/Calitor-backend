package models

import (
	"project/services/common"
	"time"
)

// Admin 管理員
type Admin struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	IsDisabled bool      `gorm:"default:false" json:"is_disabled"`
	IsSuper    bool      `gorm:"default:false" json:"is_super"`
	Account    string    `gorm:"type:varchar(255);uniqueIndex;not null" json:"account"`
	Name       string    `gorm:"type:varchar(255);not null" json:"name"`
	Password   string    `gorm:"type:varchar(255);not null" json:"-"`
	RoleId     int64     `gorm:"not null" json:"role_id"`
}

// SeedDefaultAdmin 初始化預設管理員帳號
func SeedDefaultAdmin(db *DBManager) {
	var count int64
	db.GetRead().Model(&Admin{}).Count(&count)
	if count > 0 {
		return
	}

	hashedPassword, _ := common.HashPassword("123")
	admin := Admin{
		Account:  "admin",
		Name:     "管理員",
		Password: hashedPassword,
		RoleId:   1,
		IsSuper:  true,
	}
	db.GetWrite().Create(&admin)
}
