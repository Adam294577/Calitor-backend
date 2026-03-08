package controllers

import (
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ==================== SizeGroup ====================

func GetSizeGroups(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.SizeGroup
	query := db.GetRead().Preload("Options", func(db2 *gorm.DB) *gorm.DB {
		return db2.Order("sort_order ASC")
	}).Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name")
	paged, total := Paginate(c, query, &models.SizeGroup{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateSizeGroup(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		Code string `json:"code" binding:"required"`
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var count int64
	db.GetRead().Model(&models.SizeGroup{}).Where("code = ?", req.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "代號已存在").Send()
		return
	}

	item := models.SizeGroup{Code: req.Code, Name: req.Name}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateSizeGroup(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.SizeGroup
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	if req.Code != "" && req.Code != item.Code {
		var count int64
		db.GetRead().Model(&models.SizeGroup{}).Where("code = ? AND id != ?", req.Code, id).Count(&count)
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
	db.GetWrite().Model(&item).Updates(updates)
	resp.Success("更新成功").Send()
}

func DeleteSizeGroup(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	// 檢查是否被 Product 引用
	var productCount int64
	db.GetRead().Model(&models.Product{}).
		Where("size1_group_id = ? OR size2_group_id = ? OR size3_group_id = ?", id, id, id).
		Count(&productCount)
	if productCount > 0 {
		resp.Fail(http.StatusBadRequest, "此尺碼組仍有商品使用中，無法刪除").Send()
		return
	}

	// 刪除子選項
	db.GetWrite().Where("size_group_id = ?", id).Delete(&models.SizeOption{})
	db.GetWrite().Delete(&models.SizeGroup{}, id)
	resp.Success("刪除成功").Send()
}

// ==================== SizeOption ====================

func GetSizeOptions(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.SizeOption
	query := db.GetRead().Order("sort_order ASC")
	if groupId := c.Query("size_group_id"); groupId != "" {
		query = query.Where("size_group_id = ?", groupId)
	}
	query = ApplySearch(query, c.Query("search"), "code", "label")
	paged, total := Paginate(c, query, &models.SizeOption{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateSizeOption(c *gin.Context) {
	resp := response.New(c)
	var req struct {
		SizeGroupID int64  `json:"size_group_id" binding:"required"`
		Code        string `json:"code" binding:"required"`
		Label       string `json:"label" binding:"required"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請填寫完整資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	item := models.SizeOption{
		SizeGroupID: req.SizeGroupID,
		Code:        req.Code,
		Label:       req.Label,
		SortOrder:   req.SortOrder,
	}
	if err := db.GetWrite().Create(&item).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("新增成功").SetData(item).Send()
}

func UpdateSizeOption(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var req struct {
		Code      string `json:"code"`
		Label     string `json:"label"`
		SortOrder *int   `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.SizeOption
	if err := db.GetRead().Where("id = ?", id).First(&item).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	updates := map[string]interface{}{}
	if req.Code != "" {
		updates["code"] = req.Code
	}
	if req.Label != "" {
		updates["label"] = req.Label
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	db.GetWrite().Model(&item).Updates(updates)
	resp.Success("更新成功").Send()
}

func DeleteSizeOption(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	db.GetWrite().Delete(&models.SizeOption{}, id)
	resp.Success("刪除成功").Send()
}
