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
//
// 設計原則：DB 是 source of truth；env 只是 seed,只新增不破壞既有資料
//   - env 內的 IP：DB 沒有就建立 (source=env)，被 soft delete 過就復原
//   - env 內的 IP 已存在但 source=manual：保持 manual,不改寫(避免管理者手動加的 IP 被 sync 接管後又被誤刪)
//   - source=env 但已不在 env 的紀錄：停用 (is_active=false) 不刪除,管理者可在 UI 重新啟用或刪除
//   - manual 紀錄完全不受 sync 影響
//
// env 為空時直接 noop,絕不對 DB 動手腳(避免 env 因部署 bug 暫時消失就把白名單全停掉)
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

	if len(envSet) == 0 {
		log.Warn("SyncEnvFirewallIPs: env 為空,跳過同步(不對 DB 做任何變更)")
		return
	}

	created, restored, skippedManual, alreadyEnv := 0, 0, 0, 0

	for ip := range envSet {
		var existing FirewallIP
		err := db.GetRead().Unscoped().Where("ip = ?", ip).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			if createErr := db.GetWrite().Create(&FirewallIP{
				IP: ip, Name: "環境變數", Source: "env", IsActive: true,
			}).Error; createErr != nil {
				log.Error("SyncEnvFirewallIPs: 新增 env IP %s 失敗: %s", ip, createErr.Error())
				continue
			}
			created++
			continue
		}
		if err != nil {
			log.Error("SyncEnvFirewallIPs: 查詢 %s 失敗: %s", ip, err.Error())
			continue
		}
		if existing.DeletedAt.Valid {
			if updErr := db.GetWrite().Unscoped().Model(&existing).Updates(map[string]interface{}{
				"deleted_at": nil, "source": "env", "is_active": true,
			}).Error; updErr != nil {
				log.Error("SyncEnvFirewallIPs: 復原 env IP %s 失敗: %s", ip, updErr.Error())
				continue
			}
			restored++
			continue
		}
		if existing.Source != "env" {
			// 管理者手動加的 IP 即使在 env 中也保持 manual,不接管
			skippedManual++
			continue
		}
		alreadyEnv++
	}

	// env 列表不再包含的 source=env 紀錄:停用而非刪除,讓管理者可從 UI 復原
	var stale []FirewallIP
	if findErr := db.GetRead().Where("source = ? AND is_active = ?", "env", true).Find(&stale).Error; findErr != nil {
		log.Error("SyncEnvFirewallIPs: 掃描過期 env IP 失敗: %s", findErr.Error())
	}
	disabled := 0
	for _, r := range stale {
		if envSet[r.IP] {
			continue
		}
		if err := db.GetWrite().Model(&r).Update("is_active", false).Error; err != nil {
			log.Error("SyncEnvFirewallIPs: 停用過期 env IP %s 失敗: %s", r.IP, err.Error())
			continue
		}
		disabled++
		log.Warn("SyncEnvFirewallIPs: env 已不含 %s,將該紀錄停用(未刪除,UI 可復原)", r.IP)
	}

	log.Info("SyncEnvFirewallIPs: env=%d 筆 | 新增=%d 復原=%d 已是env=%d 略過manual=%d 停用過期=%d",
		len(envSet), created, restored, alreadyEnv, skippedManual, disabled)
}
