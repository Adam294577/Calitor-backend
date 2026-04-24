package controllers

import (
	"fmt"
	"net/http"
	"project/models"
	"project/services/permission"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
)

// categoryModelByLevel 根據 level 回傳對應的 model 指標與 slice
func categoryModelByLevel(level int) (interface{}, interface{}, error) {
	switch level {
	case 1:
		return &models.ProductCategory1{}, &[]models.ProductCategory1{}, nil
	case 2:
		return &models.ProductCategory2{}, &[]models.ProductCategory2{}, nil
	case 3:
		return &models.ProductCategory3{}, &[]models.ProductCategory3{}, nil
	case 4:
		return &models.ProductCategory4{}, &[]models.ProductCategory4{}, nil
	case 5:
		return &models.ProductCategory5{}, &[]models.ProductCategory5{}, nil
	default:
		return nil, nil, fmt.Errorf("無效的類別等級: %d", level)
	}
}

func parseLevel(c *gin.Context) (int, error) {
	level, err := strconv.Atoi(c.Param("level"))
	if err != nil || level < 1 || level > 5 {
		return 0, fmt.Errorf("level 必須為 1~5")
	}
	return level, nil
}

func GetProductCategoriesByLevel(c *gin.Context) {
	if tryListCache(c) {
		return
	}
	resp := response.New(c)
	level, err := parseLevel(c)
	if err != nil {
		resp.Fail(http.StatusBadRequest, err.Error()).Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	model, items, _ := categoryModelByLevel(level)
	query := db.GetRead().Model(model).Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "code", "name")
	paged, total := Paginate(c, query, model)
	paged.Find(items)
	setListCache(c, items, total)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateProductCategoryByLevel(c *gin.Context) {
	resp := response.New(c)
	level, err := parseLevel(c)
	if err != nil {
		resp.Fail(http.StatusBadRequest, err.Error()).Send()
		return
	}

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

	model, _, _ := categoryModelByLevel(level)
	var count int64
	db.GetRead().Model(model).Where("code = ?", req.Code).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "代號已存在").Send()
		return
	}

	var created interface{}
	var createErr error
	switch level {
	case 1:
		item := models.ProductCategory1{Code: req.Code, Name: req.Name}
		createErr = db.GetWrite().Create(&item).Error
		created = item
	case 2:
		item := models.ProductCategory2{Code: req.Code, Name: req.Name}
		createErr = db.GetWrite().Create(&item).Error
		created = item
	case 3:
		item := models.ProductCategory3{Code: req.Code, Name: req.Name}
		createErr = db.GetWrite().Create(&item).Error
		created = item
	case 4:
		item := models.ProductCategory4{Code: req.Code, Name: req.Name}
		createErr = db.GetWrite().Create(&item).Error
		created = item
	case 5:
		item := models.ProductCategory5{Code: req.Code, Name: req.Name}
		createErr = db.GetWrite().Create(&item).Error
		created = item
	}

	if createErr != nil {
		resp.Panic(createErr).Send()
		return
	}
	invalidateListCache("product-categories")
	resp.Success("新增成功").SetData(created).Send()
}

func UpdateProductCategoryByLevel(c *gin.Context) {
	resp := response.New(c)
	level, err := parseLevel(c)
	if err != nil {
		resp.Fail(http.StatusBadRequest, err.Error()).Send()
		return
	}

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

	// 無「編輯主檔代碼」權限者，忽略 code 欄位變更
	permission.StripMasterCodeFields(c, &req, "code")

	db := models.PostgresNew()
	defer db.Close()

	model, _, _ := categoryModelByLevel(level)

	// 檢查存在（使用獨立 model 避免指標重用問題）
	checkModel, _, _ := categoryModelByLevel(level)
	if err := db.GetRead().Model(model).Where("id = ?", id).First(checkModel).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	// 檢查 code 唯一性
	if req.Code != "" {
		var count int64
		db.GetRead().Model(model).Where("code = ? AND id != ?", req.Code, id).Count(&count)
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

	tableName := fmt.Sprintf("product_category_%d", level)
	db.GetWrite().Table(tableName).Where("id = ?", id).Updates(updates)
	invalidateListCache("product-categories")
	resp.Success("更新成功").Send()
}

func DeleteProductCategoryByLevel(c *gin.Context) {
	resp := response.New(c)
	level, err := parseLevel(c)
	if err != nil {
		resp.Fail(http.StatusBadRequest, err.Error()).Send()
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	// 檢查是否有 ProductCategoryMap 引用
	colName := fmt.Sprintf("category%d_id", level)
	var mapCount int64
	db.GetRead().Model(&models.ProductCategoryMap{}).Where(colName+" = ?", id).Count(&mapCount)
	if mapCount > 0 {
		resp.Fail(http.StatusBadRequest, "此類別仍有商品使用中，無法刪除").Send()
		return
	}

	model, _, _ := categoryModelByLevel(level)
	if err := db.GetWrite().Where("id = ?", id).Delete(model).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	invalidateListCache("product-categories")
	resp.Success("刪除成功").Send()
}
