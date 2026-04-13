package inventory

import (
	"project/models"

	"gorm.io/gorm"
)

// AdjustStock 調整庫存（正數加、負數扣）
// productID: 商品 ID
// customerID: 庫點客戶 ID
// sizeOptionID: 尺碼選項 ID
// qty: 增減數量（正=加，負=扣）
func AdjustStock(tx *gorm.DB, productID, customerID, sizeOptionID int64, qty int) error {
	if qty == 0 {
		return nil
	}

	var stock models.ProductSizeStock
	err := tx.Where("product_id = ? AND customer_id = ? AND size_option_id = ?",
		productID, customerID, sizeOptionID).First(&stock).Error

	if err == gorm.ErrRecordNotFound {
		// 新建
		stock = models.ProductSizeStock{
			ProductID:    productID,
			CustomerID:   customerID,
			SizeOptionID: sizeOptionID,
			Qty:          qty,
		}
		return tx.Create(&stock).Error
	}
	if err != nil {
		return err
	}

	// 更新
	return tx.Model(&stock).Update("qty", stock.Qty+qty).Error
}

// AdjustStockBatch 批次調整庫存（用於進貨/出貨整單）
// customerID: 庫點客戶 ID
// items: 明細列表，每筆含 ProductID 和 Sizes
// multiplier: 1=進貨加庫存, -1=出貨扣庫存
func AdjustStockBatch(tx *gorm.DB, customerID int64, items []StockAdjustItem, multiplier int) error {
	for _, item := range items {
		for _, size := range item.Sizes {
			qty := size.Qty * multiplier
			if err := AdjustStock(tx, item.ProductID, customerID, size.SizeOptionID, qty); err != nil {
				return err
			}
		}
	}
	return nil
}

// StockAdjustItem 庫存調整用的明細
type StockAdjustItem struct {
	ProductID int64
	Sizes     []StockAdjustSize
}

// StockAdjustSize 庫存調整用的尺碼
type StockAdjustSize struct {
	SizeOptionID int64
	Qty          int
}
