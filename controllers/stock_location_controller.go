package controllers

import (
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
)

func GetStockLocations(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.StockLocation
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name")
	paged, total := Paginate(c, query, &models.StockLocation{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateStockLocation(c *gin.Context) {
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
	db.GetRead().Model(&models.StockLocation{}).Where("code = ?", req.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "代號已存在").Send()
		return
	}

	item := models.StockLocation{Code: req.Code, Name: req.Name, IsActive: true}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateStockLocation(c *gin.Context) {
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

	db := models.PostgresNew()
	defer db.Close()

	var item models.StockLocation
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	if req.Code != "" && req.Code != item.Code {
		var count int64
		db.GetRead().Model(&models.StockLocation{}).Where("code = ? AND id != ?", req.Code, id).Count(&count)
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
	resp.Success("更新成功").Send()
}

func DeleteStockLocation(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var stockCount int64
	db.GetRead().Model(&models.ProductSizeStock{}).Where("stock_location_id = ?", id).Count(&stockCount)
	if stockCount > 0 {
		resp.Fail(http.StatusBadRequest, "此庫點仍有庫存資料，無法刪除").Send()
		return
	}

	db.GetWrite().Delete(&models.StockLocation{}, id)
	resp.Success("刪除成功").Send()
}
