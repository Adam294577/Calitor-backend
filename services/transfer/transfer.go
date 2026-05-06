package transfer

import (
	"fmt"
	"math"
	"project/models"
	"project/services/inventory"
	"strconv"

	"gorm.io/gorm"
)

// CreateBatchSharedHeader 批次建立調撥單共用表頭。
// SourceStore 為整批共用的「調出庫點」(branch_code);批次內每張單對應不同 DestStore。
type CreateBatchSharedHeader struct {
	TransferDate string `json:"transfer_date" binding:"required"`
	SourceStore  string `json:"source_store" binding:"required"`
	FillPersonID *int64 `json:"fill_person_id"`
	InputMode    int    `json:"input_mode"`
}

// CreateBatchSize 單一尺碼的調撥量。
type CreateBatchSize struct {
	SizeOptionID int64 `json:"size_option_id"`
	Qty          int   `json:"qty"`
}

// CreateBatchItem 單一調撥明細(整張單共用 dest_store)。
type CreateBatchItem struct {
	ProductID     int64             `json:"product_id"`
	SizeGroupID   *int64            `json:"size_group_id"`
	ItemOrder     int               `json:"item_order"`
	UnitPrice     float64           `json:"unit_price"`
	ItemConfirmed bool              `json:"item_confirmed"`
	Sizes         []CreateBatchSize `json:"sizes"`
}

// CreateBatchTransfer 單一調撥單(整張對應一個調入庫點)。
type CreateBatchTransfer struct {
	DestStore string            `json:"dest_store" binding:"required"`
	Remark    string            `json:"remark"`
	Items     []CreateBatchItem `json:"items"`
}

// CreateBatchPayload 批次建立調撥單的完整輸入。
type CreateBatchPayload struct {
	SharedHeader CreateBatchSharedHeader `json:"shared_header" binding:"required"`
	Transfers    []CreateBatchTransfer   `json:"transfers" binding:"required"`
}

// CreatedInfo 建立成功的單張調撥回傳摘要。
type CreatedInfo struct {
	ID         int64  `json:"id"`
	TransferNo string `json:"transfer_no"`
	DestStore  string `json:"dest_store"`
}

// CreateBatch 單交易批次建立多張調撥單,連號產生,失敗整體 rollback。
// 共用表頭指定一個調出庫點(SourceStore);每張 transfer 對應一個調入庫點(DestStore)。
// 呼叫端須傳入 Transaction 的 tx。recorderID 通常來自 gin context 中的 AdminId。
func CreateBatch(tx *gorm.DB, payload CreateBatchPayload, recorderID int64) ([]CreatedInfo, error) {
	if len(payload.Transfers) == 0 {
		return nil, fmt.Errorf("無調撥單資料")
	}

	sh := payload.SharedHeader
	if sh.TransferDate == "" {
		return nil, fmt.Errorf("缺少調撥日期")
	}
	if sh.SourceStore == "" {
		return nil, fmt.Errorf("缺少調出庫點")
	}
	inputMode := sh.InputMode
	if inputMode == 0 {
		inputMode = models.TransferInputModeBarcode // 批次建單預設為條碼模式
	}

	// branch_code → RetailCustomer 快取
	branchCache := map[string]models.RetailCustomer{}
	loadBranch := func(branchCode string) (*models.RetailCustomer, error) {
		if c, ok := branchCache[branchCode]; ok {
			return &c, nil
		}
		var c models.RetailCustomer
		if err := tx.Where("branch_code = ? AND is_visible = ?", branchCode, true).First(&c).Error; err != nil {
			return nil, fmt.Errorf("庫點 %s 不存在或已停用", branchCode)
		}
		branchCache[branchCode] = c
		return &c, nil
	}

	sourceCustomer, err := loadBranch(sh.SourceStore)
	if err != nil {
		return nil, err
	}

	// 連號 prefix:整批共用 sourceCustomer.BranchCode + TransferDate
	prefix := sourceCustomer.BranchCode + sh.TransferDate
	// advisory lock:整批一次取得,釋放在 transaction 結束時
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "transfer_no:"+prefix).Error; err != nil {
		return nil, fmt.Errorf("取得流水號鎖失敗:%w", err)
	}
	var maxNo string
	if err := tx.Unscoped().Model(&models.Transfer{}).
		Where("transfer_no LIKE ?", prefix+"%").
		Select("COALESCE(MAX(transfer_no), '')").
		Scan(&maxNo).Error; err != nil {
		return nil, fmt.Errorf("查詢流水號失敗:%w", err)
	}
	nextSeq := 1
	if maxNo != "" && len(maxNo) > len(prefix) {
		tail := maxNo[len(prefix):]
		if n, perr := strconv.Atoi(tail); perr == nil {
			nextSeq = n + 1
		}
	}

	var created []CreatedInfo
	var allDeltas []inventory.StockDelta

	for idx := range payload.Transfers {
		tf := payload.Transfers[idx]
		if len(tf.Items) == 0 {
			return nil, fmt.Errorf("第 %d 張:無調撥明細", idx+1)
		}
		if tf.DestStore == "" {
			return nil, fmt.Errorf("第 %d 張:未指定調入庫點", idx+1)
		}

		destCustomer, err := loadBranch(tf.DestStore)
		if err != nil {
			return nil, fmt.Errorf("第 %d 張:%v", idx+1, err)
		}

		transferNo := fmt.Sprintf("%s%03d", prefix, nextSeq)
		nextSeq++

		transfer := models.Transfer{
			TransferNo:       transferNo,
			TransferDate:     sh.TransferDate,
			SourceStore:      sh.SourceStore,
			SourceCustomerID: sourceCustomer.ID,
			FillPersonID:     sh.FillPersonID,
			RecorderID:       recorderID,
			Remark:           tf.Remark,
			InputMode:        inputMode,
		}
		if err := tx.Create(&transfer).Error; err != nil {
			return nil, fmt.Errorf("第 %d 張:建立失敗 %v", idx+1, err)
		}

		// 預先建立 TransferItem 物件 + 累積庫存 delta
		newItems := make([]models.TransferItem, 0, len(tf.Items))
		for _, reqItem := range tf.Items {
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
				DestStore:      tf.DestStore,
				DestCustomerID: destCustomer.ID,
				ItemConfirmed:  reqItem.ItemConfirmed,
			})
			for _, s := range reqItem.Sizes {
				if s.Qty <= 0 {
					continue
				}
				allDeltas = append(allDeltas,
					inventory.StockDelta{CustomerID: sourceCustomer.ID, ProductID: reqItem.ProductID, SizeOptionID: s.SizeOptionID, Qty: -s.Qty},
					inventory.StockDelta{CustomerID: destCustomer.ID, ProductID: reqItem.ProductID, SizeOptionID: s.SizeOptionID, Qty: s.Qty},
				)
			}
		}

		// 批次插入 items,拿回 ID 後再批次插入 sizes
		if len(newItems) > 0 {
			if err := tx.CreateInBatches(&newItems, 100).Error; err != nil {
				return nil, fmt.Errorf("第 %d 張:明細建立失敗 %v", idx+1, err)
			}
		}
		var newSizes []models.TransferItemSize
		for i, reqItem := range tf.Items {
			itemID := newItems[i].ID
			for _, s := range reqItem.Sizes {
				if s.Qty <= 0 {
					continue
				}
				newSizes = append(newSizes, models.TransferItemSize{
					TransferItemID: itemID,
					SizeOptionID:   s.SizeOptionID,
					Qty:            s.Qty,
				})
			}
		}
		if len(newSizes) > 0 {
			if err := tx.CreateInBatches(&newSizes, 200).Error; err != nil {
				return nil, fmt.Errorf("第 %d 張:尺碼建立失敗 %v", idx+1, err)
			}
		}

		created = append(created, CreatedInfo{
			ID:         transfer.ID,
			TransferNo: transfer.TransferNo,
			DestStore:  tf.DestStore,
		})
	}

	// 全部 transfer 累積完 deltas 後,一次庫存足量檢查 + UPSERT
	if err := inventory.CheckStockSufficientBatch(tx, allDeltas); err != nil {
		return nil, err
	}
	if err := inventory.ApplyStockDeltas(tx, allDeltas); err != nil {
		return nil, err
	}

	return created, nil
}
