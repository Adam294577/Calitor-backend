package inventory

import (
	"fmt"
	"project/models"
	"strings"
	"time"

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

// CheckStockSufficient 檢查指定庫點是否有足夠庫存可扣
// 回傳 nil 表示足夠；不足時回傳錯誤（訊息含商品 ID 與缺少數量）
func CheckStockSufficient(tx *gorm.DB, customerID int64, items []StockAdjustItem) error {
	for _, item := range items {
		for _, size := range item.Sizes {
			if size.Qty <= 0 {
				continue
			}
			var stock models.ProductSizeStock
			err := tx.Where("product_id = ? AND customer_id = ? AND size_option_id = ?",
				item.ProductID, customerID, size.SizeOptionID).First(&stock).Error
			if err == gorm.ErrRecordNotFound {
				return fmt.Errorf("商品 ID %d 尺碼 %d 庫存不足（現有 0，需 %d）",
					item.ProductID, size.SizeOptionID, size.Qty)
			}
			if err != nil {
				return err
			}
			if stock.Qty < size.Qty {
				return fmt.Errorf("商品 ID %d 尺碼 %d 庫存不足（現有 %d，需 %d）",
					item.ProductID, size.SizeOptionID, stock.Qty, size.Qty)
			}
		}
	}
	return nil
}

// ===== 批次版本：用於 transfer / 大量 item 的單一交易，避免 N+1 query =====

// StockDelta 單筆庫存增減（正=加, 負=扣），跨多個 customer
type StockDelta struct {
	CustomerID   int64
	ProductID    int64
	SizeOptionID int64
	Qty          int
}

// ApplyStockDeltas 一次套用多筆庫存增減；用 PostgreSQL UPSERT 收斂到單一 SQL。
// 內部會聚合相同 (product, customer, size) 的 delta 避免「ON CONFLICT 兩次影響同列」錯誤。
func ApplyStockDeltas(tx *gorm.DB, deltas []StockDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	type k = [3]int64
	agg := make(map[k]int)
	for _, d := range deltas {
		agg[k{d.ProductID, d.CustomerID, d.SizeOptionID}] += d.Qty
	}
	placeholders := make([]string, 0, len(agg))
	args := make([]interface{}, 0, len(agg)*6)
	now := time.Now()
	for key, qty := range agg {
		if qty == 0 {
			continue
		}
		placeholders = append(placeholders, "(?, ?, ?, ?, ?, ?)")
		args = append(args, key[0], key[1], key[2], qty, now, now)
	}
	if len(placeholders) == 0 {
		return nil
	}
	sql := fmt.Sprintf(`
		INSERT INTO product_size_stocks (product_id, customer_id, size_option_id, qty, created_at, updated_at)
		VALUES %s
		ON CONFLICT (product_id, customer_id, size_option_id)
		DO UPDATE SET qty = product_size_stocks.qty + EXCLUDED.qty, updated_at = EXCLUDED.updated_at
	`, strings.Join(placeholders, ", "))
	return tx.Exec(sql, args...).Error
}

// CheckStockSufficientBatch 檢查 deltas 套用後，所有受影響的 (customer, product, size) 庫存仍 >= 0。
// 只檢查負 delta（扣除）；正 delta 不檢查。一次 SELECT 撈完所有相關列，避免 N+1。
func CheckStockSufficientBatch(tx *gorm.DB, deltas []StockDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	type k = [3]int64
	deduct := make(map[k]int)
	for _, d := range deltas {
		if d.Qty >= 0 {
			continue
		}
		deduct[k{d.ProductID, d.CustomerID, d.SizeOptionID}] += -d.Qty
	}
	if len(deduct) == 0 {
		return nil
	}

	placeholders := make([]string, 0, len(deduct))
	args := make([]interface{}, 0, len(deduct)*3)
	for key := range deduct {
		placeholders = append(placeholders, "(?, ?, ?)")
		args = append(args, key[0], key[1], key[2])
	}

	type stockRow struct {
		ProductID    int64
		CustomerID   int64
		SizeOptionID int64
		Qty          int
	}
	var rows []stockRow
	sql := fmt.Sprintf(`
		SELECT product_id, customer_id, size_option_id, qty
		FROM product_size_stocks
		WHERE (product_id, customer_id, size_option_id) IN (%s)
	`, strings.Join(placeholders, ", "))
	if err := tx.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return err
	}

	have := make(map[k]int, len(rows))
	for _, r := range rows {
		have[k{r.ProductID, r.CustomerID, r.SizeOptionID}] = r.Qty
	}
	for key, need := range deduct {
		if have[key] < need {
			return fmt.Errorf("商品 ID %d 尺碼 %d 庫存不足（現有 %d，需 %d）",
				key[0], key[2], have[key], need)
		}
	}
	return nil
}
