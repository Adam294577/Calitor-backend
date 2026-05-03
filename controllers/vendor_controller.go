package controllers

import (
	"net/http"
	"project/models"
	"project/services/permission"
	"project/services/purchase"
	response "project/services/responses"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetVendors(c *gin.Context) {
	if tryListCache(c) {
		return
	}
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Vendor
	search := c.Query("search")
	query := db.GetRead().Preload("Category")
	if search != "" {
		// code 前綴匹配優先 (例:輸入 54 時,540/541 排在 054/154 前)
		query = query.Order(gorm.Expr("CASE WHEN code ILIKE ? THEN 0 ELSE 1 END", search+"%"))
	}
	query = query.Order(ModelCodeOrderBy("code"))
	query = ApplySearch(query, search, "code", "name", "short_name")
	if catId := c.Query("category_id"); catId != "" {
		query = query.Where("category_id = ?", catId)
	}
	paged, total := Paginate(c, query, &models.Vendor{})
	paged.Find(&items)
	setListCache(c, items, total)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// GetVendorOptions 廠商下拉選項（輕量版，僅 id/code/name/short_name）
func GetVendorOptions(c *gin.Context) {
	if tryListCache(c) {
		return
	}
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	type option struct {
		ID        int64  `json:"id"`
		Code      string `json:"code"`
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
	}
	var items []option
	db.GetRead().Model(&models.Vendor{}).
		Select("id, code, name, short_name").
		Order(ModelCodeOrderBy("code")).
		Find(&items)
	setListCache(c, items, 0)
	resp.Success("成功").SetData(items).Send()
}

func CreateVendor(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var item models.Vendor
	if err := c.ShouldBindJSON(&item); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	if item.Code == "" || item.Name == "" {
		resp.Fail(http.StatusBadRequest, "廠商代號和名稱為必填").Send()
		return
	}

	var count int64
	db.GetRead().Model(&models.Vendor{}).Where("code = ?", item.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "廠商代號已存在").Send()
		return
	}

	item.ID = 0
	item.CreatedDate = time.Now().Format("20060102")
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	invalidateListCache("vendors")
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateVendor(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.Vendor
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	var req struct {
		Code           *string  `json:"code"`
		TaxId          *string  `json:"tax_id"`
		CreatedDate    *string  `json:"created_date"`
		Name           *string  `json:"name"`
		ShortName      *string  `json:"short_name"`
		CategoryId     *int64   `json:"category_id"`
		ClosingDate    *int     `json:"closing_date"`
		IsVisible      *bool    `json:"is_visible"`
		Owner          *string  `json:"owner"`
		ContactPerson  *string  `json:"contact_person"`
		Phone1         *string  `json:"phone1"`
		Phone2         *string  `json:"phone2"`
		Fax            *string  `json:"fax"`
		InvoiceAddress *string  `json:"invoice_address"`
		CompanyAddress *string  `json:"company_address"`
		Email          *string  `json:"email"`
		Discount       *int     `json:"discount"`
		Note           *string  `json:"note"`
		PaymentTerm    *int     `json:"payment_term"`
		TaxRate        *float64 `json:"tax_rate"`
		PriorPayable   *float64 `json:"prior_payable"`
		PrepaidAmount  *float64 `json:"prepaid_amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 無「編輯主檔代碼」權限者，忽略 code 欄位變更
	permission.StripMasterCodeFields(c, &req, "code")

	if req.Code != nil && *req.Code != "" && *req.Code != existing.Code {
		var count int64
		db.GetRead().Model(&models.Vendor{}).Where("code = ? AND id != ?", *req.Code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "廠商代號已存在").Send()
			return
		}
	}

	db.GetWrite().Model(&existing).Updates(req)
	invalidateListCache("vendors")
	resp.Success("更新成功").Send()
}

func DeleteVendor(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var pvCount int64
	db.GetRead().Model(&models.ProductVendor{}).Where("vendor_id = ?", id).Count(&pvCount)
	if pvCount > 0 {
		resp.Fail(http.StatusBadRequest, "此廠商仍有商品使用中，無法刪除").Send()
		return
	}

	db.GetWrite().Delete(&models.Vendor{}, id)
	invalidateListCache("vendors")
	resp.Success("刪除成功").Send()
}

// GetVendorRecentPurchasePrice 查指定廠商對特定商品+尺碼的「最近一次採購價」
// 用於條碼進貨切換到無候選廠商時的價格預設
// 三層 fallback:該廠商歷史採購 → 商品建檔原幣價 (Product.OriginalPrice) → 0
func GetVendorRecentPurchasePrice(c *gin.Context) {
	resp := response.New(c)
	vendorID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的廠商 ID").Send()
		return
	}
	productID, err := strconv.ParseInt(c.Query("product_id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的商品 ID").Send()
		return
	}
	sizeOptionID, err := strconv.ParseInt(c.Query("size_option_id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的尺碼選項 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	result := purchase.RecentPrice(db.GetRead(), vendorID, productID, sizeOptionID)
	resp.Success("成功").SetData(result).Send()
}
