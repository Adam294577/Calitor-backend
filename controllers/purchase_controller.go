package controllers

import (
	"fmt"
	"math"
	"net/http"
	"project/models"
	"project/services/delivery"
	"project/services/purchase"
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
		Joins("JOIN retail_customers ON retail_customers.id = purchases.customer_id AND retail_customers.is_visible = true").
		Preload("Customer").
		Preload("Vendor").
		Order("purchases.purchase_date DESC, purchases.id DESC")

	query = ApplySearch(query, c.Query("search"), "purchases.purchase_no")

	if v := c.Query("customer_id"); v != "" {
		query = query.Where("purchases.customer_id = ?", v)
	}
	if v := c.Query("vendor_id"); v != "" {
		query = query.Where("purchases.vendor_id = ?", v)
	}
	if v := c.Query("date_from"); v != "" {
		query = query.Where("purchases.purchase_date >= ?", v)
	}
	if v := c.Query("date_to"); v != "" {
		query = query.Where("purchases.purchase_date <= ?", v)
	}
	if v := c.Query("deal_mode"); v != "" {
		query = query.Where("purchases.deal_mode = ?", v)
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
		Joins("JOIN retail_customers ON retail_customers.id = purchases.customer_id AND retail_customers.is_visible = true").
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
		Where("purchases.id = ?", id).
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

	// 此廠商對所有型號的跨單總未交量（排除本單，前端 form 量會自行疊加）
	vendorOutstanding := VendorProductOutstandingMap(db.GetRead(), item.VendorID, item.ID)

	// can_change_party：本單尚未被任何進貨單引用時才允許切換採購庫點／廠商
	canChangeParty := !purchaseHasStockLink(db.GetRead(), item.ID)

	resp.Success("成功").SetData(map[string]interface{}{
		"purchase":           item,
		"delivered":          delivered,
		"stock_nos":          stockNos,
		"vendor_outstanding": vendorOutstanding,
		"can_change_party":   canChangeParty,
	}).Send()
}

// purchaseHasStockLink 判斷某採購單是否已被任何（未刪除的）進貨單引用，
// 是放寬「編輯時切換廠商／庫點」的鎖：一旦有進貨紀錄就不允許更換源頭。
func purchaseHasStockLink(db *gorm.DB, purchaseID int64) bool {
	var count int64
	db.Model(&models.StockItem{}).
		Joins("JOIN purchase_items ON purchase_items.id = stock_items.purchase_item_id").
		Joins("JOIN stocks ON stocks.id = stock_items.stock_id AND stocks.deleted_at IS NULL").
		Where("purchase_items.purchase_id = ?", purchaseID).
		Count(&count)
	return count > 0
}

// VendorProductOutstandingMap 查詢指定廠商跨所有未結清採購單，依
// (product_id, size_option_id) 加總的「總未交量」。
//   - 排除 cancel_flag IN (2, 3)（停交/取消，與 GetPurchaseOutstanding 一致）
//   - 排除 purchases.delivery_status >= 2（已交齊）
//   - excludePurchaseID > 0 時排除該採購單（編輯模式排除自己，避免與前端 form 量重複計算）
//   - 回傳 map["productID-sizeOptionID"] = outstandingQty（>0）
func VendorProductOutstandingMap(db *gorm.DB, vendorID int64, excludePurchaseID int64) map[string]int {
	result := map[string]int{}
	if vendorID <= 0 {
		return result
	}

	type row struct {
		PurchaseItemID int64
		ProductID      int64
		SizeOptionID   int64
		Qty            int
	}
	var rows []row
	q := db.Model(&models.PurchaseItemSize{}).
		Select("purchase_item_sizes.purchase_item_id, purchase_items.product_id, purchase_item_sizes.size_option_id, purchase_item_sizes.qty").
		Joins("JOIN purchase_items ON purchase_items.id = purchase_item_sizes.purchase_item_id").
		Joins("JOIN purchases ON purchases.id = purchase_items.purchase_id AND purchases.deleted_at IS NULL").
		Where("purchases.vendor_id = ?", vendorID).
		Where("purchases.delivery_status < 2").
		Where("purchase_items.cancel_flag NOT IN (2, 3)")
	if excludePurchaseID > 0 {
		q = q.Where("purchases.id <> ?", excludePurchaseID)
	}
	q.Scan(&rows)

	if len(rows) == 0 {
		return result
	}

	idSet := map[int64]bool{}
	for _, r := range rows {
		idSet[r.PurchaseItemID] = true
	}
	itemIDs := make([]int64, 0, len(idSet))
	for id := range idSet {
		itemIDs = append(itemIDs, id)
	}
	deliveredMap := delivery.DeliveredQtyMap(db, itemIDs)

	for _, r := range rows {
		delKey := fmt.Sprintf("%d-%d", r.PurchaseItemID, r.SizeOptionID)
		outstanding := r.Qty - deliveredMap[delKey]
		if outstanding <= 0 {
			continue
		}
		aggKey := fmt.Sprintf("%d-%d", r.ProductID, r.SizeOptionID)
		result[aggKey] += outstanding
	}
	return result
}

// GetVendorPurchaseOutstandingMap 取得指定廠商的型號跨單總未交量 map
// 用途：新增採購單 / 切換廠商時即時抓取
// 可選 query: exclude_purchase_id=N → 排除某張採購單（編輯模式自身）
func GetVendorPurchaseOutstandingMap(c *gin.Context) {
	resp := response.New(c)
	vendorID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的廠商 ID").Send()
		return
	}

	excludePurchaseID, _ := strconv.ParseInt(c.Query("exclude_purchase_id"), 10, 64)

	db := models.PostgresNew()
	defer db.Close()

	m := VendorProductOutstandingMap(db.GetRead(), vendorID, excludePurchaseID)
	resp.Success("成功").SetData(map[string]interface{}{
		"vendor_outstanding": m,
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

	// 查詢客戶 BranchCode(同時驗證 is_visible)
	customerPtr, cerr := EnsureCustomerVisible(db.GetRead(), req.CustomerID)
	if cerr != nil {
		resp.Fail(http.StatusBadRequest, ErrMsgCustomerNotVisible).Send()
		return
	}
	customer := *customerPtr

	// 產生採購單號: VendorCode + BranchCode + YYYYMM + 流水號4碼
	yyyymm := ""
	if len(req.PurchaseDate) >= 6 {
		yyyymm = req.PurchaseDate[:6]
	}
	prefix := vendor.Code + customer.BranchCode + yyyymm

	var maxNo string
	db.GetRead().Unscoped().Model(&models.Purchase{}).
		Where("purchase_no LIKE ?", prefix+"%").
		Select("COALESCE(MAX(purchase_no), '')").
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
		// 後端依 model_code 自然序重排,忽略前端送的 item_order
		pids := make([]int64, len(req.Items))
		for i, it := range req.Items {
			pids[i] = it.ProductID
		}
		order := ReorderItemsByModelCode(tx, pids)
		for newOrder, origIdx := range order {
			reqItem := req.Items[origIdx]
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := math.Round(float64(totalQty) * reqItem.PurchasePrice)

			cancelFlag := reqItem.CancelFlag
			if cancelFlag == 0 {
				cancelFlag = 1
			}

			item := models.PurchaseItem{
				PurchaseID:    purchase.ID,
				ProductID:     reqItem.ProductID,
				SizeGroupID:   reqItem.SizeGroupID,
				ItemOrder:     newOrder,
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

	if req.CustomerID != 0 {
		if _, verr := EnsureCustomerVisible(db.GetRead(), req.CustomerID); verr != nil {
			resp.Fail(http.StatusBadRequest, ErrMsgCustomerNotVisible).Send()
			return
		}
	}

	// 變更採購庫點／廠商時，本單必須尚未被任何進貨單引用
	customerChanged := req.CustomerID != 0 && req.CustomerID != existing.CustomerID
	vendorChanged := req.VendorID != 0 && req.VendorID != existing.VendorID
	if customerChanged || vendorChanged {
		if purchaseHasStockLink(db.GetRead(), id) {
			resp.Fail(http.StatusBadRequest, "本單已有進貨紀錄，無法更換採購庫點／廠商").Send()
			return
		}
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

		// 重建 Items + Sizes — 後端依 model_code 自然序重排,忽略前端 item_order
		pids := make([]int64, len(req.Items))
		for i, it := range req.Items {
			pids[i] = it.ProductID
		}
		order := ReorderItemsByModelCode(tx, pids)
		for newOrder, origIdx := range order {
			reqItem := req.Items[origIdx]
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := math.Round(float64(totalQty) * reqItem.PurchasePrice)

			cancelFlag := reqItem.CancelFlag
			if cancelFlag <= 0 {
				cancelFlag = 1
			}

			item := models.PurchaseItem{
				PurchaseID:    id,
				ProductID:     reqItem.ProductID,
				SizeGroupID:   reqItem.SizeGroupID,
				ItemOrder:     newOrder,
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
		// 編輯介面可能透過「清除」欄把某些 item 改為 cancel_flag=2,需同步重算
		// is_stopped(全部未停 item 都停才 true)與 delivery_status,
		// 否則列表「停交」按鈕(綁 is_stopped)disable 不會生效。
		var activeCount int64
		if err := tx.Model(&models.PurchaseItem{}).Where("purchase_id = ? AND cancel_flag < 2", id).Count(&activeCount).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Purchase{}).Where("id = ?", id).Update("is_stopped", activeCount == 0).Error; err != nil {
			return err
		}
		return delivery.UpdateDeliveryStatus(tx, id)
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

	adminId, _ := c.Get("AdminId")
	recorderID := existing.RecorderID
	if aid, ok := adminId.(float64); ok {
		recorderID = int64(aid)
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		return purchase.Stop(tx, id, recorderID)
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("停交成功").Send()
}

// StopPurchaseItems 逐列停交：只將指定 product_ids 對應的明細 cancel_flag 設為 2
// 與 StopPurchase 不同：StopPurchase 停整張採購單所有明細；本端點只停指定 product_id 的明細列。
// 給「採購未交統計」按下停按鈕時使用，避免把採購單裡其他型號一起停掉。
func StopPurchaseItems(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	var body struct {
		ProductIDs []int64 `json:"product_ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		resp.Fail(http.StatusBadRequest, "請求格式錯誤").Send()
		return
	}
	if len(body.ProductIDs) == 0 {
		resp.Fail(http.StatusBadRequest, "必須指定 product_ids").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.Purchase
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusNotFound, "採購單不存在").Send()
		return
	}

	adminId, _ := c.Get("AdminId")
	recorderID := existing.RecorderID
	if aid, ok := adminId.(float64); ok {
		recorderID = int64(aid)
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		return purchase.StopItems(tx, id, body.ProductIDs, recorderID)
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
	vendorID := c.Query("vendor_id")
	if vendorID == "" {
		resp.Fail(400, "請提供 vendor_id").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	result, err := purchase.SearchItems(db.GetRead(), vendorID, c.Query("customer_id"), c.Query("search"))
	if err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("成功").SetData(result).Send()
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
		query := db.GetRead().Order(ModelCodeOrderBy("model_code"))
		if vendorID != "" {
			query = query.Where("id IN (SELECT product_id FROM product_vendors WHERE vendor_id = ?)", vendorID)
		}
		if brandID != "" {
			query = query.Where("brand_id = ?", brandID)
		}
		if search != "" {
			// 商品列的模糊搜尋只用型號 (使用者偏好,品名規格不參與)
			query = query.Where("model_code ILIKE ?", "%"+search+"%")
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
			Limit(100).
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

	// --- 訂貨情境：回傳 supplement_info（舖/補 判定 + 最近一筆未取消訂貨明細的 order_price）---
	if orderContext == "1" && customerID != "" && len(items) > 0 {
		productIDs := make([]int64, 0, len(items))
		for _, p := range items {
			productIDs = append(productIDs, p.ID)
		}

		// (a) 訂貨歷史:客戶 × 型號 是否有任何未取消的訂貨明細(cancel_flag<2)
		//     有 → 下一張視為「補」(2);無 → 視為「舖」(1)
		type orderHistRow struct {
			ProductID int64
		}
		var orderHistRows []orderHistRow
		db.GetRead().Table("order_items AS oi").
			Select("DISTINCT oi.product_id").
			Joins("JOIN orders o ON o.id = oi.order_id AND o.deleted_at IS NULL").
			Where("o.customer_id = ? AND oi.product_id IN ? AND oi.cancel_flag < 2", customerID, productIDs).
			Scan(&orderHistRows)
		hasOrderHistorySet := map[int64]bool{}
		for _, r := range orderHistRows {
			hasOrderHistorySet[r.ProductID] = true
		}

		// (b) 最近一筆未取消訂貨明細的 non_tax_price (canonical 未稅基底,依 order_date DESC, id DESC)
		// 前端依 form.tax_mode/tax_rate 自行換算成 order_price/ship_price 顯示值
		type priceRow struct {
			ProductID   int64
			NonTaxPrice float64
		}
		var priceRows []priceRow
		db.GetRead().Raw(`
			SELECT DISTINCT ON (oi.product_id) oi.product_id, oi.non_tax_price
			FROM order_items oi
			JOIN orders o ON o.id = oi.order_id AND o.deleted_at IS NULL
			WHERE o.customer_id = ? AND oi.product_id IN (?) AND oi.cancel_flag < 2
			ORDER BY oi.product_id, o.order_date DESC, oi.id DESC
		`, customerID, productIDs).Scan(&priceRows)
		recentNonTaxPriceMap := map[int64]float64{}
		for _, r := range priceRows {
			recentNonTaxPriceMap[r.ProductID] = r.NonTaxPrice
		}

		type supplementInfo struct {
			HasOrderHistory   bool    `json:"has_order_history"`
			RecentNonTaxPrice float64 `json:"recent_non_tax_price"`
		}
		infoMap := map[int64]supplementInfo{}
		for _, pid := range productIDs {
			infoMap[pid] = supplementInfo{
				HasOrderHistory:   hasOrderHistorySet[pid],
				RecentNonTaxPrice: recentNonTaxPriceMap[pid],
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
