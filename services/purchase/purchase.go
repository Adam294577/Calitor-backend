package purchase

import (
	"fmt"
	"project/models"
	"project/services/delivery"

	"gorm.io/gorm"
)

// Stop 停交採購單：將未停交明細標記 cancel_flag=2、採購單標為 is_stopped，並重算 delivery 狀態。
// 呼叫端必須傳入 Transaction 的 tx（避免部分寫入）。
func Stop(tx *gorm.DB, purchaseID int64) error {
	if err := tx.Model(&models.PurchaseItem{}).Where("purchase_id = ? AND cancel_flag < 2", purchaseID).Update("cancel_flag", 2).Error; err != nil {
		return err
	}
	if err := tx.Model(&models.Purchase{}).Where("id = ?", purchaseID).Update("is_stopped", true).Error; err != nil {
		return err
	}
	return delivery.UpdateDeliveryStatus(tx, purchaseID)
}

// RecentPriceResult 為 RecentPrice 的回傳值，可直接作為 JSON response data。
type RecentPriceResult struct {
	PurchasePrice float64 `json:"purchase_price"`
	CurrencyCode  string  `json:"currency_code"`
	Source        string  `json:"source"` // "history" | "product" | "empty"
	Hint          string  `json:"hint"`
}

// RecentPrice 以 (vendor, product, sizeOption) 三層 fallback 取得參考採購價：
// 1. 該廠商對此 (product, size) 的最近一次歷史採購
// 2. 商品建檔 OriginalPrice
// 3. 空值
// 任一層 DB 查詢失敗皆 swallow，往下一層 fallback（維持既有行為）。
func RecentPrice(db *gorm.DB, vendorID, productID, sizeOptionID int64) *RecentPriceResult {
	type row struct {
		PurchasePrice float64
		PurchaseNo    string
		CurrencyCode  string
		PurchaseDate  string
	}
	var r row
	err := db.Table("purchase_items pi").
		Select("pi.purchase_price, p.purchase_no, p.currency_code, p.purchase_date").
		Joins("JOIN purchase_item_sizes pis ON pis.purchase_item_id = pi.id").
		Joins("JOIN purchases p ON p.id = pi.purchase_id AND p.deleted_at IS NULL").
		Where("p.vendor_id = ? AND pi.product_id = ? AND pis.size_option_id = ?", vendorID, productID, sizeOptionID).
		Order("p.purchase_date DESC, pi.id DESC").
		Limit(1).
		Scan(&r).Error
	if err == nil && r.PurchaseNo != "" {
		cc := r.CurrencyCode
		if cc == "" {
			cc = "RMB"
		}
		return &RecentPriceResult{
			PurchasePrice: r.PurchasePrice,
			CurrencyCode:  cc,
			Source:        "history",
			Hint:          fmt.Sprintf("來自採購單 %s", r.PurchaseNo),
		}
	}

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
