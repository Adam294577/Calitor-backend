package controllers

import (
	"fmt"
	"net/http"
	"project/models"
	"project/services/delivery"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetStocks 進貨單列表
func GetStocks(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Stock
	query := db.GetRead().
		Preload("Customer").
		Preload("Vendor").
		Preload("FillPerson").
		Preload("Recorder").
		Preload("Purchase").
		Order("stock_date DESC, id DESC")

	// 進貨單號搜尋
	if v := c.Query("search"); v != "" {
		query = ApplySearch(query, v, "stock_no")
	}

	// 廠商單號搜尋（透過關聯 Purchase）
	if v := c.Query("purchase_no"); v != "" {
		like := "%" + v + "%"
		query = query.Where("purchase_id IN (SELECT id FROM purchases WHERE deleted_at IS NULL AND purchase_no ILIKE ?)", like)
	}

	if v := c.Query("customer_id"); v != "" {
		query = query.Where("customer_id = ?", v)
	}
	if v := c.Query("vendor_id"); v != "" {
		query = query.Where("vendor_id = ?", v)
	}
	if v := c.Query("date_from"); v != "" {
		query = query.Where("stock_date >= ?", v)
	}
	if v := c.Query("date_to"); v != "" {
		query = query.Where("stock_date <= ?", v)
	}
	if v := c.Query("stock_mode"); v != "" {
		query = query.Where("stock_mode = ?", v)
	}

	paged, total := Paginate(c, query, &models.Stock{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// GetStock 進貨單詳情
func GetStock(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Stock
	err = db.GetRead().
		Preload("Customer").
		Preload("Vendor").
		Preload("FillPerson").
		Preload("Recorder").
		Preload("Purchase").
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("item_order ASC")
		}).
		Preload("Items.Product.Size1Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Product.Size2Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Product.Size3Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Product.CategoryMaps", func(db *gorm.DB) *gorm.DB {
			return db.Where("category_type = 5")
		}).
		Preload("Items.Product.CategoryMaps.Category5").
		Preload("Items.SizeGroup.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Sizes.SizeOption").
		Preload("Items.PurchaseItem").
		Where("id = ?", id).
		First(&item).Error
	if err != nil {
		resp.Fail(http.StatusNotFound, "進貨單不存在").Send()
		return
	}
	resp.Success("成功").SetData(item).Send()
}

// CreateStock 新增進貨單
func CreateStock(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		StockDate       string  `json:"stock_date" binding:"required"`
		CustomerID      int64   `json:"customer_id" binding:"required"`
		VendorID        int64   `json:"vendor_id" binding:"required"`
		PurchaseID      *int64  `json:"purchase_id"`
		StockMode       int     `json:"stock_mode"`
		DealMode        int     `json:"deal_mode"`
		CurrencyCode    string  `json:"currency_code"`
		FillPersonID    *int64  `json:"fill_person_id"`
		CloseMonth      string  `json:"close_month"`
		Remark          string  `json:"remark"`
		TaxMode         int     `json:"tax_mode"`
		TaxRate         float64 `json:"tax_rate"`
		TaxAmount       float64 `json:"tax_amount"`
		DiscountPercent float64 `json:"discount_percent"`
		DiscountAmount  float64 `json:"discount_amount"`
		InvoiceDate     string  `json:"invoice_date"`
		InvoiceNo       string  `json:"invoice_no"`
		InvoiceAmount   float64 `json:"invoice_amount"`
		ChargeAmount    float64 `json:"charge_amount"`
		Items           []struct {
			ProductID      int64   `json:"product_id"`
			SizeGroupID    *int64  `json:"size_group_id"`
			PurchaseItemID *int64  `json:"purchase_item_id"`
			ItemOrder      int     `json:"item_order"`
			AdvicePrice    float64 `json:"advice_price"`
			Discount       float64 `json:"discount"`
			PurchasePrice  float64 `json:"purchase_price"`
			NonTaxPrice    float64 `json:"non_tax_price"`
			Supplement     int     `json:"supplement"`
			Sizes          []struct {
				SizeOptionID int64 `json:"size_option_id"`
				Qty          int   `json:"qty"`
			} `json:"sizes"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 查詢客戶 BranchCode
	var customer models.RetailCustomer
	if err := db.GetRead().Where("id = ?", req.CustomerID).First(&customer).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "客戶不存在").Send()
		return
	}

	// 決定前綴
	if req.StockMode == 0 {
		req.StockMode = 1
	}
	prefix := "I"
	if req.StockMode == 2 {
		prefix = "B"
	}

	// 產生進貨單號: {前綴}{BranchCode}{YYYYMM}{流水號4碼}
	yyyymm := ""
	if len(req.StockDate) >= 6 {
		yyyymm = req.StockDate[:6]
	}
	noPrefix := prefix + customer.BranchCode + yyyymm

	var maxNo string
	db.GetRead().Model(&models.Stock{}).
		Where("stock_no LIKE ?", noPrefix+"%").
		Select("MAX(stock_no)").
		Scan(&maxNo)

	seq := 1
	if maxNo != "" && len(maxNo) > len(noPrefix) {
		tail := maxNo[len(noPrefix):]
		if n, err := strconv.Atoi(tail); err == nil {
			seq = n + 1
		}
	}
	stockNo := fmt.Sprintf("%s%04d", noPrefix, seq)

	if req.DealMode == 0 {
		req.DealMode = 1
	}
	if req.TaxMode == 0 {
		req.TaxMode = 2
	}
	if req.DiscountPercent == 0 {
		req.DiscountPercent = 100
	}

	// CloseMonth 若未填則取 StockDate 前 6 碼
	closeMonth := req.CloseMonth
	if closeMonth == "" && len(req.StockDate) >= 6 {
		closeMonth = req.StockDate[:6]
	}

	// 系統紀錄者
	adminId, _ := c.Get("AdminId")
	recorderID := int64(0)
	if id, ok := adminId.(float64); ok {
		recorderID = int64(id)
	}

	stock := models.Stock{
		StockNo:         stockNo,
		StockDate:       req.StockDate,
		CustomerID:      req.CustomerID,
		VendorID:        req.VendorID,
		PurchaseID:      req.PurchaseID,
		StockMode:       req.StockMode,
		DealMode:        req.DealMode,
		CurrencyCode:    req.CurrencyCode,
		FillPersonID:    req.FillPersonID,
		RecorderID:      recorderID,
		CloseMonth:      closeMonth,
		Remark:          req.Remark,
		TaxMode:         req.TaxMode,
		TaxRate:         req.TaxRate,
		TaxAmount:       req.TaxAmount,
		DiscountPercent: req.DiscountPercent,
		DiscountAmount:  req.DiscountAmount,
		InvoiceDate:     req.InvoiceDate,
		InvoiceNo:       req.InvoiceNo,
		InvoiceAmount:   req.InvoiceAmount,
		ChargeAmount:    req.ChargeAmount,
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&stock).Error; err != nil {
			return err
		}
		for _, reqItem := range req.Items {
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := float64(totalQty) * reqItem.PurchasePrice

			item := models.StockItem{
				StockID:        stock.ID,
				ProductID:      reqItem.ProductID,
				SizeGroupID:    reqItem.SizeGroupID,
				PurchaseItemID: reqItem.PurchaseItemID,
				ItemOrder:      reqItem.ItemOrder,
				AdvicePrice:    reqItem.AdvicePrice,
				Discount:       reqItem.Discount,
				PurchasePrice:  reqItem.PurchasePrice,
				NonTaxPrice:    reqItem.NonTaxPrice,
				TotalQty:       totalQty,
				TotalAmount:    totalAmount,
				Supplement:     reqItem.Supplement,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			for _, s := range reqItem.Sizes {
				size := models.StockItemSize{
					StockItemID:  item.ID,
					SizeOptionID: s.SizeOptionID,
					Qty:          s.Qty,
				}
				if err := tx.Create(&size).Error; err != nil {
					return err
				}
			}
		}

		// 更新關聯採購單交貨狀態
		if stock.PurchaseID != nil {
			if err := delivery.UpdateDeliveryStatus(tx, *stock.PurchaseID); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("新增成功").SetData(stock).Send()
}

// UpdateStock 更新進貨單
func UpdateStock(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.Stock
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusNotFound, "進貨單不存在").Send()
		return
	}

	var req struct {
		StockDate       string  `json:"stock_date"`
		CustomerID      int64   `json:"customer_id"`
		VendorID        int64   `json:"vendor_id"`
		PurchaseID      *int64  `json:"purchase_id"`
		StockMode       int     `json:"stock_mode"`
		DealMode        int     `json:"deal_mode"`
		CurrencyCode    string  `json:"currency_code"`
		FillPersonID    *int64  `json:"fill_person_id"`
		CloseMonth      string  `json:"close_month"`
		Remark          string  `json:"remark"`
		TaxMode         int     `json:"tax_mode"`
		TaxRate         float64 `json:"tax_rate"`
		TaxAmount       float64 `json:"tax_amount"`
		DiscountPercent float64 `json:"discount_percent"`
		DiscountAmount  float64 `json:"discount_amount"`
		InvoiceDate     string  `json:"invoice_date"`
		InvoiceNo       string  `json:"invoice_no"`
		InvoiceAmount   float64 `json:"invoice_amount"`
		ChargeAmount    float64 `json:"charge_amount"`
		Items           []struct {
			ProductID      int64   `json:"product_id"`
			SizeGroupID    *int64  `json:"size_group_id"`
			PurchaseItemID *int64  `json:"purchase_item_id"`
			ItemOrder      int     `json:"item_order"`
			AdvicePrice    float64 `json:"advice_price"`
			Discount       float64 `json:"discount"`
			PurchasePrice  float64 `json:"purchase_price"`
			NonTaxPrice    float64 `json:"non_tax_price"`
			Supplement     int     `json:"supplement"`
			Sizes          []struct {
				SizeOptionID int64 `json:"size_option_id"`
				Qty          int   `json:"qty"`
			} `json:"sizes"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 記錄舊的 PurchaseID，更新後可能需要同時更新兩張採購單狀態
	oldPurchaseID := existing.PurchaseID

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 刪除舊的 Sizes 和 Items
		var oldItemIDs []int64
		tx.Model(&models.StockItem{}).Where("stock_id = ?", id).Pluck("id", &oldItemIDs)
		if len(oldItemIDs) > 0 {
			tx.Where("stock_item_id IN ?", oldItemIDs).Delete(&models.StockItemSize{})
		}
		tx.Where("stock_id = ?", id).Delete(&models.StockItem{})

		// 系統紀錄者
		adminId, _ := c.Get("AdminId")
		recorderID := existing.RecorderID
		if aid, ok := adminId.(float64); ok {
			recorderID = int64(aid)
		}

		// 更新主表
		updates := map[string]interface{}{
			"stock_date":       req.StockDate,
			"customer_id":      req.CustomerID,
			"vendor_id":        req.VendorID,
			"purchase_id":      req.PurchaseID,
			"stock_mode":       req.StockMode,
			"deal_mode":        req.DealMode,
			"currency_code":    req.CurrencyCode,
			"fill_person_id":   req.FillPersonID,
			"recorder_id":      recorderID,
			"close_month":      req.CloseMonth,
			"remark":           req.Remark,
			"tax_mode":         req.TaxMode,
			"tax_rate":         req.TaxRate,
			"tax_amount":       req.TaxAmount,
			"discount_percent": req.DiscountPercent,
			"discount_amount":  req.DiscountAmount,
			"invoice_date":     req.InvoiceDate,
			"invoice_no":       req.InvoiceNo,
			"invoice_amount":   req.InvoiceAmount,
			"charge_amount":    req.ChargeAmount,
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

			item := models.StockItem{
				StockID:        id,
				ProductID:      reqItem.ProductID,
				SizeGroupID:    reqItem.SizeGroupID,
				PurchaseItemID: reqItem.PurchaseItemID,
				ItemOrder:      reqItem.ItemOrder,
				AdvicePrice:    reqItem.AdvicePrice,
				Discount:       reqItem.Discount,
				PurchasePrice:  reqItem.PurchasePrice,
				NonTaxPrice:    reqItem.NonTaxPrice,
				TotalQty:       totalQty,
				TotalAmount:    totalAmount,
				Supplement:     reqItem.Supplement,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			for _, s := range reqItem.Sizes {
				size := models.StockItemSize{
					StockItemID:  item.ID,
					SizeOptionID: s.SizeOptionID,
					Qty:          s.Qty,
				}
				if err := tx.Create(&size).Error; err != nil {
					return err
				}
			}
		}

		// 更新關聯採購單交貨狀態
		if req.PurchaseID != nil {
			if err := delivery.UpdateDeliveryStatus(tx, *req.PurchaseID); err != nil {
				return err
			}
		}
		// 如果舊的採購單跟新的不同，也要更新舊的
		if oldPurchaseID != nil && (req.PurchaseID == nil || *oldPurchaseID != *req.PurchaseID) {
			if err := delivery.UpdateDeliveryStatus(tx, *oldPurchaseID); err != nil {
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

// DeleteStock 軟刪除進貨單
func DeleteStock(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	// 先取得進貨單，確認是否有關聯採購單
	var stock models.Stock
	if err := db.GetRead().Where("id = ?", id).First(&stock).Error; err != nil {
		resp.Fail(http.StatusNotFound, "進貨單不存在").Send()
		return
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.Stock{}, id).Error; err != nil {
			return err
		}
		// 更新關聯採購單交貨狀態
		if stock.PurchaseID != nil {
			if err := delivery.UpdateDeliveryStatus(tx, *stock.PurchaseID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("刪除成功").Send()
}

// SearchPurchases 搜尋採購單（供進貨單選擇關聯採購）
// 回傳 { purchases: [...], delivered: { "itemId-sizeOptionId": qty } }
func SearchPurchases(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	query := db.GetRead().
		Where("delivery_status < 2"). // 排除已交齊
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("item_order ASC")
		}).
		Preload("Items.Product.Size1Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Product.Size2Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Product.Size3Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Product.CategoryMaps", func(db *gorm.DB) *gorm.DB {
			return db.Where("category_type = 5")
		}).
		Preload("Items.Product.CategoryMaps.Category5").
		Preload("Items.Product.ProductVendors").
		Preload("Items.SizeGroup.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Sizes.SizeOption").
		Order("purchase_date DESC, id DESC")

	if v := c.Query("vendor_id"); v != "" {
		query = query.Where("vendor_id = ?", v)
	}
	if v := c.Query("customer_id"); v != "" {
		query = query.Where("customer_id = ?", v)
	}
	if v := c.Query("search"); v != "" {
		query = ApplySearch(query, v, "purchase_no")
	}

	var purchases []models.Purchase
	query.Limit(20).Find(&purchases)

	// 收集所有 purchase_item_id，查已進貨數量
	var allItemIDs []int64
	for _, p := range purchases {
		for _, item := range p.Items {
			allItemIDs = append(allItemIDs, item.ID)
		}
	}

	delivered := delivery.DeliveredQtyMap(db.GetRead(), allItemIDs)

	resp.Success("成功").SetData(map[string]interface{}{
		"purchases": purchases,
		"delivered": delivered,
	}).Send()
}
