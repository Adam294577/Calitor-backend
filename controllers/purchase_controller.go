package controllers

import (
	"fmt"
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetPurchases 採購單列表
func GetPurchases(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Purchase
	query := db.GetRead().
		Preload("Customer").
		Preload("Vendor").
		Order("purchase_date DESC, id DESC")

	query = ApplySearch(query, c.Query("search"), "purchase_no")

	if v := c.Query("customer_id"); v != "" {
		query = query.Where("customer_id = ?", v)
	}
	if v := c.Query("vendor_id"); v != "" {
		query = query.Where("vendor_id = ?", v)
	}
	if v := c.Query("date_from"); v != "" {
		query = query.Where("purchase_date >= ?", v)
	}
	if v := c.Query("date_to"); v != "" {
		query = query.Where("purchase_date <= ?", v)
	}
	if v := c.Query("deal_mode"); v != "" {
		query = query.Where("deal_mode = ?", v)
	}

	paged, total := Paginate(c, query, &models.Purchase{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// GetPurchase 採購單詳情
func GetPurchase(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Purchase
	err = db.GetRead().
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("item_order ASC")
		}).
		Preload("Items.Product").
		Preload("Items.Product.Size1Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Product.Size2Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Product.Size3Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.SizeGroup.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Sizes").
		Where("id = ?", id).
		First(&item).Error
	if err != nil {
		resp.Fail(http.StatusNotFound, "採購單不存在").Send()
		return
	}
	resp.Success("成功").SetData(item).Send()
}

// CreatePurchase 新增採購單
func CreatePurchase(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		PurchaseDate     string  `json:"purchase_date" binding:"required"`
		CustomerID       int64   `json:"customer_id" binding:"required"`
		VendorID         int64   `json:"vendor_id" binding:"required"`
		FillPersonID     *int64  `json:"fill_person_id"`
		DealMode         int     `json:"deal_mode"`
		ConfirmationDate string  `json:"confirmation_date"`
		Remark           string  `json:"remark"`
		TaxMode          int     `json:"tax_mode"`
		TaxRate          float64 `json:"tax_rate"`
		Items            []struct {
			ProductID     int64   `json:"product_id"`
			SizeGroupID   *int64  `json:"size_group_id"`
			ItemOrder     int     `json:"item_order"`
			AdvicePrice   float64 `json:"advice_price"`
			Discount      float64 `json:"discount"`
			PurchasePrice float64 `json:"purchase_price"`
			NonTaxPrice   float64 `json:"non_tax_price"`
			Supplement    int     `json:"supplement"`
			ExpectedDate  string  `json:"expected_date"`
			Sizes         []struct {
				SizeOptionID int64 `json:"size_option_id"`
				Qty          int   `json:"qty"`
			} `json:"sizes"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 查詢廠商代號
	var vendor models.Vendor
	if err := db.GetRead().Where("id = ?", req.VendorID).First(&vendor).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "廠商不存在").Send()
		return
	}

	// 查詢客戶 BranchCode
	var customer models.RetailCustomer
	if err := db.GetRead().Where("id = ?", req.CustomerID).First(&customer).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "客戶不存在").Send()
		return
	}

	// 產生採購單號: VendorCode + BranchCode + YYYYMM + 流水號4碼
	yyyymm := ""
	if len(req.PurchaseDate) >= 6 {
		yyyymm = req.PurchaseDate[:6]
	}
	prefix := vendor.Code + customer.BranchCode + yyyymm

	var maxNo string
	db.GetRead().Model(&models.Purchase{}).
		Where("purchase_no LIKE ?", prefix+"%").
		Select("MAX(purchase_no)").
		Scan(&maxNo)

	seq := 1
	if maxNo != "" && len(maxNo) > len(prefix) {
		tail := maxNo[len(prefix):]
		if n, err := strconv.Atoi(tail); err == nil {
			seq = n + 1
		}
	}
	purchaseNo := fmt.Sprintf("%s%04d", prefix, seq)

	if req.DealMode == 0 {
		req.DealMode = 1
	}
	if req.TaxMode == 0 {
		req.TaxMode = 2
	}

	// 系統紀錄者：永遠是登入者
	adminId, _ := c.Get("AdminId")
	recorderID := int64(0)
	if id, ok := adminId.(float64); ok {
		recorderID = int64(id)
	}

	purchase := models.Purchase{
		PurchaseNo:       purchaseNo,
		PurchaseDate:     req.PurchaseDate,
		CustomerID:       req.CustomerID,
		VendorID:         req.VendorID,
		FillPersonID:     req.FillPersonID,
		RecorderID:       recorderID,
		DealMode:         req.DealMode,
		ConfirmationDate: req.ConfirmationDate,
		Remark:           req.Remark,
		TaxMode:          req.TaxMode,
		TaxRate:          req.TaxRate,
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&purchase).Error; err != nil {
			return err
		}
		for _, reqItem := range req.Items {
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := float64(totalQty) * reqItem.PurchasePrice

			item := models.PurchaseItem{
				PurchaseID:    purchase.ID,
				ProductID:     reqItem.ProductID,
				SizeGroupID:   reqItem.SizeGroupID,
				ItemOrder:     reqItem.ItemOrder,
				AdvicePrice:   reqItem.AdvicePrice,
				Discount:      reqItem.Discount,
				PurchasePrice: reqItem.PurchasePrice,
				NonTaxPrice:   reqItem.NonTaxPrice,
				TotalQty:      totalQty,
				TotalAmount:   totalAmount,
				Supplement:    reqItem.Supplement,
				ExpectedDate:  reqItem.ExpectedDate,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			for _, s := range reqItem.Sizes {
				size := models.PurchaseItemSize{
					PurchaseItemID: item.ID,
					SizeOptionID:   s.SizeOptionID,
					Qty:            s.Qty,
				}
				if err := tx.Create(&size).Error; err != nil {
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

	resp.Success("新增成功").SetData(purchase).Send()
}

// UpdatePurchase 更新採購單
func UpdatePurchase(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.Purchase
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusNotFound, "採購單不存在").Send()
		return
	}

	var req struct {
		PurchaseDate     string  `json:"purchase_date"`
		CustomerID       int64   `json:"customer_id"`
		VendorID         int64   `json:"vendor_id"`
		FillPersonID     *int64  `json:"fill_person_id"`
		DealMode         int     `json:"deal_mode"`
		ConfirmationDate string  `json:"confirmation_date"`
		Remark           string  `json:"remark"`
		TaxMode          int     `json:"tax_mode"`
		TaxRate          float64 `json:"tax_rate"`
		Items            []struct {
			ProductID     int64   `json:"product_id"`
			SizeGroupID   *int64  `json:"size_group_id"`
			ItemOrder     int     `json:"item_order"`
			AdvicePrice   float64 `json:"advice_price"`
			Discount      float64 `json:"discount"`
			PurchasePrice float64 `json:"purchase_price"`
			NonTaxPrice   float64 `json:"non_tax_price"`
			Supplement    int     `json:"supplement"`
			ExpectedDate  string  `json:"expected_date"`
			Sizes         []struct {
				SizeOptionID int64 `json:"size_option_id"`
				Qty          int   `json:"qty"`
			} `json:"sizes"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 刪除舊的 Sizes 和 Items
		var oldItemIDs []int64
		tx.Model(&models.PurchaseItem{}).Where("purchase_id = ?", id).Pluck("id", &oldItemIDs)
		if len(oldItemIDs) > 0 {
			tx.Where("purchase_item_id IN ?", oldItemIDs).Delete(&models.PurchaseItemSize{})
		}
		tx.Where("purchase_id = ?", id).Delete(&models.PurchaseItem{})

		// 系統紀錄者：永遠是登入者
		adminId, _ := c.Get("AdminId")
		recorderID := existing.RecorderID
		if id, ok := adminId.(float64); ok {
			recorderID = int64(id)
		}

		// 更新主表
		updates := map[string]interface{}{
			"purchase_date":     req.PurchaseDate,
			"customer_id":       req.CustomerID,
			"vendor_id":         req.VendorID,
			"fill_person_id":    req.FillPersonID,
			"recorder_id":       recorderID,
			"deal_mode":         req.DealMode,
			"confirmation_date": req.ConfirmationDate,
			"remark":            req.Remark,
			"tax_mode":          req.TaxMode,
			"tax_rate":          req.TaxRate,
		}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}

		// 重建 Items + Sizes
		for _, reqItem := range req.Items {
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := float64(totalQty) * reqItem.PurchasePrice

			item := models.PurchaseItem{
				PurchaseID:    id,
				ProductID:     reqItem.ProductID,
				SizeGroupID:   reqItem.SizeGroupID,
				ItemOrder:     reqItem.ItemOrder,
				AdvicePrice:   reqItem.AdvicePrice,
				Discount:      reqItem.Discount,
				PurchasePrice: reqItem.PurchasePrice,
				NonTaxPrice:   reqItem.NonTaxPrice,
				TotalQty:      totalQty,
				TotalAmount:   totalAmount,
				Supplement:    reqItem.Supplement,
				ExpectedDate:  reqItem.ExpectedDate,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			for _, s := range reqItem.Sizes {
				size := models.PurchaseItemSize{
					PurchaseItemID: item.ID,
					SizeOptionID:   s.SizeOptionID,
					Qty:            s.Qty,
				}
				if err := tx.Create(&size).Error; err != nil {
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

	resp.Success("更新成功").Send()
}

// DeletePurchase 軟刪除採購單
func DeletePurchase(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	if err := db.GetWrite().Delete(&models.Purchase{}, id).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("刪除成功").Send()
}

// SearchProducts 搜尋特定廠商的商品（供採購單選擇商品用）
func SearchProducts(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	search := c.Query("search")
	vendorID := c.Query("vendor_id")

	query := db.GetRead().Order("id ASC")
	if vendorID != "" {
		query = query.Where("id IN (SELECT product_id FROM product_vendors WHERE vendor_id = ?)", vendorID)
	}
	if search != "" {
		like := "%" + search + "%"
		query = query.Where("model_code ILIKE ? OR name_spec ILIKE ?", like, like)
	}

	var items []models.Product
	query.
		Preload("Size1Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Size2Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Size3Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("CategoryMaps", func(db *gorm.DB) *gorm.DB {
			return db.Where("category_type = 5")
		}).
		Preload("CategoryMaps.Category5").
		Preload("ProductVendors", func(db *gorm.DB) *gorm.DB {
			if vendorID != "" {
				return db.Where("vendor_id = ?", vendorID)
			}
			return db
		}).
		Limit(20).
		Find(&items)

	resp.Success("成功").SetData(items).Send()
}
