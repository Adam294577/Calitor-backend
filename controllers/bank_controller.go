package controllers

import (
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
)

// GetBanks 銀行帳號列表
func GetBanks(c *gin.Context) {
	if tryListCache(c) {
		return
	}
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Bank
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "account_no", "name")
	paged, total := Paginate(c, query, &models.Bank{})
	paged.Find(&items)
	setListCache(c, items, total)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// CreateBank 新增銀行帳號
func CreateBank(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		AccountNo     string  `json:"account_no" binding:"required"`
		Name          string  `json:"name" binding:"required"`
		Phone         string  `json:"phone"`
		ContactPerson string  `json:"contact_person"`
		Balance       float64 `json:"balance"`
		BalanceDate   string  `json:"balance_date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var count int64
	db.GetRead().Model(&models.Bank{}).Where("account_no = ?", req.AccountNo).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "帳號已存在").Send()
		return
	}

	item := models.Bank{
		AccountNo:     req.AccountNo,
		Name:          req.Name,
		Phone:         req.Phone,
		ContactPerson: req.ContactPerson,
		Balance:       req.Balance,
		BalanceDate:   req.BalanceDate,
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	invalidateListCache("banks")
	resp.Success("新增成功").SetData(item).Send()
}

// UpdateBank 更新銀行帳號
func UpdateBank(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		AccountNo     string  `json:"account_no"`
		Name          string  `json:"name"`
		Phone         string  `json:"phone"`
		ContactPerson string  `json:"contact_person"`
		Balance       float64 `json:"balance"`
		BalanceDate   string  `json:"balance_date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Bank
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	if req.AccountNo != "" && req.AccountNo != item.AccountNo {
		var count int64
		db.GetRead().Model(&models.Bank{}).Where("account_no = ? AND id != ?", req.AccountNo, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "帳號已存在").Send()
			return
		}
	}

	updates := map[string]interface{}{}
	if req.AccountNo != "" {
		updates["account_no"] = req.AccountNo
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	updates["phone"] = req.Phone
	updates["contact_person"] = req.ContactPerson
	updates["balance"] = req.Balance
	updates["balance_date"] = req.BalanceDate

	db.GetWrite().Model(&item).Updates(updates)
	invalidateListCache("banks")
	resp.Success("更新成功").Send()
}

// DeleteBank 刪除銀行帳號
func DeleteBank(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	if err := db.GetWrite().Delete(&models.Bank{}, id).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	invalidateListCache("banks")
	resp.Success("刪除成功").Send()
}
