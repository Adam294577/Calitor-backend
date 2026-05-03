package modify

import (
	"fmt"
	"project/models"
	"project/services/inventory"

	"gorm.io/gorm"
)

// UpdateSize 更新時的單一尺碼數量(可正可負)。
type UpdateSize struct {
	SizeOptionID int64 `json:"size_option_id"`
	Qty          int   `json:"qty"`
}

// UpdateItem 更新時的單一明細列。
type UpdateItem struct {
	ProductID   int64        `json:"product_id"`
	SizeGroupID *int64       `json:"size_group_id"`
	ItemOrder   int          `json:"item_order"`
	Sizes       []UpdateSize `json:"sizes"`
}

// UpdatePayload 更新庫存調整單的輸入。
type UpdatePayload struct {
	ModifyDate   string       `json:"modify_date" binding:"required"`
	ModifyStore  string       `json:"modify_store" binding:"required"`
	FillPersonID *int64       `json:"fill_person_id"`
	Remark       string       `json:"remark"`
	Items        []UpdateItem `json:"items"`
}

// Update 更新庫存調整單,須在外層 Transaction 內呼叫(tx 由呼叫端提供)。
// 流程:還原舊庫存(-1) -> 刪舊明細 -> 更新主表 -> 重建明細與新庫存(+1)。
// recorderID 為 0 時不覆寫(保留原 RecorderID)。
func Update(tx *gorm.DB, id int64, payload UpdatePayload, recorderID int64) error {
	if len(payload.Items) == 0 {
		return fmt.Errorf("調整單明細不可為空")
	}

	var existing models.Modify
	if err := tx.Where("id = ?", id).First(&existing).Error; err != nil {
		return fmt.Errorf("調整單不存在: %w", err)
	}

	var customer models.RetailCustomer
	if err := tx.Where("branch_code = ?", payload.ModifyStore).First(&customer).Error; err != nil {
		return fmt.Errorf("調整庫點不存在: %w", err)
	}

	if err := revertItems(tx, id, existing.CustomerID); err != nil {
		return err
	}

	if err := deleteItems(tx, id); err != nil {
		return err
	}

	finalRecorderID := existing.RecorderID
	if recorderID != 0 {
		finalRecorderID = recorderID
	}
	updates := map[string]interface{}{
		"modify_date":    payload.ModifyDate,
		"modify_store":   payload.ModifyStore,
		"customer_id":    customer.ID,
		"fill_person_id": payload.FillPersonID,
		"recorder_id":    finalRecorderID,
		"remark":         payload.Remark,
	}
	if err := tx.Model(&existing).Updates(updates).Error; err != nil {
		return err
	}

	return createItems(tx, id, customer.ID, payload.Items)
}

// Delete 軟刪除庫存調整單並還原庫存,須在外層 Transaction 內呼叫。
func Delete(tx *gorm.DB, id int64) error {
	var modify models.Modify
	if err := tx.Where("id = ?", id).First(&modify).Error; err != nil {
		return fmt.Errorf("調整單不存在: %w", err)
	}

	if err := revertItems(tx, id, modify.CustomerID); err != nil {
		return err
	}

	if err := tx.Delete(&models.Modify{}, id).Error; err != nil {
		return err
	}
	return nil
}

// revertItems 將某調整單舊明細的影響從 size_stocks 還原(multiplier=-1)。
func revertItems(tx *gorm.DB, modifyID, customerID int64) error {
	var oldItems []models.ModifyItem
	if err := tx.Preload("Sizes").Where("modify_id = ?", modifyID).Find(&oldItems).Error; err != nil {
		return err
	}
	var adj []inventory.StockAdjustItem
	for _, oi := range oldItems {
		var sizes []inventory.StockAdjustSize
		for _, s := range oi.Sizes {
			if s.Qty != 0 {
				sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
			}
		}
		if len(sizes) > 0 {
			adj = append(adj, inventory.StockAdjustItem{ProductID: oi.ProductID, Sizes: sizes})
		}
	}
	if len(adj) == 0 {
		return nil
	}
	return inventory.AdjustStockBatch(tx, customerID, adj, -1)
}

// deleteItems 硬刪除某調整單下所有 ModifyItemSize / ModifyItem(無 soft delete)。
func deleteItems(tx *gorm.DB, modifyID int64) error {
	var oldItemIDs []int64
	if err := tx.Model(&models.ModifyItem{}).Where("modify_id = ?", modifyID).Pluck("id", &oldItemIDs).Error; err != nil {
		return err
	}
	if len(oldItemIDs) > 0 {
		if err := tx.Where("modify_item_id IN ?", oldItemIDs).Delete(&models.ModifyItemSize{}).Error; err != nil {
			return err
		}
	}
	return tx.Where("modify_id = ?", modifyID).Delete(&models.ModifyItem{}).Error
}

// createItems 建立調整單明細與尺碼,並對 size_stocks 加上影響(multiplier=+1)。
func createItems(tx *gorm.DB, modifyID, customerID int64, items []UpdateItem) error {
	var newAdj []inventory.StockAdjustItem
	for _, reqItem := range items {
		totalQty := 0
		for _, s := range reqItem.Sizes {
			totalQty += s.Qty
		}
		item := models.ModifyItem{
			ModifyID:    modifyID,
			ProductID:   reqItem.ProductID,
			SizeGroupID: reqItem.SizeGroupID,
			ItemOrder:   reqItem.ItemOrder,
			TotalQty:    totalQty,
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		var sizes []inventory.StockAdjustSize
		for _, s := range reqItem.Sizes {
			size := models.ModifyItemSize{
				ModifyItemID: item.ID,
				SizeOptionID: s.SizeOptionID,
				Qty:          s.Qty,
			}
			if err := tx.Create(&size).Error; err != nil {
				return err
			}
			if s.Qty != 0 {
				sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
			}
		}
		if len(sizes) > 0 {
			newAdj = append(newAdj, inventory.StockAdjustItem{ProductID: reqItem.ProductID, Sizes: sizes})
		}
	}
	if len(newAdj) == 0 {
		return nil
	}
	return inventory.AdjustStockBatch(tx, customerID, newAdj, 1)
}
