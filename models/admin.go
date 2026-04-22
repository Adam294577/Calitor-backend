package models

import (
	"project/services/common"
	"project/services/log"
	"time"

	"github.com/spf13/viper"
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
}

// AdminRole 帳號角色關聯（1 對多）
type AdminRole struct {
	ID      int64 `gorm:"primaryKey" json:"id"`
	AdminId int64 `gorm:"not null;uniqueIndex:idx_admin_role" json:"admin_id"`
	RoleId  int64 `gorm:"not null;uniqueIndex:idx_admin_role" json:"role_id"`
}

// SeedDefaultAdmin 初始化預設管理員帳號
// 密碼從環境變數 SEED_ADMIN_PASSWORD 或設定檔 Server.SeedAdminPassword 讀取
func SeedDefaultAdmin(db *DBManager) {
	var count int64
	db.GetRead().Model(&Admin{}).Count(&count)
	if count > 0 {
		return
	}

	password := viper.GetString("Server.SeedAdminPassword")
	if password == "" {
		log.Error("未設定 Server.SeedAdminPassword（或環境變數 SERVER_SEEDADMINPASSWORD），跳過建立預設管理員")
		return
	}

	hashedPassword, err := common.HashPassword(password)
	if err != nil {
		log.Error("密碼雜湊失敗: %s", err.Error())
		return
	}
	admin := Admin{
		Account:  "admin",
		Name:     "管理員",
		Password: hashedPassword,
		IsSuper:  true,
	}
	if err := db.GetWrite().Create(&admin).Error; err != nil {
		log.Error("建立預設管理員失敗: %s", err.Error())
		return
	}
	// 綁定 admin 角色（ID=1）
	ar := AdminRole{AdminId: admin.ID, RoleId: 1}
	db.GetWrite().Where("admin_id = ? AND role_id = ?", ar.AdminId, ar.RoleId).FirstOrCreate(&ar)
	log.Info("已建立預設管理員帳號: admin")
}
