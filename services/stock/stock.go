package stock

import (
	"fmt"
	"project/models"
	"project/services/delivery"
	"project/services/inventory"
	"strconv"

	"gorm.io/gorm"
)

// CreateBatchSharedHeader 批次建立進貨單共用表頭。
// CustomerID 為可選的「整批預設庫點」(向後相容);若 per-stock CustomerID 有值則優先使用 per-stock。
type CreateBatchSharedHeader struct {
	StockDate       string  `json:"stock_date" binding:"required"`
	CustomerID      int64   `json:"customer_id"` // optional fallback;per-stock 優先
	StockMode       int     `json:"stock_mode"`
	DealMode        int     `json:"deal_mode"`
	FillPersonID    *int64  `json:"fill_person_id"`
	CloseMonth      string  `json:"close_month"`
	Remark          string  `json:"remark"`
	TaxMode         int     `json:"tax_mode"`
	TaxRate         float64 `json:"tax_rate"`
	DiscountPercent float64 `json:"discount_percent"`
	InputMode       int     `json:"input_mode"`
}

// CreateBatchSize 單一尺碼的進貨量。
type CreateBatchSize struct {
	SizeOptionID int64 `json:"size_option_id"`
	Qty          int   `json:"qty"`
}

// CreateBatchItem 單一進貨明細。
type CreateBatchItem struct {
	ProductID      int64             `json:"product_id"`
	SizeGroupID    *int64            `json:"size_group_id"`
	PurchaseItemID *int64            `json:"purchase_item_id"`
	ItemOrder      int               `json:"item_order"`
	AdvicePrice    float64           `json:"advice_price"`
	Discount       float64           `json:"discount"`
	PurchasePrice  float64           `json:"purchase_price"`
	NonTaxPrice    float64           `json:"non_tax_price"`
	Supplement     int               `json:"supplement"`
	Sizes          []CreateBatchSize `json:"sizes"`
}

// CreateBatchStock 單一進貨單（同一批次中會建多張）。
// CustomerID 可由此處傳入(支援多庫點批次);若為 0 則 fallback 到 SharedHeader.CustomerID。
type CreateBatchStock struct {
	CustomerID    int64             `json:"customer_id"`
	VendorID      int64             `json:"vendor_id" binding:"required"`
	VendorStockNo string            `json:"vendor_stock_no"`
	Items         []CreateBatchItem `json:"items"`
}

// CreateBatchPayload 批次建立進貨單的完整輸入。
type CreateBatchPayload struct {
	SharedHeader CreateBatchSharedHeader `json:"shared_header" binding:"required"`
	Stocks       []CreateBatchStock      `json:"stocks" binding:"required"`
}

// CreatedInfo 建立成功的單張進貨回傳摘要。
type CreatedInfo struct {
	ID         int64  `json:"id"`
	StockNo    string `json:"stock_no"`
	VendorID   int64  `json:"vendor_id"`
	VendorName string `json:"vendor_name"`
}

// CreateBatch 單交易批次建立多張進貨單，連號產生，失敗整體 rollback。
// 支援每張 stock 屬於不同客戶(庫點);per-stock CustomerID 為 0 時 fallback 到 SharedHeader.CustomerID。
// 呼叫端須傳入 Transaction 的 tx。
// recorderID 通常來自 gin context 中的 AdminId。
func CreateBatch(tx *gorm.DB, payload CreateBatchPayload, recorderID int64) ([]CreatedInfo, error) {
	if len(payload.Stocks) == 0 {
		return nil, fmt.Errorf("無進貨單資料")
	}

	sh := payload.SharedHeader
	if sh.StockMode == 0 {
		sh.StockMode = 1
	}
	if sh.DealMode == 0 {
		sh.DealMode = 1
	}
	if sh.TaxMode == 0 {
		sh.TaxMode = 2
	}
	if sh.DiscountPercent == 0 {
		sh.DiscountPercent = 100
	}
	closeMonth := sh.CloseMonth
	if closeMonth == "" && len(sh.StockDate) >= 6 {
		closeMonth = sh.StockDate[:6]
	}

	prefix := "I"
	if sh.StockMode == 2 {
		prefix = "B"
	}
	yyyymm := ""
	if len(sh.StockDate) >= 6 {
		yyyymm = sh.StockDate[:6]
	}

	// 每個 (branch_code, yyyymm) 各自連號;以 noPrefix 為 key 緩存當前 seq
	seqByPrefix := map[string]int{}
	customerCache := map[int64]models.RetailCustomer{}

	var created []CreatedInfo
	var allPurchaseItemIDs []int64

	for idx := range payload.Stocks {
		st := payload.Stocks[idx]

		// 解析此張 stock 的 customer
		effCustomerID := st.CustomerID
		if effCustomerID == 0 {
			effCustomerID = sh.CustomerID
		}
		if effCustomerID == 0 {
			return nil, fmt.Errorf("第 %d 張:未指定客戶", idx+1)
		}
		customer, cached := customerCache[effCustomerID]
		if !cached {
			if err := tx.Where("id = ? AND is_visible = ?", effCustomerID, true).First(&customer).Error; err != nil {
				return nil, fmt.Errorf("第 %d 張:客戶不存在或已停用 (ID %d)", idx+1, effCustomerID)
			}
			customerCache[effCustomerID] = customer
		}

		// 連號 prefix 以該 stock 客戶的 branch_code 為準
		noPrefix := prefix + customer.BranchCode + yyyymm
		seq, exists := seqByPrefix[noPrefix]
		if !exists {
			// 用 Postgres advisory lock 鎖在 (noPrefix) 上,避免並發撞號
			// 鎖在 transaction 結束自動釋放,不影響其他 noPrefix 的並發寫入
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "stock_no:"+noPrefix).Error; err != nil {
				return nil, fmt.Errorf("取得流水號鎖失敗:%w", err)
			}
			var maxNo string
			if err := tx.Unscoped().Model(&models.Stock{}).
				Where("stock_no LIKE ?", noPrefix+"%").
				Select("MAX(stock_no)").
				Scan(&maxNo).Error; err != nil {
				return nil, fmt.Errorf("查詢流水號失敗:%w", err)
			}
			seq = 1
			if maxNo != "" && len(maxNo) > len(noPrefix) {
				tail := maxNo[len(noPrefix):]
				if n, perr := strconv.Atoi(tail); perr == nil {
					seq = n + 1
				}
			}
		}
		stockNo := fmt.Sprintf("%s%04d", noPrefix, seq)
		seqByPrefix[noPrefix] = seq + 1

		var vendor models.Vendor
		if err := tx.Where("id = ?", st.VendorID).First(&vendor).Error; err != nil {
			return nil, fmt.Errorf("第 %d 張:廠商 ID %d 不存在", idx+1, st.VendorID)
		}
		vendorName := vendor.ShortName
		if vendorName == "" {
			vendorName = vendor.Name
		}

		inputMode := sh.InputMode
		if inputMode == 0 {
			inputMode = models.StockInputModeBarcode // 批次建單預設為條碼模式
		}
		s := models.Stock{
			StockNo:         stockNo,
			StockDate:       sh.StockDate,
			CustomerID:      customer.ID,
			VendorID:        st.VendorID,
			VendorStockNo:   st.VendorStockNo,
			StockMode:       sh.StockMode,
			DealMode:        sh.DealMode,
			FillPersonID:    sh.FillPersonID,
			RecorderID:      recorderID,
			CloseMonth:      closeMonth,
			Remark:          sh.Remark,
			TaxMode:         sh.TaxMode,
			TaxRate:         sh.TaxRate,
			DiscountPercent: sh.DiscountPercent,
			InputMode:       inputMode,
		}
		if err := tx.Create(&s).Error; err != nil {
			return nil, fmt.Errorf("第 %d 張:建立失敗 %v", idx+1, err)
		}

		for itemIdx, reqItem := range st.Items {
			totalQty := 0
			for _, sz := range reqItem.Sizes {
				totalQty += sz.Qty
			}
			totalAmount := float64(totalQty) * reqItem.PurchasePrice

			item := models.StockItem{
				StockID:        s.ID,
				ProductID:      reqItem.ProductID,
				SizeGroupID:    reqItem.SizeGroupID,
				PurchaseItemID: reqItem.PurchaseItemID,
				ItemOrder:      itemIdx,
				AdvicePrice:    reqItem.AdvicePrice,
				Discount:       reqItem.Discount,
				PurchasePrice:  reqItem.PurchasePrice,
				NonTaxPrice:    reqItem.NonTaxPrice,
				TotalQty:       totalQty,
				TotalAmount:    totalAmount,
				Supplement:     reqItem.Supplement,
			}
			if err := tx.Create(&item).Error; err != nil {
				return nil, fmt.Errorf("第 %d 張第 %d 筆明細建立失敗 %v", idx+1, itemIdx+1, err)
			}
			for _, sz := range reqItem.Sizes {
				size := models.StockItemSize{
					StockItemID:  item.ID,
					SizeOptionID: sz.SizeOptionID,
					Qty:          sz.Qty,
				}
				if err := tx.Create(&size).Error; err != nil {
					return nil, err
				}
			}

			if reqItem.PurchaseItemID != nil {
				allPurchaseItemIDs = append(allPurchaseItemIDs, *reqItem.PurchaseItemID)
			}
		}

		multiplier := 1
		if sh.StockMode == 2 {
			multiplier = -1
		}
		var adjustItems []inventory.StockAdjustItem
		for _, reqItem := range st.Items {
			var sizes []inventory.StockAdjustSize
			for _, sz := range reqItem.Sizes {
				if sz.Qty > 0 {
					sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: sz.SizeOptionID, Qty: sz.Qty})
				}
			}
			if len(sizes) > 0 {
				adjustItems = append(adjustItems, inventory.StockAdjustItem{ProductID: reqItem.ProductID, Sizes: sizes})
			}
		}
		if err := inventory.AdjustStockBatch(tx, s.CustomerID, adjustItems, multiplier); err != nil {
			return nil, fmt.Errorf("第 %d 張:庫存調整失敗 %v", idx+1, err)
		}

		created = append(created, CreatedInfo{
			ID:         s.ID,
			StockNo:    s.StockNo,
			VendorID:   st.VendorID,
			VendorName: vendorName,
		})
	}

	purchaseIDs := DistinctPurchaseIDs(tx, allPurchaseItemIDs)
	if err := RecalcPurchasesDeliveryStatus(tx, purchaseIDs); err != nil {
		return nil, err
	}

	return created, nil
}

// DistinctPurchaseIDs 從一組 purchaseItemID 查詢所屬的 purchase_id 去重清單。
func DistinctPurchaseIDs(db *gorm.DB, purchaseItemIDs []int64) []int64 {
	if len(purchaseItemIDs) == 0 {
		return nil
	}
	var ids []int64
	db.Model(&models.PurchaseItem{}).
		Distinct("purchase_id").
		Where("id IN ?", purchaseItemIDs).
		Pluck("purchase_id", &ids)
	return ids
}

// RecalcPurchasesDeliveryStatus 對多個採購單依序呼叫 delivery.UpdateDeliveryStatus，去重且跳過 0。
func RecalcPurchasesDeliveryStatus(tx *gorm.DB, purchaseIDs []int64) error {
	seen := map[int64]bool{}
	for _, pid := range purchaseIDs {
		if pid == 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		if err := delivery.UpdateDeliveryStatus(tx, pid); err != nil {
			return err
		}
	}
	return nil
}
