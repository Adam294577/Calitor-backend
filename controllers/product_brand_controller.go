package controllers

import (
	"net/http"
	"project/middlewares"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
)

func GetProductBrands(c *gin.Context) {
	if tryListCache(c) {
		return
	}
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.ProductBrand
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name")
	paged, total := Paginate(c, query, &models.ProductBrand{})
	paged.Find(&items)
	setListCache(c, items, total)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateProductBrand(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		Code     string `json:"code" binding:"required"`
		Name     string `json:"name" binding:"required"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var count int64
	db.GetRead().Model(&models.ProductBrand{}).Where("code = ?", req.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "代號已存在").Send()
		return
	}

	item := models.ProductBrand{Code: req.Code, Name: req.Name, IsActive: true}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	invalidateListCache("product-brands")
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateProductBrand(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 無「編輯主檔代碼」權限者，忽略 code 欄位變更
	if !middlewares.HasPermission(c, "edit-master-code") {
		req.Code = ""
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.ProductBrand
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	if req.Code != "" && req.Code != item.Code {
		var count int64
		db.GetRead().Model(&models.ProductBrand{}).Where("code = ? AND id != ?", req.Code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "代號已存在").Send()
			return
		}
	}

	updates := map[string]interface{}{}
	if req.Code != "" {
		updates["code"] = req.Code
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	db.GetWrite().Model(&item).Updates(updates)
	invalidateListCache("product-brands")
	resp.Success("更新成功").Send()
}

func DeleteProductBrand(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var productCount int64
	db.GetRead().Model(&models.Product{}).Where("product_brand_id = ?", id).Count(&productCount)
	if productCount > 0 {
		resp.Fail(http.StatusBadRequest, "此品牌仍有商品使用中，無法刪除").Send()
		return
	}

	db.GetWrite().Delete(&models.ProductBrand{}, id)
	invalidateListCache("product-brands")
	resp.Success("刪除成功").Send()
}
