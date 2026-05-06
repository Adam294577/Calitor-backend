package controllers

import (
	"fmt"
	"math"
	"net/http"
	"project/models"
	"project/services/inventory"
	"project/services/log"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// BatchShipmentSize 單一尺碼的出貨量。
type BatchShipmentSize struct {
	SizeOptionID int64 `json:"size_option_id"`
	Qty          int   `json:"qty"`
}

// BatchShipmentItem 單一出貨明細。
type BatchShipmentItem struct {
	ProductID   int64               `json:"product_id"`
	SizeGroupID *int64              `json:"size_group_id"`
	OrderItemID *int64              `json:"order_item_id"`
	ItemOrder   int                 `json:"item_order"`
	SellPrice   float64             `json:"sell_price"`
	Discount    float64             `json:"discount"`
	ShipPrice   float64             `json:"ship_price"`
	NonTaxPrice float64             `json:"non_tax_price"`
	ShipCost    float64             `json:"ship_cost"`
	Supplement  int                 `json:"supplement"`
	Sizes       []BatchShipmentSize `json:"sizes"`
}

// BatchShipmentEntry 批次中單張出貨單(對應一個客戶)。
type BatchShipmentEntry struct {
	CustomerID    int64               `json:"customer_id" binding:"required"`
	ShipStore     string              `json:"ship_store"`
	TaxRate       float64             `json:"tax_rate"`
	Remark        string              `json:"remark"`
	DiscountAmt   float64             `json:"discount_amount"`
	TaxAmount     float64             `json:"tax_amount"`
	InvoiceAmount float64             `json:"invoice_amount"`
	ChargeAmount  float64             `json:"charge_amount"`
	Items         []BatchShipmentItem `json:"items"`
}

// BatchShipmentSharedHeader 批次共用表頭。
type BatchShipmentSharedHeader struct {
	ShipmentDate    string  `json:"shipment_date" binding:"required"`
	ShipmentMode    int     `json:"shipment_mode"`
	DealMode        int     `json:"deal_mode"`
	FillPersonID    *int64  `json:"fill_person_id"`
	SalesmanID      *int64  `json:"salesman_id"`
	CloseMonth      string  `json:"close_month"`
	TaxMode         int     `json:"tax_mode"`
	DiscountPercent float64 `json:"discount_percent"`
	InvoiceNo       string  `json:"invoice_no"`
	ClientGoodID    string  `json:"client_good_id"`
	InputMode       int     `json:"input_mode"`
}

// BatchShipmentPayload 批次建立出貨單的完整輸入。
type BatchShipmentPayload struct {
	SharedHeader BatchShipmentSharedHeader `json:"shared_header" binding:"required"`
	Shipments    []BatchShipmentEntry      `json:"shipments" binding:"required"`
}

// BatchShipmentCreated 建立成功的單張出貨摘要。
type BatchShipmentCreated struct {
	ID           int64  `json:"id"`
	ShipmentNo   string `json:"shipment_no"`
	CustomerID   int64  `json:"customer_id"`
	CustomerName string `json:"customer_name"`
}

// BatchShipmentSkipped 因未繳期等原因被跳過的客戶資訊。
type BatchShipmentSkipped struct {
	CustomerID   int64  `json:"customer_id"`
	CustomerName string `json:"customer_name"`
	CutoffMonth  string `json:"cutoff_month"`
	Count        int64  `json:"count"`
	Reason       string `json:"reason"` // overdue
}

// CreateShipmentBatch 批次建立多張出貨單(單一交易,失敗整體 rollback)
// 主要給條碼出貨使用:一次 TXT 解析後,多客戶各建一張
func CreateShipmentBatch(c *gin.Context) {
	resp := response.New(c)
	var payload BatchShipmentPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}
	if len(payload.Shipments) == 0 {
		resp.Fail(http.StatusBadRequest, "無出貨單資料").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	adminId, _ := c.Get("AdminId")
	recorderID := int64(0)
	if id, ok := adminId.(float64); ok {
		recorderID = int64(id)
	}

	sh := payload.SharedHeader
	if sh.ShipmentMode == 0 {
		sh.ShipmentMode = 3
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
	if sh.InputMode == 0 {
		sh.InputMode = 2
	}

	prefix := "S"
	if sh.ShipmentMode == 4 {
		prefix = "R"
	}

	// 效能 log:條碼匯入未來查調批次規模用,後續評估是否需 N+1 INSERT 優化
	{
		totalItems := 0
		totalSizes := 0
		maxItemsPerCust := 0
		for _, e := range payload.Shipments {
			n := len(e.Items)
			totalItems += n
			if n > maxItemsPerCust {
				maxItemsPerCust = n
			}
			for _, it := range e.Items {
				totalSizes += len(it.Sizes)
			}
		}
		log.Info("[shipment_batch] admin=%d customers=%d items=%d sizes=%d max_items_per_cust=%d mode=%d",
			recorderID, len(payload.Shipments), totalItems, totalSizes, maxItemsPerCust, sh.ShipmentMode)
	}

	// 月份未沖帳檢查:逐客戶過濾,未繳期客戶從 payload 移除並記入 skipped(出貨/退貨皆過濾)
	// 前端正常路徑已過濾,這裡作為 defense-in-depth + race condition 補救
	var skipped []BatchShipmentSkipped
	{
		filtered := make([]BatchShipmentEntry, 0, len(payload.Shipments))
		for idx := range payload.Shipments {
			entry := payload.Shipments[idx]
			customerPtr, verr := EnsureCustomerVisible(db.GetRead(), entry.CustomerID)
			if verr != nil {
				resp.Fail(http.StatusBadRequest, fmt.Sprintf("第 %d 張:%s (ID %d)", idx+1, ErrMsgCustomerNotVisible, entry.CustomerID)).Send()
				return
			}
			customer := *customerPtr
			cm, cnt, cerr := CheckCustomerOverdueShipments(db.GetRead(), entry.CustomerID, sh.ShipmentDate, customer.Month)
			if cerr != nil {
				resp.Fail(http.StatusInternalServerError, fmt.Sprintf("檢查未繳期失敗: %v", cerr)).Send()
				return
			}
			if cnt > 0 {
				skipped = append(skipped, BatchShipmentSkipped{
					CustomerID:   entry.CustomerID,
					CustomerName: customer.Name,
					CutoffMonth:  cm,
					Count:        cnt,
					Reason:       "overdue",
				})
				continue
			}
			filtered = append(filtered, entry)
		}
		payload.Shipments = filtered
	}

	if len(payload.Shipments) == 0 {
		resp.Success("無可建立的出貨單(全部客戶未繳期已跳過)").SetData(map[string]interface{}{
			"shipments": []BatchShipmentCreated{},
			"skipped":   skipped,
		}).Send()
		return
	}

	var created []BatchShipmentCreated

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 對每張出貨單分配連號(以 ship_store 為單位)
		seqByPrefix := map[string]int{}

		for idx := range payload.Shipments {
			entry := payload.Shipments[idx]

			customerPtr, verr := EnsureCustomerVisible(tx, entry.CustomerID)
			if verr != nil {
				return fmt.Errorf("第 %d 張:%s (ID %d)", idx+1, ErrMsgCustomerNotVisible, entry.CustomerID)
			}
			customer := *customerPtr

			yyyymm := ""
			if len(sh.ShipmentDate) >= 6 {
				yyyymm = sh.ShipmentDate[:6]
			}
			noPrefix := prefix + entry.ShipStore + yyyymm

			seq, exists := seqByPrefix[noPrefix]
			if !exists {
				// 用 Postgres advisory lock 鎖在 (noPrefix) 上,避免並發撞號
				// 鎖在 transaction 結束自動釋放,不影響其他 noPrefix 的並發寫入
				if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "shipment_no:"+noPrefix).Error; err != nil {
					return fmt.Errorf("第 %d 張:取得流水號鎖失敗 %w", idx+1, err)
				}
				var maxNo string
				if err := tx.Unscoped().Model(&models.Shipment{}).
					Where("shipment_no LIKE ?", noPrefix+"%").
					Select("COALESCE(MAX(shipment_no), '')").
					Scan(&maxNo).Error; err != nil {
					return fmt.Errorf("第 %d 張:查詢出貨單流水號失敗 %w", idx+1, err)
				}
				seq = 1
				if maxNo != "" && len(maxNo) > len(noPrefix) {
					tail := maxNo[len(noPrefix):]
					if n, perr := strconv.Atoi(tail); perr == nil {
						seq = n + 1
					}
				}
			}
			shipmentNo := fmt.Sprintf("%s%04d", noPrefix, seq)
			seqByPrefix[noPrefix] = seq + 1

			// 入帳月份(若沒帶,以 ShipmentDate 推算)
			closeMonth := sh.CloseMonth
			if closeMonth == "" && len(sh.ShipmentDate) >= 8 {
				y, _ := strconv.Atoi(sh.ShipmentDate[:4])
				m, _ := strconv.Atoi(sh.ShipmentDate[4:6])
				d, _ := strconv.Atoi(sh.ShipmentDate[6:8])
				closingDay := customer.ClosingDate
				if closingDay <= 0 {
					closingDay = 26
				}
				if d > closingDay {
					m++
					if m > 12 {
						m = 1
						y++
					}
				}
				closeMonth = fmt.Sprintf("%04d%02d", y, m)
			} else if closeMonth == "" && len(sh.ShipmentDate) >= 6 {
				closeMonth = sh.ShipmentDate[:6]
			}

			// 計算 totalShipAmount / dealAmount
			// 一律 math.Round 為整數(對齊規範:金額存整數,前端顯示什麼 DB 就存什麼)
			var totalShipAmount float64
			for _, reqItem := range entry.Items {
				qty := 0
				for _, s := range reqItem.Sizes {
					qty += s.Qty
				}
				totalShipAmount += float64(qty) * reqItem.ShipPrice
			}
			totalShipAmount = math.Round(totalShipAmount)
			taxAmt := entry.TaxAmount
			if taxAmt == 0 && sh.TaxMode == 2 {
				taxAmt = math.Round(totalShipAmount * entry.TaxRate / 100)
			}
			dealAmount := math.Round(totalShipAmount + taxAmt - entry.DiscountAmt)
			if sh.ShipmentMode == 4 {
				dealAmount = -dealAmount
			}

			shipment := models.Shipment{
				ShipmentNo:      shipmentNo,
				ShipmentDate:    sh.ShipmentDate,
				CustomerID:      entry.CustomerID,
				ShipmentMode:    sh.ShipmentMode,
				DealMode:        sh.DealMode,
				ShipStore:       entry.ShipStore,
				FillPersonID:    sh.FillPersonID,
				SalesmanID:      sh.SalesmanID,
				RecorderID:      recorderID,
				CloseMonth:      closeMonth,
				Remark:          entry.Remark,
				TaxMode:         sh.TaxMode,
				TaxRate:         entry.TaxRate,
				TaxAmount:       taxAmt,
				DiscountPercent: sh.DiscountPercent,
				DiscountAmount:  entry.DiscountAmt,
				InvoiceDate:     sh.ShipmentDate,
				InvoiceNo:       sh.InvoiceNo,
				InvoiceAmount:   entry.InvoiceAmount,
				ChargeAmount:    entry.ChargeAmount,
				ClientGoodID:    sh.ClientGoodID,
				InputMode:       sh.InputMode,
			}
			if err := tx.Create(&shipment).Error; err != nil {
				return fmt.Errorf("第 %d 張:建立失敗 %v", idx+1, err)
			}

			orderIDSet := map[int64]bool{}

			for itemIdx, reqItem := range entry.Items {
				totalQty := 0
				for _, s := range reqItem.Sizes {
					totalQty += s.Qty
				}
				totalAmount := math.Round(float64(totalQty) * reqItem.ShipPrice)

				orderItemID := reqItem.OrderItemID
				if orderItemID != nil && *orderItemID == 0 {
					orderItemID = nil
				}

				item := models.ShipmentItem{
					ShipmentID:  shipment.ID,
					ProductID:   reqItem.ProductID,
					SizeGroupID: reqItem.SizeGroupID,
					OrderItemID: orderItemID,
					ItemOrder:   reqItem.ItemOrder,
					SellPrice:   reqItem.SellPrice,
					Discount:    reqItem.Discount,
					ShipPrice:   reqItem.ShipPrice,
					NonTaxPrice: reqItem.NonTaxPrice,
					TotalQty:    totalQty,
					TotalAmount: totalAmount,
					ShipCost:    reqItem.ShipCost,
					Supplement:  reqItem.Supplement,
				}
				if err := tx.Create(&item).Error; err != nil {
					return fmt.Errorf("第 %d 張第 %d 筆明細建立失敗 %v", idx+1, itemIdx+1, err)
				}
				for _, s := range reqItem.Sizes {
					size := models.ShipmentItemSize{
						ShipmentItemID: item.ID,
						SizeOptionID:   s.SizeOptionID,
						Qty:            s.Qty,
					}
					if err := tx.Create(&size).Error; err != nil {
						return err
					}
				}

				if orderItemID != nil {
					var oi models.OrderItem
					if err := tx.Where("id = ?", *orderItemID).First(&oi).Error; err == nil {
						orderIDSet[oi.OrderID] = true
					} else if err != gorm.ErrRecordNotFound {
						return err
					}
				}
			}

			for orderID := range orderIDSet {
				if err := UpdateOrderDeliveryStatus(tx, orderID); err != nil {
					return err
				}
			}

			// 庫存:從 ship_store 扣
			if entry.ShipStore != "" {
				var storeCustomer models.RetailCustomer
				if err := tx.Where("branch_code = ?", entry.ShipStore).First(&storeCustomer).Error; err != nil && err != gorm.ErrRecordNotFound {
					return err
				}
				if storeCustomer.ID > 0 {
					multiplier := -1
					if sh.ShipmentMode == 4 {
						multiplier = 1
					}
					var adjustItems []inventory.StockAdjustItem
					for _, reqItem := range entry.Items {
						var sizes []inventory.StockAdjustSize
						for _, s := range reqItem.Sizes {
							if s.Qty > 0 {
								sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
							}
						}
						if len(sizes) > 0 {
							adjustItems = append(adjustItems, inventory.StockAdjustItem{ProductID: reqItem.ProductID, Sizes: sizes})
						}
					}
					if err := inventory.AdjustStockBatch(tx, storeCustomer.ID, adjustItems, multiplier); err != nil {
						return fmt.Errorf("第 %d 張:庫存調整失敗 %v", idx+1, err)
					}
				}
			}

			if err := tx.Model(&shipment).Update("deal_amount", dealAmount).Error; err != nil {
				return fmt.Errorf("第 %d 張:更新成交金額失敗 %w", idx+1, err)
			}

			created = append(created, BatchShipmentCreated{
				ID:           shipment.ID,
				ShipmentNo:   shipment.ShipmentNo,
				CustomerID:   entry.CustomerID,
				CustomerName: customer.Name,
			})
		}

		return nil
	})

	if err != nil {
		resp.Fail(http.StatusBadRequest, err.Error()).Send()
		return
	}
	resp.Success("成功").SetData(map[string]interface{}{
		"shipments": created,
		"skipped":   skipped,
	}).Send()
}
