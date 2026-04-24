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

	// 查已進貨數量（供前端顯示採購未交量）
	var allItemIDs []int64
	for _, it := range item.Items {
		allItemIDs = append(allItemIDs, it.ID)
	}
	delivered := delivery.DeliveredQtyMap(db.GetRead(), allItemIDs)

	// 查關聯進貨單號（每個 purchase_item 對應的 stock_no 列表）
	type stockInfo struct {
		PurchaseItemID int64
		StockNo        string
	}
	var stockInfos []stockInfo
	if len(allItemIDs) > 0 {
		db.GetRead().Model(&models.StockItem{}).
			Select("stock_items.purchase_item_id, stocks.stock_no").
			Joins("JOIN stocks ON stocks.id = stock_items.stock_id AND stocks.deleted_at IS NULL").
			Where("stock_items.purchase_item_id IN ?", allItemIDs).
			Scan(&stockInfos)
	}
	stockNos := map[int64][]string{}
	for _, si := range stockInfos {
		stockNos[si.PurchaseItemID] = append(stockNos[si.PurchaseItemID], si.StockNo)
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"purchase":  item,
		"delivered": delivered,
		"stock_nos": stockNos,
	}).Send()
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
		CurrencyCode     string  `json:"currency_code"`
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
			CancelFlag    int     `json:"cancel_flag"`
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
	db.GetRead().Unscoped().Model(&models.Purchase{}).
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
		CurrencyCode:     req.CurrencyCode,
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

			cancelFlag := reqItem.CancelFlag
			if cancelFlag == 0 {
				cancelFlag = 1
			}

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
				CancelFlag:    cancelFlag,
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
		CurrencyCode     string  `json:"currency_code"`
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
			CancelFlag    int     `json:"cancel_flag"`
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
			"currency_code":     req.CurrencyCode,
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

			cancelFlag := reqItem.CancelFlag
			if cancelFlag <= 0 {
				cancelFlag = 1
			}

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
				CancelFlag:    cancelFlag,
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

// StopPurchase 停交：將所有明細 cancel_flag 設為 2(停交)，並更新交貨狀態
func StopPurchase(c *gin.Context) {
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

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.PurchaseItem{}).Where("purchase_id = ? AND cancel_flag < 2", id).Update("cancel_flag", 2).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Purchase{}).Where("id = ?", id).Update("is_stopped", true).Error; err != nil {
			return err
		}
		return delivery.UpdateDeliveryStatus(tx, id)
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("停交成功").Send()
}

// SearchPurchaseItems 搜尋廠商未交齊的採購明細（供進貨單逐筆選擇商品用）
// 同一型號可能來自多張採購單，每筆都獨立列出
func SearchPurchaseItems(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	vendorID := c.Query("vendor_id")
	if vendorID == "" {
		resp.Fail(400, "請提供 vendor_id").Send()
		return
	}

	// 查該廠商未交齊且未停交的採購單中，未停交明細
	query := db.GetRead().
		Where("purchase_items.cancel_flag < 2").
		Joins("JOIN purchases ON purchases.id = purchase_items.purchase_id AND purchases.deleted_at IS NULL AND purchases.delivery_status < 2 AND purchases.vendor_id = ?", vendorID).
		Preload("Product").
		Preload("Product.Size1Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Product.Size2Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Product.Size3Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Product.CategoryMaps", func(db *gorm.DB) *gorm.DB {
			return db.Where("category_type = 5")
		}).
		Preload("Product.CategoryMaps.Category5").
		Preload("SizeGroup.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Sizes").
		Order("purchase_items.id DESC")

	if v := c.Query("customer_id"); v != "" {
		query = query.Where("purchases.customer_id = ?", v)
	}

	// 型號搜尋
	if v := c.Query("search"); v != "" {
		like := "%" + v + "%"
		query = query.Where("purchase_items.product_id IN (SELECT id FROM products WHERE deleted_at IS NULL AND (model_code ILIKE ? OR name_spec ILIKE ?))", like, like)
	}

	var items []models.PurchaseItem
	if err := query.Limit(50).Find(&items).Error; err != nil {
		resp.Panic(err)
		return
	}

	// 查每筆明細的採購單號與幣別（幣別用於前端換算）
	type purchaseRef struct {
		ID           int64
		PurchaseNo   string
		CurrencyCode string
	}
	var purchaseIDs []int64
	for _, item := range items {
		purchaseIDs = append(purchaseIDs, item.PurchaseID)
	}
	purchaseNoMap := map[int64]string{}
	purchaseCurrencyMap := map[int64]string{}
	if len(purchaseIDs) > 0 {
		var refs []purchaseRef
		if err := db.GetRead().Model(&models.Purchase{}).Select("id, purchase_no, currency_code").Where("id IN ?", purchaseIDs).Scan(&refs).Error; err != nil {
			resp.Panic(err)
			return
		}
		for _, r := range refs {
			purchaseNoMap[r.ID] = r.PurchaseNo
			purchaseCurrencyMap[r.ID] = r.CurrencyCode
		}
	}

	// 查已進貨數量
	var allItemIDs []int64
	for _, item := range items {
		allItemIDs = append(allItemIDs, item.ID)
	}
	delivered := delivery.DeliveredQtyMap(db.GetRead(), allItemIDs)

	resp.Success("成功").SetData(map[string]interface{}{
		"items":                 items,
		"purchase_no_map":       purchaseNoMap,
		"purchase_currency_map": purchaseCurrencyMap,
		"delivered":             delivered,
		"stock_map":             map[string]int{},
	}).Send()
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
// 快取策略（方案 A）：商品主檔（不含 SizeStocks）走 Redis，庫存與訂貨明細每次即時查 DB
func SearchProducts(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	search := c.Query("search")
	vendorID := c.Query("vendor_id")
	brandID := c.Query("brand_id")
	customerID := c.Query("customer_id")
	storeCode := c.Query("store_code")
	orderContext := c.Query("order_context") // "1" = 訂貨情境：回傳 supplement_info 供舖/補 與批價預設判定

	// --- 商品主檔：先查 Redis，miss 再打 DB ---
	var items []models.Product
	if !getListCache(c, &items) {
		query := db.GetRead().Order("id ASC")
		if vendorID != "" {
			query = query.Where("id IN (SELECT product_id FROM product_vendors WHERE vendor_id = ?)", vendorID)
		}
		if brandID != "" {
			query = query.Where("brand_id = ?", brandID)
		}
		if customerID != "" && vendorID == "" && brandID == "" && orderContext != "1" {
			// 出貨用：依客戶的訂貨明細找商品（訂貨情境不套用，訂貨時未選品牌應顯示全部商品）
			query = query.Where("id IN (SELECT DISTINCT oi.product_id FROM order_items oi JOIN orders o ON o.id = oi.order_id AND o.deleted_at IS NULL WHERE o.customer_id = ?)", customerID)
		}
		if search != "" {
			like := "%" + search + "%"
			query = query.Where("model_code ILIKE ? OR name_spec ILIKE ?", like, like)
		}

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

		setListCacheRaw(c, items)
	}

	// --- 即時查庫存（不快取）並掛回每個 product 的 SizeStocks ---
	if (storeCode != "" || customerID != "") && len(items) > 0 {
		productIDs := make([]int64, 0, len(items))
		for _, p := range items {
			productIDs = append(productIDs, p.ID)
		}
		var stocks []models.ProductSizeStock
		stockQ := db.GetRead().Where("product_id IN ?", productIDs)
		if storeCode != "" {
			stockQ = stockQ.Where("customer_id IN (SELECT id FROM retail_customers WHERE branch_code = ?)", storeCode)
		} else if customerID != "" {
			stockQ = stockQ.Where("customer_id = ?", customerID)
		}
		stockQ.Find(&stocks)
		stockMap := map[int64][]models.ProductSizeStock{}
		for _, s := range stocks {
			stockMap[s.ProductID] = append(stockMap[s.ProductID], s)
		}
		for i := range items {
			items[i].SizeStocks = stockMap[items[i].ID]
		}
	}

	// --- 即時查訂貨明細（不快取）：出貨用，帶入售價/數量（訂貨情境走下方 supplement_info 分支）---
	if customerID != "" && orderContext != "1" {
		var productIDs []int64
		for _, p := range items {
			productIDs = append(productIDs, p.ID)
		}
		if len(productIDs) > 0 {
			var orderItems []models.OrderItem
			db.GetRead().
				Preload("Sizes").
				Joins("JOIN orders ON orders.id = order_items.order_id AND orders.deleted_at IS NULL AND orders.customer_id = ?", customerID).
				Where("order_items.product_id IN ?", productIDs).
				Order("orders.order_date DESC, order_items.id DESC").
				Find(&orderItems)

			// 每個 product 取最新一筆 orderItem
			orderItemMap := map[int64]models.OrderItem{}
			for _, oi := range orderItems {
				if _, exists := orderItemMap[oi.ProductID]; !exists {
					orderItemMap[oi.ProductID] = oi
				}
			}

			resp.Success("成功").SetData(map[string]interface{}{
				"products":    items,
				"order_items": orderItemMap,
			}).Send()
			return
		}
	}

	// --- 訂貨情境：回傳 supplement_info（舖/補 判定 + 第一次舖的 order_price）---
	if orderContext == "1" && customerID != "" && len(items) > 0 {
		productIDs := make([]int64, 0, len(items))
		for _, p := range items {
			productIDs = append(productIDs, p.ID)
		}

		// (a) 出貨歷史：客戶 × 型號 是否有任何未刪除的出貨明細
		type shipRow struct {
			ProductID int64
		}
		var shipRows []shipRow
		db.GetRead().Table("shipment_items AS si").
			Select("DISTINCT si.product_id").
			Joins("JOIN shipments s ON s.id = si.shipment_id AND s.deleted_at IS NULL").
			Where("s.customer_id = ? AND si.product_id IN ?", customerID, productIDs).
			Scan(&shipRows)
		hasShipmentSet := map[int64]bool{}
		for _, r := range shipRows {
			hasShipmentSet[r.ProductID] = true
		}

		// (b) 最早一次「舖」的 order_price（依 order_date ASC, id ASC）
		type priceRow struct {
			ProductID  int64
			OrderPrice float64
		}
		var priceRows []priceRow
		db.GetRead().Raw(`
			SELECT DISTINCT ON (oi.product_id) oi.product_id, oi.order_price
			FROM order_items oi
			JOIN orders o ON o.id = oi.order_id AND o.deleted_at IS NULL
			WHERE o.customer_id = ? AND oi.product_id IN (?) AND oi.supplement = 1
			ORDER BY oi.product_id, o.order_date ASC, oi.id ASC
		`, customerID, productIDs).Scan(&priceRows)
		firstPuPriceMap := map[int64]float64{}
		for _, r := range priceRows {
			firstPuPriceMap[r.ProductID] = r.OrderPrice
		}

		type supplementInfo struct {
			HasShipmentHistory bool    `json:"has_shipment_history"`
			FirstPuPrice       float64 `json:"first_pu_price"`
		}
		infoMap := map[int64]supplementInfo{}
		for _, pid := range productIDs {
			infoMap[pid] = supplementInfo{
				HasShipmentHistory: hasShipmentSet[pid],
				FirstPuPrice:       firstPuPriceMap[pid],
			}
		}

		resp.Success("成功").SetData(map[string]interface{}{
			"products":        items,
			"supplement_info": infoMap,
		}).Send()
		return
	}

	resp.Success("成功").SetData(items).Send()
}
