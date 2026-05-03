package controllers

import (
	"net"
	"net/http"
	"project/models"
	"project/services/firewall"
	response "project/services/responses"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// validateIPOrCIDR 驗證輸入是否為合法 IP 或 CIDR
// 同時回傳標準化後的字串（IP 會被 net.ParseIP 標準化）
func validateIPOrCIDR(entry string) (string, bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", false
	}
	if strings.Contains(entry, "/") {
		if _, _, err := net.ParseCIDR(entry); err != nil {
			return "", false
		}
		return entry, true
	}
	parsed := net.ParseIP(entry)
	if parsed == nil {
		return "", false
	}
	return parsed.String(), true
}

// GetFirewallIPs 防火牆 IP 列表
func GetFirewallIPs(c *gin.Context) {
	if tryListCache(c) {
		return
	}
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.FirewallIP
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "ip", "name")
	paged, total := Paginate(c, query, &models.FirewallIP{})
	paged.Find(&items)
	setListCache(c, items, total)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// CreateFirewallIP 新增防火牆 IP（固定 source='manual'）
func CreateFirewallIP(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		IP       string `json:"ip" binding:"required"`
		Name     string `json:"name" binding:"required"`
		Note     string `json:"note"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	normalizedIP, ok := validateIPOrCIDR(req.IP)
	if !ok {
		resp.Fail(http.StatusBadRequest, "IP 格式錯誤，請輸入合法的 IP 或 CIDR").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var count int64
	db.GetRead().Model(&models.FirewallIP{}).Where("ip = ?", normalizedIP).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "IP 已存在").Send()
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	item := models.FirewallIP{
		IP:       normalizedIP,
		Name:     req.Name,
		Note:     req.Note,
		IsActive: isActive,
		Source:   "manual",
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	invalidateListCache("firewall-ips")
	firewall.Invalidate()
	resp.Success("新增成功").SetData(item).Send()
}

// UpdateFirewallIP 更新防火牆 IP
// source='env' 只能改 name/note/is_active，ip 不能動
// source='manual' 全部欄位可改
func UpdateFirewallIP(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		IP       string `json:"ip"`
		Name     string `json:"name"`
		Note     string `json:"note"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.FirewallIP
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	updates := map[string]interface{}{}

	// ip 變更（僅 manual 來源允許）
	if req.IP != "" && req.IP != item.IP {
		if item.Source == "env" {
			resp.Fail(http.StatusBadRequest, "環境變數來源的 IP 無法修改，請改由部署環境變數設定").Send()
			return
		}
		normalizedIP, ok := validateIPOrCIDR(req.IP)
		if !ok {
			resp.Fail(http.StatusBadRequest, "IP 格式錯誤，請輸入合法的 IP 或 CIDR").Send()
			return
		}
		var count int64
		db.GetRead().Model(&models.FirewallIP{}).Where("ip = ? AND id != ?", normalizedIP, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "IP 已存在").Send()
			return
		}
		updates["ip"] = normalizedIP
	}

	if req.Name != "" {
		updates["name"] = req.Name
	}
	updates["note"] = req.Note
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	if len(updates) == 0 {
		resp.Success("無變更").Send()
		return
	}

	if err := db.GetWrite().Model(&item).Updates(updates).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	invalidateListCache("firewall-ips")
	firewall.Invalidate()
	resp.Success("更新成功").Send()
}

// DeleteFirewallIP 刪除防火牆 IP（env 來源禁止刪除）
func DeleteFirewallIP(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.FirewallIP
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}
	if item.Source == "env" {
		resp.Fail(http.StatusBadRequest, "環境變數來源的 IP 無法刪除，請改由部署環境變數設定移除").Send()
		return
	}

	if err := db.GetWrite().Delete(&models.FirewallIP{}, id).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	invalidateListCache("firewall-ips")
	firewall.Invalidate()
	resp.Success("刪除成功").Send()
}
