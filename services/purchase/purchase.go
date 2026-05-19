package purchase

import (
	"project/models"
	"project/services/delivery"

	"gorm.io/gorm"
)

// Stop 停交採購單：將未停交明細標記 cancel_flag=2、採購單標為 is_stopped、寫入異動人，並重算 delivery 狀態。
// 呼叫端必須傳入 Transaction 的 tx（避免部分寫入）。recorderID 為 0 時不覆寫原值。
func Stop(tx *gorm.DB, purchaseID int64, recorderID int64) error {
	if err := tx.Model(&models.PurchaseItem{}).Where("purchase_id = ? AND cancel_flag < 2", purchaseID).Update("cancel_flag", 2).Error; err != nil {
		return err
	}
	updates := map[string]interface{}{"is_stopped": true}
	if recorderID != 0 {
		updates["recorder_id"] = recorderID
	}
	if err := tx.Model(&models.Purchase{}).Where("id = ?", purchaseID).Updates(updates).Error; err != nil {
		return err
	}
	return delivery.UpdateDeliveryStatus(tx, purchaseID)
}

// StopItems 逐列停交：只將指定 product_ids 對應的明細 cancel_flag 設為 2。
// 與 Stop 不同：Stop 停整張採購單所有明細；本函式只停指定 product_id 的明細列。
// 給「採購未交統計」按下停按鈕時使用，避免把採購單裡其他型號一起停掉。
// is_stopped 旗標會依「剩餘未停 item 數是否為 0」重算（採購單列表頁的停交按鈕綁此旗標 disable）。
// 呼叫端必須傳入 Transaction 的 tx。recorderID 為 0 時不覆寫原值。
func StopItems(tx *gorm.DB, purchaseID int64, productIDs []int64, recorderID int64) error {
	if len(productIDs) == 0 {
		return nil
	}
	if err := tx.Model(&models.PurchaseItem{}).Where("purchase_id = ? AND product_id IN ? AND cancel_flag < 2", purchaseID, productIDs).Update("cancel_flag", 2).Error; err != nil {
		return err
	}
	// 重算 is_stopped：只有「沒有任何未停明細」才視為整單停交
	var activeCount int64
	if err := tx.Model(&models.PurchaseItem{}).Where("purchase_id = ? AND cancel_flag < 2", purchaseID).Count(&activeCount).Error; err != nil {
		return err
	}
	updates := map[string]interface{}{"is_stopped": activeCount == 0}
	if recorderID != 0 {
		updates["recorder_id"] = recorderID
	}
	if err := tx.Model(&models.Purchase{}).Where("id = ?", purchaseID).Updates(updates).Error; err != nil {
		return err
	}
	return delivery.UpdateDeliveryStatus(tx, purchaseID)
}

// RecentPriceResult 為 RecentPrice 的回傳值，可直接作為 JSON response data。
type RecentPriceResult struct {
	PurchasePrice float64 `json:"purchase_price"`
	CurrencyCode  string  `json:"currency_code"`
	Source        string  `json:"source"` // "vendor" | "product" | "empty"
	Hint          string  `json:"hint"`
}

// RecentPrice 以 (vendor, product) 三層 fallback 取得進貨切廠商時的參考原幣價：
// 1. product_vendors 該廠商對此商品設定的原幣價（> 0 才採用）
// 2. 商品建檔 OriginalPrice
// 3. 空值
// 進貨流程不參考歷史採購價（採購單沒打就只看主檔建立的價）。
// 任一層 DB 查詢失敗皆 swallow，往下一層 fallback。
// sizeOptionID 保留參數簽名供將來尺寸層級擴充用，目前未使用。
func RecentPrice(db *gorm.DB, vendorID, productID, sizeOptionID int64) *RecentPriceResult {
	_ = sizeOptionID

	// Layer 1: 廠商對此商品有設定原幣價，且 > 0 才採用（欄位為後加，可能是 0/NULL）
	var pv models.ProductVendor
	if err := db.Where("product_id = ? AND vendor_id = ?", productID, vendorID).First(&pv).Error; err == nil && pv.OriginalPrice > 0 {
		return &RecentPriceResult{
			PurchasePrice: pv.OriginalPrice,
			CurrencyCode:  "RMB",
			Source:        "vendor",
			Hint:          "廠商商品設定原幣價",
		}
	}

	// Layer 2: 商品主檔
	var product models.Product
	if err := db.Where("id = ?", productID).First(&product).Error; err == nil && product.OriginalPrice > 0 {
		return &RecentPriceResult{
			PurchasePrice: product.OriginalPrice,
			CurrencyCode:  "RMB",
			Source:        "product",
			Hint:          "商品建檔原幣價",
		}
	}

	return &RecentPriceResult{
		PurchasePrice: 0,
		CurrencyCode:  "",
		Source:        "empty",
		Hint:          "",
	}
}

// SearchItemsResult 為 SearchItems 的回傳值，可直接作為 JSON response data。
// stock_map 保留為空 map（維持既有 API 契約，前端已依賴）。
type SearchItemsResult struct {
	Items               []models.PurchaseItem `json:"items"`
	PurchaseNoMap       map[int64]string      `json:"purchase_no_map"`
	PurchaseCurrencyMap map[int64]string      `json:"purchase_currency_map"`
	Delivered           map[string]int        `json:"delivered"`
	StockMap            map[string]int        `json:"stock_map"`
}

// SearchItems 搜尋指定廠商未交齊、未停交的採購明細（供進貨單選擇商品用）。
// vendorID 必填；customerID / search 為空字串時不過濾。
// Limit 50 筆（與既有 controller 行為一致）。
func SearchItems(db *gorm.DB, vendorID, customerID, search string) (*SearchItemsResult, error) {
	query := db.
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

	if customerID != "" {
		query = query.Where("purchases.customer_id = ?", customerID)
	}
	if search != "" {
		like := "%" + search + "%"
		query = query.Where("purchase_items.product_id IN (SELECT id FROM products WHERE deleted_at IS NULL AND (model_code ILIKE ? OR name_spec ILIKE ?))", like, like)
	}

	var items []models.PurchaseItem
	if err := query.Limit(50).Find(&items).Error; err != nil {
		return nil, err
	}

	type purchaseRef struct {
		ID           int64
		PurchaseNo   string
		CurrencyCode string
	}
	purchaseNoMap := map[int64]string{}
	purchaseCurrencyMap := map[int64]string{}

	if len(items) > 0 {
		purchaseIDs := make([]int64, 0, len(items))
		for _, item := range items {
			purchaseIDs = append(purchaseIDs, item.PurchaseID)
		}
		var refs []purchaseRef
		if err := db.Model(&models.Purchase{}).Select("id, purchase_no, currency_code").Where("id IN ?", purchaseIDs).Scan(&refs).Error; err != nil {
			return nil, err
		}
		for _, r := range refs {
			purchaseNoMap[r.ID] = r.PurchaseNo
			purchaseCurrencyMap[r.ID] = r.CurrencyCode
		}
	}

	allItemIDs := make([]int64, 0, len(items))
	for _, item := range items {
		allItemIDs = append(allItemIDs, item.ID)
	}
	delivered := delivery.DeliveredQtyMap(db, allItemIDs)

	return &SearchItemsResult{
		Items:               items,
		PurchaseNoMap:       purchaseNoMap,
		PurchaseCurrencyMap: purchaseCurrencyMap,
		Delivered:           delivered,
		StockMap:            map[string]int{},
	}, nil
}
