package controllers

import (
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func GetCustomers(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.RetailCustomer
	query := db.GetRead().Preload("Location").Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name", "short_name")
	if locId := c.Query("location_id"); locId != "" {
		query = query.Where("location_id = ?", locId)
	}
	paged, total := Paginate(c, query, &models.RetailCustomer{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// GetCustomerOptions 客戶下拉選項（輕量版，僅 id/code/name/short_name/branch_code）
func GetCustomerOptions(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	type option struct {
		ID         int64  `json:"id"`
		Code       string `json:"code"`
		Name       string `json:"name"`
		ShortName  string `json:"short_name"`
		BranchCode string `json:"branch_code"`
	}
	var items []option
	db.GetRead().Model(&models.RetailCustomer{}).
		Select("id, code, name, short_name, branch_code").
		Order("id ASC").
		Find(&items)
	resp.Success("成功").SetData(items).Send()
}

func CreateCustomer(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var item models.RetailCustomer
	if err := c.ShouldBindJSON(&item); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	if item.Code == "" || item.Name == "" {
		resp.Fail(http.StatusBadRequest, "客戶代號和名稱為必填").Send()
		return
	}

	var count int64
	db.GetRead().Model(&models.RetailCustomer{}).Where("code = ?", item.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "客戶代號已存在").Send()
		return
	}

	item.ID = 0
	item.CreatedDate = time.Now().Format("20060102")
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateCustomer(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.RetailCustomer
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	var req struct {
		Code               *string  `json:"code"`
		BranchCode         *string  `json:"branch_code"`
		ChainNo            *string  `json:"chain_no"`
		Name               *string  `json:"name"`
		ShortName          *string  `json:"short_name"`
		Category           *string  `json:"category"`
		Month              *string  `json:"month"`
		ClosingDate        *int     `json:"closing_date"`
		TaxId              *string  `json:"tax_id"`
		InvoiceName        *string  `json:"invoice_name"`
		TaxRate            *float64 `json:"tax_rate"`
		Discount           *int     `json:"discount"`
		CreatedDate        *string  `json:"created_date"`
		CreditLimit        *float64 `json:"credit_limit"`
		IsVisible          *bool    `json:"is_visible"`
		IsCreditRestricted *bool    `json:"is_credit_restricted"`
		Owner              *string  `json:"owner"`
		ContactPerson      *string  `json:"contact_person"`
		Phone1             *string  `json:"phone1"`
		Phone2             *string  `json:"phone2"`
		Fax                *string  `json:"fax"`
		Email              *string  `json:"email"`
		InvoiceAddress     *string  `json:"invoice_address"`
		BillingAddress     *string  `json:"billing_address"`
		ShippingAddress    *string  `json:"shipping_address"`
		LocationId         *int64   `json:"location_id"`
		District           *string  `json:"district"`
		Note               *string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 檢查 code 唯一性
	if req.Code != nil && *req.Code != "" && *req.Code != existing.Code {
		var count int64
		db.GetRead().Model(&models.RetailCustomer{}).Where("code = ? AND id != ?", *req.Code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "客戶代號已存在").Send()
			return
		}
	}

	db.GetWrite().Model(&existing).Updates(req)
	resp.Success("更新成功").Send()
}

func DeleteCustomer(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	db.GetWrite().Delete(&models.RetailCustomer{}, id)
	resp.Success("刪除成功").Send()
}
