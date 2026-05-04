package controllers

import (
	"fmt"
	"math"
	"net/http"
	"project/models"
	"project/services/inventory"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetTransfers 調撥單列表
func GetTransfers(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Transfer
	query := db.GetRead().
		Preload("SourceCustomer").
		Preload("FillPerson").
		Order("transfer_date DESC, id DESC")

	query = ApplySearch(query, c.Query("search"), "transfer_no")

	if v := c.Query("source_store"); v != "" {
		query = query.Where("source_store = ?", v)
	}
	if v := c.Query("dest_store"); v != "" {
		// 篩選包含此調入庫點的調撥單
		query = query.Where("id IN (SELECT transfer_id FROM transfer_items WHERE dest_store = ?)", v)
	}
	if v := c.Query("date_from"); v != "" {
		query = query.Where("transfer_date >= ?", v)
	}
	if v := c.Query("date_to"); v != "" {
		query = query.Where("transfer_date <= ?", v)
	}
	if v := c.Query("confirmed"); v != "" {
		query = query.Where("confirmed = ?", v == "1")
	}

	paged, total := Paginate(c, query, &models.Transfer{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// GetTransfer 調撥單詳情
func GetTransfer(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Transfer
	err = db.GetRead().
		Preload("SourceCustomer").
		Preload("FillPerson").
		Preload("Recorder").
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
		Preload("Items.SizeGroup.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Sizes.SizeOption").
		Preload("Items.DestCustomer").
		Where("id = ?", id).
		First(&item).Error
	if err != nil {
		resp.Fail(http.StatusNotFound, "調撥單不存在").Send()
		return
	}

	resp.Success("成功").SetData(item).Send()
}

// CreateTransfer 新增調撥單
func CreateTransfer(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		TransferDate string `json:"transfer_date" binding:"required"`
		SourceStore  string `json:"source_store" binding:"required"`
		FillPersonID *int64 `json:"fill_person_id"`
		Remark       string `json:"remark"`
		Items        []struct {
			ProductID     int64   `json:"product_id"`
			SizeGroupID   *int64  `json:"size_group_id"`
			ItemOrder     int     `json:"item_order"`
			UnitPrice     float64 `json:"unit_price"`
			DestStore     string  `json:"dest_store"`
			ItemConfirmed bool    `json:"item_confirmed"`
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

	// 查調出庫點
	var sourceCustomer models.RetailCustomer
	if err := db.GetRead().Where("branch_code = ?", req.SourceStore).First(&sourceCustomer).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "調出庫點不存在").Send()
		return
	}

	// 查調入庫點（快取）
	destCustomerMap := make(map[string]*models.RetailCustomer)
	for _, reqItem := range req.Items {
		if reqItem.DestStore == "" {
			resp.Fail(http.StatusBadRequest, "調入庫點不可為空").Send()
			return
		}
		if _, ok := destCustomerMap[reqItem.DestStore]; !ok {
			var dc models.RetailCustomer
			if err := db.GetRead().Where("branch_code = ?", reqItem.DestStore).First(&dc).Error; err != nil {
				resp.Fail(http.StatusBadRequest, fmt.Sprintf("調入庫點 %s 不存在", reqItem.DestStore)).Send()
				return
			}
			destCustomerMap[reqItem.DestStore] = &dc
		}
	}

	// 產生調撥單號: SourceBranchCode + YYYYMMDD + 3碼流水號
	prefix := sourceCustomer.BranchCode + req.TransferDate
	var maxNo string
	db.GetRead().Unscoped().Model(&models.Transfer{}).
		Where("transfer_no LIKE ?", prefix+"%").
		Select("COALESCE(MAX(transfer_no), '')").
		Scan(&maxNo)

	seq := 1
	if maxNo != "" && len(maxNo) > len(prefix) {
		tail := maxNo[len(prefix):]
		if n, err := strconv.Atoi(tail); err == nil {
			seq = n + 1
		}
	}
	transferNo := fmt.Sprintf("%s%03d", prefix, seq)

	// 系統紀錄者
	adminId, _ := c.Get("AdminId")
	recorderID := int64(0)
	if id, ok := adminId.(float64); ok {
		recorderID = int64(id)
	}

	transfer := models.Transfer{
		TransferNo:       transferNo,
		TransferDate:     req.TransferDate,
		SourceStore:      req.SourceStore,
		SourceCustomerID: sourceCustomer.ID,
		FillPersonID:     req.FillPersonID,
		RecorderID:       recorderID,
		Remark:           req.Remark,
		InputMode:        models.TransferInputModeKeyboard,
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&transfer).Error; err != nil {
			return err
		}

		// 預先算 totalQty / totalAmount 與庫存 delta，準備批次插入
		newItems := make([]models.TransferItem, 0, len(req.Items))
		var deltas []inventory.StockDelta

		for _, reqItem := range req.Items {
			destCust := destCustomerMap[reqItem.DestStore]
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := math.Round(float64(totalQty) * reqItem.UnitPrice)
			newItems = append(newItems, models.TransferItem{
				TransferID:     transfer.ID,
				ProductID:      reqItem.ProductID,
				SizeGroupID:    reqItem.SizeGroupID,
				ItemOrder:      reqItem.ItemOrder,
				TotalQty:       totalQty,
				UnitPrice:      reqItem.UnitPrice,
				TotalAmount:    totalAmount,
				DestStore:      reqItem.DestStore,
				DestCustomerID: destCust.ID,
				ItemConfirmed:  reqItem.ItemConfirmed,
			})
			for _, s := range reqItem.Sizes {
				if s.Qty <= 0 {
					continue
				}
				deltas = append(deltas,
					inventory.StockDelta{CustomerID: sourceCustomer.ID, ProductID: reqItem.ProductID, SizeOptionID: s.SizeOptionID, Qty: -s.Qty},
					inventory.StockDelta{CustomerID: destCust.ID, ProductID: reqItem.ProductID, SizeOptionID: s.SizeOptionID, Qty: s.Qty},
				)
			}
		}

		// 庫存足量檢查（套用 deltas 後仍 >= 0）
		if err := inventory.CheckStockSufficientBatch(tx, deltas); err != nil {
			return err
		}

		// 批次插入 TransferItem，拿回 ID 後再批次插入 sizes
		if len(newItems) > 0 {
			if err := tx.CreateInBatches(&newItems, 100).Error; err != nil {
				return err
			}
		}
		var newSizes []models.TransferItemSize
		for i, reqItem := range req.Items {
			itemID := newItems[i].ID
			for _, s := range reqItem.Sizes {
				newSizes = append(newSizes, models.TransferItemSize{
					TransferItemID: itemID,
					SizeOptionID:   s.SizeOptionID,
					Qty:            s.Qty,
				})
			}
		}
		if len(newSizes) > 0 {
			if err := tx.CreateInBatches(&newSizes, 200).Error; err != nil {
				return err
			}
		}

		// 一次 UPSERT 套用全部庫存 delta
		if err := inventory.ApplyStockDeltas(tx, deltas); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		resp.Fail(http.StatusBadRequest, err.Error()).Send()
		return
	}

	resp.Success("新增成功").SetData(transfer).Send()
}

// UpdateTransfer 更新調撥單
func UpdateTransfer(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.Transfer
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusNotFound, "調撥單不存在").Send()
		return
	}

	var req struct {
		TransferDate string `json:"transfer_date"`
		SourceStore  string `json:"source_store"`
		FillPersonID *int64 `json:"fill_person_id"`
		Remark       string `json:"remark"`
		Items        []struct {
			ProductID     int64   `json:"product_id"`
			SizeGroupID   *int64  `json:"size_group_id"`
			ItemOrder     int     `json:"item_order"`
			UnitPrice     float64 `json:"unit_price"`
			DestStore     string  `json:"dest_store"`
			ItemConfirmed bool    `json:"item_confirmed"`
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

	// 查新的調出庫點
	var sourceCustomer models.RetailCustomer
	if err := db.GetRead().Where("branch_code = ?", req.SourceStore).First(&sourceCustomer).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "調出庫點不存在").Send()
		return
	}

	// 查調入庫點（快取）
	destCustomerMap := make(map[string]*models.RetailCustomer)
	for _, reqItem := range req.Items {
		if reqItem.DestStore == "" {
			resp.Fail(http.StatusBadRequest, "調入庫點不可為空").Send()
			return
		}
		if _, ok := destCustomerMap[reqItem.DestStore]; !ok {
			var dc models.RetailCustomer
			if err := db.GetRead().Where("branch_code = ?", reqItem.DestStore).First(&dc).Error; err != nil {
				resp.Fail(http.StatusBadRequest, fmt.Sprintf("調入庫點 %s 不存在", reqItem.DestStore)).Send()
				return
			}
			destCustomerMap[reqItem.DestStore] = &dc
		}
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// === 1. 撈舊明細（為計算還原 delta） ===
		var oldItems []models.TransferItem
		if err := tx.Preload("Sizes").Where("transfer_id = ?", id).Find(&oldItems).Error; err != nil {
			return err
		}

		// === 2. 算淨 delta：舊還原（調出加回、調入扣回）+ 新套用（調出扣、調入加） ===
		var deltas []inventory.StockDelta
		for _, oi := range oldItems {
			for _, s := range oi.Sizes {
				if s.Qty <= 0 {
					continue
				}
				deltas = append(deltas,
					inventory.StockDelta{CustomerID: existing.SourceCustomerID, ProductID: oi.ProductID, SizeOptionID: s.SizeOptionID, Qty: s.Qty},
					inventory.StockDelta{CustomerID: oi.DestCustomerID, ProductID: oi.ProductID, SizeOptionID: s.SizeOptionID, Qty: -s.Qty},
				)
			}
		}
		for _, reqItem := range req.Items {
			destCust := destCustomerMap[reqItem.DestStore]
			for _, s := range reqItem.Sizes {
				if s.Qty <= 0 {
					continue
				}
				deltas = append(deltas,
					inventory.StockDelta{CustomerID: sourceCustomer.ID, ProductID: reqItem.ProductID, SizeOptionID: s.SizeOptionID, Qty: -s.Qty},
					inventory.StockDelta{CustomerID: destCust.ID, ProductID: reqItem.ProductID, SizeOptionID: s.SizeOptionID, Qty: s.Qty},
				)
			}
		}

		// === 3. 庫存足量檢查（單一 SELECT） ===
		if err := inventory.CheckStockSufficientBatch(tx, deltas); err != nil {
			return err
		}

		// === 4. 刪除舊明細 ===
		var oldItemIDs []int64
		if err := tx.Model(&models.TransferItem{}).Where("transfer_id = ?", id).Pluck("id", &oldItemIDs).Error; err != nil {
			return err
		}
		if len(oldItemIDs) > 0 {
			if err := tx.Where("transfer_item_id IN ?", oldItemIDs).Delete(&models.TransferItemSize{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("transfer_id = ?", id).Delete(&models.TransferItem{}).Error; err != nil {
			return err
		}

		// === 5. 更新主表 ===
		adminId, _ := c.Get("AdminId")
		recorderID := existing.RecorderID
		if aid, ok := adminId.(float64); ok {
			recorderID = int64(aid)
		}
		updates := map[string]interface{}{
			"transfer_date":      req.TransferDate,
			"source_store":       req.SourceStore,
			"source_customer_id": sourceCustomer.ID,
			"fill_person_id":     req.FillPersonID,
			"recorder_id":        recorderID,
			"remark":             req.Remark,
		}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}

		// === 6. 批次重建明細 ===
		newItems := make([]models.TransferItem, 0, len(req.Items))
		for _, reqItem := range req.Items {
			destCust := destCustomerMap[reqItem.DestStore]
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := math.Round(float64(totalQty) * reqItem.UnitPrice)
			newItems = append(newItems, models.TransferItem{
				TransferID:     id,
				ProductID:      reqItem.ProductID,
				SizeGroupID:    reqItem.SizeGroupID,
				ItemOrder:      reqItem.ItemOrder,
				TotalQty:       totalQty,
				UnitPrice:      reqItem.UnitPrice,
				TotalAmount:    totalAmount,
				DestStore:      reqItem.DestStore,
				DestCustomerID: destCust.ID,
				ItemConfirmed:  reqItem.ItemConfirmed,
			})
		}
		if len(newItems) > 0 {
			if err := tx.CreateInBatches(&newItems, 100).Error; err != nil {
				return err
			}
		}
		var newSizes []models.TransferItemSize
		for i, reqItem := range req.Items {
			itemID := newItems[i].ID
			for _, s := range reqItem.Sizes {
				newSizes = append(newSizes, models.TransferItemSize{
					TransferItemID: itemID,
					SizeOptionID:   s.SizeOptionID,
					Qty:            s.Qty,
				})
			}
		}
		if len(newSizes) > 0 {
			if err := tx.CreateInBatches(&newSizes, 200).Error; err != nil {
				return err
			}
		}

		// === 7. 一次 UPSERT 套用全部庫存 delta ===
		if err := inventory.ApplyStockDeltas(tx, deltas); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		resp.Fail(http.StatusBadRequest, err.Error()).Send()
		return
	}

	resp.Success("更新成功").Send()
}

// DeleteTransfer 軟刪除調撥單
func DeleteTransfer(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var transfer models.Transfer
	if err := db.GetRead().Where("id = ?", id).First(&transfer).Error; err != nil {
		resp.Fail(http.StatusNotFound, "調撥單不存在").Send()
		return
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 還原庫存
		var items []models.TransferItem
		if err := tx.Preload("Sizes").Where("transfer_id = ?", id).Find(&items).Error; err != nil {
			return err
		}

		var sourceAdj []inventory.StockAdjustItem
		destAdj := make(map[int64][]inventory.StockAdjustItem)
		for _, ti := range items {
			var sizes []inventory.StockAdjustSize
			for _, s := range ti.Sizes {
				if s.Qty > 0 {
					sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
				}
			}
			if len(sizes) > 0 {
				sourceAdj = append(sourceAdj, inventory.StockAdjustItem{ProductID: ti.ProductID, Sizes: sizes})
				destAdj[ti.DestCustomerID] = append(destAdj[ti.DestCustomerID], inventory.StockAdjustItem{ProductID: ti.ProductID, Sizes: sizes})
			}
		}

		// 調出方加回
		if len(sourceAdj) > 0 {
			if err := inventory.AdjustStockBatch(tx, transfer.SourceCustomerID, sourceAdj, 1); err != nil {
				return err
			}
		}
		// 調入方扣回
		for destCustID, adjItems := range destAdj {
			if err := inventory.AdjustStockBatch(tx, destCustID, adjItems, -1); err != nil {
				return err
			}
		}

		if err := tx.Delete(&models.Transfer{}, id).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("刪除成功").Send()
}

// ConfirmTransfer 確認調撥單
func ConfirmTransfer(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var transfer models.Transfer
	if err := db.GetRead().Where("id = ?", id).First(&transfer).Error; err != nil {
		resp.Fail(http.StatusNotFound, "調撥單不存在").Send()
		return
	}
	if transfer.Confirmed {
		resp.Fail(http.StatusBadRequest, "此調撥單已確認").Send()
		return
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&transfer).Update("confirmed", true).Error; err != nil {
			return err
		}
		// 所有明細也標記確認
		if err := tx.Model(&models.TransferItem{}).Where("transfer_id = ?", id).Update("item_confirmed", true).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("確認成功").Send()
}
