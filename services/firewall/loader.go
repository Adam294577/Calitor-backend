package firewall

import (
	"net"
	"project/models"
	"project/services/log"
	"project/services/redis"
	"strings"
	"time"
)

const cacheKey = "firewall:allowed_ips"
const cacheTTL = 5 * time.Minute

// Snapshot 快取用的 JSON 結構（Redis 友善、middleware 拿到後再 parse）
type Snapshot struct {
	IPs  []string `json:"ips"`
	CIDR []string `json:"cidr"`
}

// Load 取得目前生效的白名單（先查 Redis 快取，miss 則查 DB 並寫回）
// 任何一層失敗都會 fallback 直接查 DB；DB 也失敗就回傳空 Snapshot
func Load() Snapshot {
	rc := redis.Global()
	if rc.IsAvailable() {
		var cached Snapshot
		if err := rc.GetJSON(cacheKey, &cached); err == nil {
			return cached
		}
	}
	snap := queryDB()
	if rc.IsAvailable() {
		if err := rc.SetJSON(cacheKey, snap, cacheTTL); err != nil {
			log.Warn("FirewallLoader: 寫入 Redis 快取失敗: %s", err.Error())
		}
	}
	return snap
}

// Invalidate 清除快取，CRUD 後呼叫以立即生效
func Invalidate() {
	rc := redis.Global()
	if !rc.IsAvailable() {
		return
	}
	if err := rc.Delete(cacheKey); err != nil {
		log.Warn("FirewallLoader: 清除快取失敗: %s", err.Error())
	}
}

func queryDB() Snapshot {
	db := models.PostgresNew()
	var rows []models.FirewallIP
	if err := db.GetRead().Where("is_active = ?", true).Find(&rows).Error; err != nil {
		log.Error("FirewallLoader: 查詢 firewall_ips 失敗: %s", err.Error())
		return Snapshot{}
	}

	ips := make([]string, 0, len(rows))
	cidrs := make([]string, 0)
	for _, r := range rows {
		entry := strings.TrimSpace(r.IP)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, _, err := net.ParseCIDR(entry); err != nil {
				log.Error("FirewallLoader: 無法解析 CIDR %s: %s", entry, err.Error())
				continue
			}
			cidrs = append(cidrs, entry)
		} else {
			if parsed := net.ParseIP(entry); parsed != nil {
				ips = append(ips, parsed.String())
			} else {
				log.Error("FirewallLoader: 無法解析 IP %s", entry)
			}
		}
	}
	return Snapshot{IPs: ips, CIDR: cidrs}
}
