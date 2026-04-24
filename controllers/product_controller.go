package controllers

import (
	"math"
	"net/http"
	"project/models"
	"project/services/permission"
	response "project/services/responses"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetProducts(c *gin.Context) {
	if tryListCache(c) {
		return
	}
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Product
	query := db.GetRead().Order("id ASC")
	query = ApplySearch(query, c.Query("search"), "model_code", "name_spec")
	if brandId := c.Query("brand_id"); brandId != "" {
		query = query.Where("brand_id = ?", brandId)
	}
	if vendorId := c.Query("vendor_id"); vendorId != "" {
		query = query.Where("id IN (SELECT product_id FROM product_vendors WHERE vendor_id = ?)", vendorId)
	}
	paged, total := Paginate(c, query, &models.Product{})
	paged.
		Preload("ProductBrand").
		Preload("ProductVendors.Vendor").
		Find(&items)
	setListCache(c, items, total)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

func GetProduct(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Product
	err = db.GetRead().
		Preload("ProductBrand").
		Preload("Brand").
		Preload("Size1Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Size2Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Size3Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("ProductVendors.Vendor").
		Preload("CategoryMaps.Category1").
		Preload("CategoryMaps.Category2").
		Preload("CategoryMaps.Category3").
		Preload("CategoryMaps.Category4").
		Preload("CategoryMaps.Category5").
		Where("id = ?", id).
		First(&item).Error
	if err != nil {
		resp.Fail(http.StatusNotFound, "商品不存在").Send()
		return
	}
	resp.Success("成功").SetData(item).Send()
}

func CreateProduct(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		ModelCode         string  `json:"model_code" binding:"required"`
		Currency          string  `json:"currency"`
		NameSpec          string  `json:"name_spec"`
		MSRP              float64 `json:"msrp"`
		SpecialPrice      float64 `json:"special_price"`
		OriginalPrice     float64 `json:"original_price"`
		WholesaleTaxIncl  float64 `json:"wholesale_tax_incl"`
		WholesaleDiscount float64 `json:"wholesale_discount"`
		BillingBrand      string  `json:"billing_brand"`
		ProductBrandID    *int64  `json:"product_brand_id"`
		TradeMode         int64   `json:"trade_mode"`
		IsVisible         bool    `json:"is_visible"`
		Season            string  `json:"season"`
		Remark            string  `json:"remark"`
		MaterialOuter     string  `json:"material_outer"`
		MaterialInner     string  `json:"material_inner"`
		ToeCaptrim        string  `json:"toe_cap_trim"`
		Lining            string  `json:"lining"`
		Sock              string  `json:"sock"`
		Sole              string  `json:"sole"`
		ImageURL          string  `json:"image_url"`
		Size1GroupID      *int64  `json:"size1_group_id"`
		Size2GroupID      *int64  `json:"size2_group_id"`
		Size3GroupID      *int64  `json:"size3_group_id"`
		CategoryMaps      []struct {
			CategoryType int    `json:"category_type"`
			Category1ID  *int64 `json:"category1_id"`
			Category2ID  *int64 `json:"category2_id"`
			Category3ID  *int64 `json:"category3_id"`
			Category4ID  *int64 `json:"category4_id"`
			Category5ID  *int64 `json:"category5_id"`
		} `json:"category_maps"`
		ProductVendors []struct {
			VendorID      int64   `json:"vendor_id"`
			CostDiscount  float64 `json:"cost_discount"`
			CostStart     float64 `json:"cost_start"`
			CostLast      float64 `json:"cost_last"`
			OriginalPrice float64 `json:"original_price"`
			IsPrimary     bool    `json:"is_primary"`
		} `json:"product_vendors"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	var count int64
	db.GetRead().Model(&models.Product{}).Where("model_code = ?", req.ModelCode).Count(&count)
	if count > 0 {
		resp.Fail(http.StatusBadRequest, "型號已存在").Send()
		return
	}

	now := time.Now()
	product := models.Product{
		ModelCode:         req.ModelCode,
		Currency:          req.Currency,
		NameSpec:          req.NameSpec,
		MSRP:              req.MSRP,
		SpecialPrice:      req.SpecialPrice,
		OriginalPrice:     req.OriginalPrice,
		WholesaleTaxIncl:  req.WholesaleTaxIncl,
		Wholesale:         math.Round(req.WholesaleTaxIncl / 1.05),
		WholesaleDiscount: req.WholesaleDiscount,
		BillingBrand:      req.BillingBrand,
		ProductBrandId:    req.ProductBrandID,
		TradeMode:         req.TradeMode,
		IsVisible:         req.IsVisible,
		Season:            req.Season,
		Remark:            req.Remark,
		MaterialOuter:     req.MaterialOuter,
		MaterialInner:     req.MaterialInner,
		ToeCapTrim:        req.ToeCaptrim,
		Lining:            req.Lining,
		Sock:              req.Sock,
		Sole:              req.Sole,
		ImageURL:          req.ImageURL,
		Size1GroupID:      req.Size1GroupID,
		Size2GroupID:      req.Size2GroupID,
		Size3GroupID:      req.Size3GroupID,
		CreatedOn:         &now,
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&product).Error; err != nil {
			return err
		}
		// 建立 CategoryMaps
		for _, cm := range req.CategoryMaps {
			item := models.ProductCategoryMap{
				ProductID:    product.ID,
				CategoryType: cm.CategoryType,
				Category1ID:  cm.Category1ID,
				Category2ID:  cm.Category2ID,
				Category3ID:  cm.Category3ID,
				Category4ID:  cm.Category4ID,
				Category5ID:  cm.Category5ID,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		// 建立 ProductVendors
		for _, pv := range req.ProductVendors {
			item := models.ProductVendor{
				ProductID:     product.ID,
				VendorID:      pv.VendorID,
				CostDiscount:  pv.CostDiscount,
				CostStart:     pv.CostStart,
				CostLast:      pv.CostLast,
				OriginalPrice: pv.OriginalPrice,
				IsPrimary:     pv.IsPrimary,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	invalidateListCache("products")
	resp.Success("新增成功").SetData(product).Send()
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

	// 無「編輯主檔代碼」權限者，忽略 model_code 欄位變更
	permission.StripMasterCodeFields(c, rawReq, "model_code")

	// 檢查 model_code 唯一性
	if code, ok := rawReq["model_code"].(string); ok && code != "" && code != existing.ModelCode {
		var count int64
		db.GetRead().Model(&models.Product{}).Where("model_code = ? AND id != ?", code, id).Count(&count)
		if count > 0 {
			resp.Fail(http.StatusBadRequest, "型號已存在").Send()
			return
		}
	}

	// 取出 category_maps
	var categoryMaps []interface{}
	hasCategoryMaps := false
	if raw, ok := rawReq["category_maps"]; ok {
		hasCategoryMaps = true
		if arr, ok := raw.([]interface{}); ok {
			categoryMaps = arr
		}
	}

	// 取出 product_vendors
	var productVendors []interface{}
	hasProductVendors := false
	if raw, ok := rawReq["product_vendors"]; ok {
		hasProductVendors = true
		if arr, ok := raw.([]interface{}); ok {
			productVendors = arr
		}
	}

	// 含稅批價 → 自動計算未稅批價
	if v, ok := rawReq["wholesale_tax_incl"].(float64); ok {
		rawReq["wholesale"] = math.Round(v / 1.05)
	}

	// 移除不可更新的欄位
	for _, key := range []string{"id", "created_at", "deleted_at", "product_brand", "brand", "category_maps", "product_vendors", "size1_group", "size2_group", "size3_group"} {
		delete(rawReq, key)
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if len(rawReq) > 0 {
			if err := tx.Model(&existing).Updates(rawReq).Error; err != nil {
				return err
			}
		}
		// 重建 CategoryMaps
		if hasCategoryMaps {
			tx.Where("product_id = ?", id).Delete(&models.ProductCategoryMap{})
			for _, raw := range categoryMaps {
				m, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				cm := models.ProductCategoryMap{ProductID: id}
				if v, ok := m["category_type"].(float64); ok {
					cm.CategoryType = int(v)
				}
				if v, ok := m["category1_id"].(float64); ok {
					vid := int64(v)
					cm.Category1ID = &vid
				}
				if v, ok := m["category2_id"].(float64); ok {
					vid := int64(v)
					cm.Category2ID = &vid
				}
				if v, ok := m["category3_id"].(float64); ok {
					vid := int64(v)
					cm.Category3ID = &vid
				}
				if v, ok := m["category4_id"].(float64); ok {
					vid := int64(v)
					cm.Category4ID = &vid
				}
				if v, ok := m["category5_id"].(float64); ok {
					vid := int64(v)
					cm.Category5ID = &vid
				}
				if err := tx.Create(&cm).Error; err != nil {
					return err
				}
			}
		}
		// 重建 ProductVendors
		if hasProductVendors {
			tx.Where("product_id = ?", id).Delete(&models.ProductVendor{})
			for _, raw := range productVendors {
				m, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				pv := models.ProductVendor{ProductID: id}
				if v, ok := m["vendor_id"].(float64); ok {
					pv.VendorID = int64(v)
				}
				if v, ok := m["cost_discount"].(float64); ok {
					pv.CostDiscount = v
				}
				if v, ok := m["cost_start"].(float64); ok {
					pv.CostStart = v
				}
				if v, ok := m["cost_last"].(float64); ok {
					pv.CostLast = v
				}
				if v, ok := m["original_price"].(float64); ok {
					pv.OriginalPrice = v
				}
				if v, ok := m["is_primary"].(bool); ok {
					pv.IsPrimary = v
				}
				if err := tx.Create(&pv).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	invalidateListCache("products")
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

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		tx.Where("product_id = ?", id).Delete(&models.ProductCategoryMap{})
		tx.Where("product_id = ?", id).Delete(&models.ProductVendor{})
		return tx.Delete(&models.Product{}, id).Error
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	invalidateListCache("products")
	resp.Success("刪除成功").Send()
}
