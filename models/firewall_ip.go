package models

import (
	"fmt"
	"project/services/log"
	"strings"
	"time"

	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// FirewallIP 防火牆白名單 IP
// 支援單一 IP（IPv4 / IPv6）與 CIDR（例：192.168.1.0/24）
type FirewallIP struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
	IP        string         `gorm:"type:varchar(64);uniqueIndex;not null" json:"ip"`
	Name      string         `gorm:"type:varchar(100);not null" json:"name"`
	Note      string         `gorm:"type:varchar(255)" json:"note"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
	Source    string         `gorm:"type:varchar(20);default:'manual'" json:"source"` // 'env' | 'manual'
}

// SyncEnvFirewallIPs 啟動時把 SERVER_SECURITY_ALLOWEDOFFICEIP 環境變數同步到 DB 表
// env var 是 source of truth：有就建立 / 復原 env 紀錄；不在 env 的舊 env 紀錄則刪除
// manual 紀錄不受影響
func SyncEnvFirewallIPs(db *DBManager) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("⚠ SyncEnvFirewallIPs 同步失敗: %v\n", r)
		}
	}()

	raw := strings.TrimSpace(viper.GetString("Server.Security.AllowedOfficeIP"))
	envSet := map[string]bool{}
	if raw != "" {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				envSet[p] = true
			}
		}
	}

	// upsert env 紀錄
	for ip := range envSet {
		var existing FirewallIP
		err := db.GetRead().Unscoped().Where("ip = ?", ip).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			if createErr := db.GetWrite().Create(&FirewallIP{
				IP: ip, Name: "環境變數", Source: "env", IsActive: true,
			}).Error; createErr != nil {
				log.Error("SyncEnvFirewallIPs: 新增 env IP %s 失敗: %s", ip, createErr.Error())
			}
			continue
		}
		if err != nil {
			log.Error("SyncEnvFirewallIPs: 查詢 %s 失敗: %s", ip, err.Error())
			continue
		}
		if existing.DeletedAt.Valid {
			// 之前被 soft delete，復原為 env 紀錄
			db.GetWrite().Unscoped().Model(&existing).Updates(map[string]interface{}{
				"deleted_at": nil, "source": "env", "is_active": true,
			})
		} else if existing.Source != "env" {
			// 手動建立的後來被設為 env，轉成 env 來源
			db.GetWrite().Model(&existing).Update("source", "env")
		}
	}

	// 移除 DB 裡 source='env' 但已經不在 env 的紀錄
	var stale []FirewallIP
	db.GetRead().Where("source = ?", "env").Find(&stale)
	for _, r := range stale {
		if !envSet[r.IP] {
			if err := db.GetWrite().Delete(&r).Error; err != nil {
				log.Error("SyncEnvFirewallIPs: 刪除過期 env IP %s 失敗: %s", r.IP, err.Error())
			}
		}
	}
	log.Info("SyncEnvFirewallIPs: 已同步 %d 筆 env IP 到 DB 表", len(envSet))
}
