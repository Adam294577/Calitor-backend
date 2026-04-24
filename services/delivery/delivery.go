package delivery

import (
	"fmt"
	"project/models"

	"gorm.io/gorm"
)

// DeliveredQtyMap 查詢指定 purchaseItemIDs 的已進貨數量
// 回傳 map["purchaseItemID-sizeOptionID"] = deliveredQty
func DeliveredQtyMap(db *gorm.DB, purchaseItemIDs []int64) map[string]int {
	result := map[string]int{}
	if len(purchaseItemIDs) == 0 {
		return result
	}

	type row struct {
		PurchaseItemID int64
		SizeOptionID   int64
		TotalQty       int
	}
	var rows []row
	db.Model(&models.StockItemSize{}).
		Select("stock_items.purchase_item_id, stock_item_sizes.size_option_id, SUM(stock_item_sizes.qty) as total_qty").
		Joins("JOIN stock_items ON stock_items.id = stock_item_sizes.stock_item_id").
		Joins("JOIN stocks ON stocks.id = stock_items.stock_id AND stocks.deleted_at IS NULL").
		Where("stock_items.purchase_item_id IN ?", purchaseItemIDs).
		Group("stock_items.purchase_item_id, stock_item_sizes.size_option_id").
		Scan(&rows)

	for _, r := range rows {
		key := fmt.Sprintf("%d-%d", r.PurchaseItemID, r.SizeOptionID)
		result[key] = r.TotalQty
	}
	return result
}

// UpdateDeliveryStatus 比對採購單的採購量 vs 已進貨量，更新 Purchase.DeliveryStatus
// 呼叫時機：進貨新增/修改/刪除後
func UpdateDeliveryStatus(tx *gorm.DB, purchaseID int64) error {
	// 1. 查所有 PurchaseItemSize 的採購數量
	type sizeQty struct {
		PurchaseItemID int64
		SizeOptionID   int64
		Qty            int
	}

	// 檢查是否所有明細都已停交（cancel_flag >= 2），若是則視為已交齊
	var totalItemCount int64
	if err := tx.Model(&models.PurchaseItem{}).Where("purchase_id = ?", purchaseID).Count(&totalItemCount).Error; err != nil {
		return err
	}
	var stoppedItemCount int64
	if err := tx.Model(&models.PurchaseItem{}).Where("purchase_id = ? AND cancel_flag >= 2", purchaseID).Count(&stoppedItemCount).Error; err != nil {
		return err
	}
	if totalItemCount > 0 && stoppedItemCount == totalItemCount {
		return tx.Model(&models.Purchase{}).Where("id = ?", purchaseID).Update("delivery_status", 2).Error
	}

	// 只計算未停交的明細
	var purchaseSizes []sizeQty
	tx.Model(&models.PurchaseItemSize{}).
		Select("purchase_item_sizes.purchase_item_id, purchase_item_sizes.size_option_id, purchase_item_sizes.qty").
		Joins("JOIN purchase_items ON purchase_items.id = purchase_item_sizes.purchase_item_id").
		Where("purchase_items.purchase_id = ? AND purchase_items.cancel_flag < 2", purchaseID).
		Scan(&purchaseSizes)

	if len(purchaseSizes) == 0 {
		return tx.Model(&models.Purchase{}).Where("id = ?", purchaseID).Update("delivery_status", 0).Error
	}

	// 2. 收集 purchaseItemIDs，查已進貨數量
	idSet := map[int64]bool{}
	for _, ps := range purchaseSizes {
		idSet[ps.PurchaseItemID] = true
	}
	var itemIDs []int64
	for id := range idSet {
		itemIDs = append(itemIDs, id)
	}

	stockMap := DeliveredQtyMap(tx, itemIDs)

	// 3. 逐尺碼比對
	allDelivered := true
	anyDelivered := false
	for _, ps := range purchaseSizes {
		if ps.Qty <= 0 {
			continue
		}
		key := fmt.Sprintf("%d-%d", ps.PurchaseItemID, ps.SizeOptionID)
		delivered := stockMap[key]
		if delivered > 0 {
			anyDelivered = true
		}
		if delivered < ps.Qty {
			allDelivered = false
		}
	}

	status := 0
	if allDelivered && anyDelivered {
		status = 2
	} else if anyDelivered {
		status = 1
	}

	return tx.Model(&models.Purchase{}).Where("id = ?", purchaseID).Update("delivery_status", status).Error
}
