package controllers

import (
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetProducts(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Product
	query := db.GetRead().
		Preload("Currency").
		Preload("Vendor").
		Preload("Brand").
		Preload("Categories").
		Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "model_code", "name_spec")
	if brandId := c.Query("brand_id"); brandId != "" {
		query = query.Where("brand_id = ?", brandId)
	}
	if vendorId := c.Query("vendor_id"); vendorId != "" {
		query = query.Where("vendor_id = ?", vendorId)
	}
	paged, total := Paginate(c, query, &models.Product{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func CreateProduct(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		models.Product
		CategoryIds []int64 `json:"category_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	if req.ModelCode == "" {
		resp.Fail(http.StatusBadRequest, "型號為必填").Send()
		return
	}

	var count int64
	db.GetRead().Model(&models.Product{}).Where("model_code = ?", req.ModelCode).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "型號已存在").Send()
		return
	}

	req.Product.ID = 0
	req.Product.Categories = nil

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&req.Product).Error; err != nil {
			return err
		}
		if len(req.CategoryIds) > 0 {
			var categories []models.ProductCategory
			tx.Where("id IN ?", req.CategoryIds).Find(&categories)
			if err := tx.Model(&req.Product).Association("Categories").Replace(categories); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("新增成功").SetData(req.Product).Send()
}

func UpdateProduct(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.Product
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "資料不存在").Send()
		return
	}

	var rawReq map[string]interface{}
	if err := c.ShouldBindJSON(&rawReq); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 檢查 model_code 唯一性
	if code, ok := rawReq["model_code"].(string); ok && code != "" && code != existing.ModelCode {
		var count int64
		db.GetRead().Model(&models.Product{}).Where("model_code = ? AND id != ?", code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "型號已存在").Send()
			return
		}
	}

	// 取出 category_ids
	var categoryIds []int64
	hasCategoryIds := false
	if raw, ok := rawReq["category_ids"]; ok {
		hasCategoryIds = true
		if arr, ok := raw.([]interface{}); ok {
			for _, v := range arr {
				if f, ok := v.(float64); ok {
					categoryIds = append(categoryIds, int64(f))
				}
			}
		}
	}

	// 移除不可更新的欄位
	delete(rawReq, "id")
	delete(rawReq, "created_at")
	delete(rawReq, "deleted_at")
	delete(rawReq, "currency")
	delete(rawReq, "vendor")
	delete(rawReq, "brand")
	delete(rawReq, "categories")
	delete(rawReq, "category_ids")

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if len(rawReq) > 0 {
			if err := tx.Model(&existing).Updates(rawReq).Error; err != nil {
				return err
			}
		}
		if hasCategoryIds {
			var categories []models.ProductCategory
			if len(categoryIds) > 0 {
				tx.Where("id IN ?", categoryIds).Find(&categories)
			}
			if err := tx.Model(&existing).Association("Categories").Replace(categories); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("更新成功").Send()
}

func DeleteProduct(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	// 清除 M2M 關聯
	var product models.Product
	if err := db.GetRead().Where("id = ?", id).First(&product).Error; err == nil {
		db.GetWrite().Model(&product).Association("Categories").Clear()
	}

	db.GetWrite().Delete(&models.Product{}, id)
	resp.Success("刪除成功").Send()
}
