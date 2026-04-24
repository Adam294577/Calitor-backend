package controllers

import (
	"fmt"
	"net/http"
	"project/models"
	"project/services/inventory"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateStockBatch 批次建立多張進貨單(單一事務,連號產生,失敗整體 rollback)
// 主要給條碼進貨使用:一次 TXT 解析後,多家廠商各建一張
func CreateStockBatch(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		SharedHeader struct {
			StockDate       string  `json:"stock_date" binding:"required"`
			CustomerID      int64   `json:"customer_id" binding:"required"`
			StockMode       int     `json:"stock_mode"`
			DealMode        int     `json:"deal_mode"`
			FillPersonID    *int64  `json:"fill_person_id"`
			CloseMonth      string  `json:"close_month"`
			Remark          string  `json:"remark"`
			TaxMode         int     `json:"tax_mode"`
			TaxRate         float64 `json:"tax_rate"`
			DiscountPercent float64 `json:"discount_percent"`
			InputMode       int     `json:"input_mode"`
		} `json:"shared_header" binding:"required"`
		Stocks []struct {
			VendorID      int64  `json:"vendor_id" binding:"required"`
			VendorStockNo string `json:"vendor_stock_no"`
			Items         []struct {
				ProductID      int64   `json:"product_id"`
				SizeGroupID    *int64  `json:"size_group_id"`
				PurchaseItemID *int64  `json:"purchase_item_id"`
				ItemOrder      int     `json:"item_order"`
				AdvicePrice    float64 `json:"advice_price"`
				Discount       float64 `json:"discount"`
				PurchasePrice  float64 `json:"purchase_price"`
				NonTaxPrice    float64 `json:"non_tax_price"`
				Supplement     int     `json:"supplement"`
				Sizes          []struct {
					SizeOptionID int64 `json:"size_option_id"`
					Qty          int   `json:"qty"`
				} `json:"sizes"`
			} `json:"items"`
		} `json:"stocks" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	if len(req.Stocks) == 0 {
		resp.Fail(http.StatusBadRequest, "無進貨單資料").Send()
		return
	}

	// 查客戶 BranchCode
	var customer models.RetailCustomer
	if err := db.GetRead().Where("id = ?", req.SharedHeader.CustomerID).First(&customer).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "客戶不存在").Send()
		return
	}

	// 預設值
	sh := req.SharedHeader
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

	// 產單號前綴
	prefix := "I"
	if sh.StockMode == 2 {
		prefix = "B"
	}
	yyyymm := ""
	if len(sh.StockDate) >= 6 {
		yyyymm = sh.StockDate[:6]
	}
	noPrefix := prefix + customer.BranchCode + yyyymm

	// 紀錄者
	adminId, _ := c.Get("AdminId")
	recorderID := int64(0)
	if id, ok := adminId.(float64); ok {
		recorderID = int64(id)
	}

	// 結果集
	type createdInfo struct {
		ID         int64  `json:"id"`
		StockNo    string `json:"stock_no"`
		VendorID   int64  `json:"vendor_id"`
		VendorName string `json:"vendor_name"`
	}
	var created []createdInfo

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 起始流水號:事務內取 MAX 後逐筆 +1
		var maxNo string
		if err := tx.Unscoped().Model(&models.Stock{}).
			Where("stock_no LIKE ?", noPrefix+"%").
			Select("MAX(stock_no)").
			Scan(&maxNo).Error; err != nil {
			return fmt.Errorf("查詢流水號失敗:%w", err)
		}

		seq := 1
		if maxNo != "" && len(maxNo) > len(noPrefix) {
			tail := maxNo[len(noPrefix):]
			if n, err := strconv.Atoi(tail); err == nil {
				seq = n + 1
			}
		}

		// 收集所有涉及的 purchase_item_ids (最後一次性 recalc)
		var allPurchaseItemIDs []int64

		for idx := range req.Stocks {
			st := req.Stocks[idx]
			stockNo := fmt.Sprintf("%s%04d", noPrefix, seq)
			seq++

			// 查 vendor 名稱
			var vendor models.Vendor
			if err := tx.Where("id = ?", st.VendorID).First(&vendor).Error; err != nil {
				return fmt.Errorf("第 %d 張:廠商 ID %d 不存在", idx+1, st.VendorID)
			}
			vendorName := vendor.ShortName
			if vendorName == "" {
				vendorName = vendor.Name
			}

			stock := models.Stock{
				StockNo:         stockNo,
				StockDate:       sh.StockDate,
				CustomerID:      sh.CustomerID,
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
			}
			if err := tx.Create(&stock).Error; err != nil {
				return fmt.Errorf("第 %d 張:建立失敗 %v", idx+1, err)
			}

			for itemIdx, reqItem := range st.Items {
				totalQty := 0
				for _, s := range reqItem.Sizes {
					totalQty += s.Qty
				}
				totalAmount := float64(totalQty) * reqItem.PurchasePrice

				item := models.StockItem{
					StockID:        stock.ID,
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
					return fmt.Errorf("第 %d 張第 %d 筆明細建立失敗 %v", idx+1, itemIdx+1, err)
				}
				for _, s := range reqItem.Sizes {
					size := models.StockItemSize{
						StockItemID:  item.ID,
						SizeOptionID: s.SizeOptionID,
						Qty:          s.Qty,
					}
					if err := tx.Create(&size).Error; err != nil {
						return err
					}
				}

				if reqItem.PurchaseItemID != nil {
					allPurchaseItemIDs = append(allPurchaseItemIDs, *reqItem.PurchaseItemID)
				}
			}

			// 調整庫存:進貨加、退貨扣
			multiplier := 1
			if sh.StockMode == 2 {
				multiplier = -1
			}
			var adjustItems []inventory.StockAdjustItem
			for _, reqItem := range st.Items {
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
			if err := inventory.AdjustStockBatch(tx, stock.CustomerID, adjustItems, multiplier); err != nil {
				return fmt.Errorf("第 %d 張:庫存調整失敗 %v", idx+1, err)
			}

			created = append(created, createdInfo{
				ID:         stock.ID,
				StockNo:    stock.StockNo,
				VendorID:   st.VendorID,
				VendorName: vendorName,
			})
		}

		// 統一 recalc 受影響採購單的交貨狀態
		purchaseIDs := distinctPurchaseIDs(tx, allPurchaseItemIDs)
		if err := recalcPurchasesDeliveryStatus(tx, purchaseIDs); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		resp.Fail(http.StatusBadRequest, err.Error()).Send()
		return
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"stocks": created,
	}).Send()
}
