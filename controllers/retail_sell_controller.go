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

// GetRetailSells 零售銷售單列表
func GetRetailSells(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.RetailSell
	query := db.GetRead().
		Select("retail_sells.*").
		Joins("JOIN retail_customers ON retail_customers.id = retail_sells.customer_id AND retail_customers.is_visible = true").
		Preload("Customer").
		Preload("SellPerson").
		Preload("Recorder").
		Order("retail_sells.sell_date DESC, retail_sells.id DESC")

	if v := c.Query("search"); v != "" {
		query = ApplySearch(query, v, "sell_no")
	}
	if v := c.Query("customer_id"); v != "" {
		query = query.Where("retail_sells.customer_id = ?", v)
	}
	if v := c.Query("date_from"); v != "" {
		query = query.Where("sell_date >= ?", v)
	}
	if v := c.Query("date_to"); v != "" {
		query = query.Where("sell_date <= ?", v)
	}

	paged, total := Paginate(c, query, &models.RetailSell{})
	paged.Find(&items)

	// 計算每筆銷售單的明細數量合計
	type qtyRow struct {
		RetailSellID int64
		SumQty       int
	}
	var sellIDs []int64
	for _, it := range items {
		sellIDs = append(sellIDs, it.ID)
	}
	qtyMap := map[int64]int{}
	if len(sellIDs) > 0 {
		var rows []qtyRow
		db.GetRead().Model(&models.RetailSellItem{}).
			Select("retail_sell_id, COALESCE(SUM(total_qty), 0) as sum_qty").
			Where("retail_sell_id IN ?", sellIDs).
			Group("retail_sell_id").
			Scan(&rows)
		for _, r := range rows {
			qtyMap[r.RetailSellID] = r.SumQty
		}
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"items":   items,
		"qty_map": qtyMap,
	}).SetTotal(total).Send()
}

// GetRetailSell 零售銷售單詳情
func GetRetailSell(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	// 先查 sell_store 以便篩選庫存
	var sellStore string
	db.GetRead().Model(&models.RetailSell{}).Where("id = ?", id).Pluck("sell_store", &sellStore)

	var item models.RetailSell
	query := db.GetRead().
		Preload("Customer").
		Preload("SellPerson").
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
		Preload("Items.Member")
	if sellStore != "" {
		query = query.Preload("Items.Product.SizeStocks", "customer_id IN (SELECT id FROM retail_customers WHERE branch_code = ?)", sellStore)
	} else {
		query = query.Preload("Items.Product.SizeStocks")
	}
	err = query.Where("id = ?", id).First(&item).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			resp.Fail(http.StatusNotFound, "銷售單不存在").Send()
			return
		}
		resp.Panic(err).Send()
		return
	}

	resp.Success("成功").SetData(item).Send()
}

// CreateRetailSell 新增零售銷售單
func CreateRetailSell(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		SellDate      string  `json:"sell_date" binding:"required"`
		CustomerID    int64   `json:"customer_id" binding:"required"`
		SellStore     string  `json:"sell_store"`
		SellPersonID  *int64  `json:"sell_person_id"`
		CashAmount    float64 `json:"cash_amount"`
		CardAmount    float64 `json:"card_amount"`
		GiftAmount    float64 `json:"gift_amount"`
		TaxRate       float64 `json:"tax_rate"`
		TaxID         string  `json:"tax_id"`
		InvoiceAmount float64 `json:"invoice_amount"`
		CardType      string  `json:"card_type"`
		GiftType      string  `json:"gift_type"`
		CreditCardNo  string  `json:"credit_card_no"`
		IsAbnormal    bool    `json:"is_abnormal"`
		Remark        string  `json:"remark"`
		Items         []struct {
			ProductID   int64   `json:"product_id"`
			SizeGroupID *int64  `json:"size_group_id"`
			MemberID    *int64  `json:"member_id"`
			ItemOrder   int     `json:"item_order"`
			SellPrice   float64 `json:"sell_price"`
			Discount    float64 `json:"discount"`
			SellMode    int     `json:"sell_mode"`
			CashAmount  float64 `json:"cash_amount"`
			CardAmount  float64 `json:"card_amount"`
			GiftAmount  float64 `json:"gift_amount"`
			Sizes       []struct {
				SizeOptionID int64 `json:"size_option_id"`
				Qty          int   `json:"qty"`
			} `json:"sizes"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 查詢客戶取 branch_code
	var customer models.RetailCustomer
	if err := db.GetRead().Where("id = ?", req.CustomerID).First(&customer).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "客戶不存在").Send()
		return
	}

	sellStore := req.SellStore
	if sellStore == "" {
		sellStore = customer.BranchCode
	}

	// 產生單號: {BranchID}{YYYYMMDD}{流水號3碼}
	yyyymmdd := req.SellDate
	if len(yyyymmdd) > 8 {
		yyyymmdd = yyyymmdd[:8]
	}
	noPrefix := sellStore + yyyymmdd

	var maxNo string
	db.GetRead().Unscoped().Model(&models.RetailSell{}).
		Where("sell_no LIKE ?", noPrefix+"%").
		Select("MAX(sell_no)").
		Scan(&maxNo)

	seq := 1
	if maxNo != "" && len(maxNo) > len(noPrefix) {
		tail := maxNo[len(noPrefix):]
		if n, err := strconv.Atoi(tail); err == nil {
			seq = n + 1
		}
	}
	sellNo := fmt.Sprintf("%s%03d", noPrefix, seq)

	adminId, _ := c.Get("AdminId")
	recorderID := int64(0)
	if id, ok := adminId.(float64); ok {
		recorderID = int64(id)
	}

	retailSell := models.RetailSell{
		SellNo:        sellNo,
		SellDate:      req.SellDate,
		CustomerID:    req.CustomerID,
		SellStore:     sellStore,
		SellPersonID:  req.SellPersonID,
		RecorderID:    recorderID,
		CashAmount:    req.CashAmount,
		CardAmount:    req.CardAmount,
		GiftAmount:    req.GiftAmount,
		TaxRate:       req.TaxRate,
		TaxID:         req.TaxID,
		InvoiceAmount: req.InvoiceAmount,
		CardType:      req.CardType,
		GiftType:      req.GiftType,
		CreditCardNo:  req.CreditCardNo,
		IsAbnormal:    req.IsAbnormal,
		Remark:        req.Remark,
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&retailSell).Error; err != nil {
			return err
		}

		for _, reqItem := range req.Items {
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}

			sellMode := reqItem.SellMode
			if sellMode == 0 {
				sellMode = 1
			}

			// 計算金額：折扣為折數（如 85 表示 85 折），0 表示不打折
			unitPrice := reqItem.SellPrice
			if reqItem.Discount > 0 && reqItem.Discount < 100 {
				unitPrice = math.Round(reqItem.SellPrice * reqItem.Discount / 100)
			}
			totalAmount := math.Round(float64(totalQty) * unitPrice)

			// 贈品金額為 0
			if sellMode == 7 {
				totalAmount = 0
			}

			item := models.RetailSellItem{
				RetailSellID: retailSell.ID,
				ItemOrder:    reqItem.ItemOrder,
				ProductID:    reqItem.ProductID,
				SizeGroupID:  reqItem.SizeGroupID,
				MemberID:     reqItem.MemberID,
				SellPrice:    reqItem.SellPrice,
				Discount:     reqItem.Discount,
				TotalQty:     totalQty,
				TotalAmount:  totalAmount,
				CashAmount:   reqItem.CashAmount,
				CardAmount:   reqItem.CardAmount,
				GiftAmount:   reqItem.GiftAmount,
				SellMode:     sellMode,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			for _, s := range reqItem.Sizes {
				size := models.RetailSellItemSize{
					RetailSellItemID: item.ID,
					SizeOptionID:     s.SizeOptionID,
					Qty:              s.Qty,
				}
				if err := tx.Create(&size).Error; err != nil {
					return err
				}
			}
		}

		// 自動彙總明細的付款到主表
		var sumCash, sumCard, sumGift float64
		for _, reqItem := range req.Items {
			sumCash += reqItem.CashAmount
			sumCard += reqItem.CardAmount
			sumGift += reqItem.GiftAmount
		}
		tx.Model(&retailSell).Updates(map[string]interface{}{
			"cash_amount": sumCash,
			"card_amount": sumCard,
			"gift_amount": sumGift,
		})

		// 調整庫存：銷貨/贈品扣庫存，退貨加庫存
		var storeCustomer models.RetailCustomer
		if sellStore != "" {
			if err := tx.Where("branch_code = ?", sellStore).First(&storeCustomer).Error; err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
		}
		if storeCustomer.ID > 0 {
			for _, reqItem := range req.Items {
				sellMode := reqItem.SellMode
				if sellMode == 0 {
					sellMode = 1
				}
				multiplier := -1 // 銷貨/贈品扣庫存
				if sellMode == 2 {
					multiplier = 1 // 退貨加庫存
				}
				var sizes []inventory.StockAdjustSize
				for _, s := range reqItem.Sizes {
					if s.Qty > 0 {
						sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
					}
				}
				if len(sizes) > 0 {
					adjItems := []inventory.StockAdjustItem{{ProductID: reqItem.ProductID, Sizes: sizes}}
					if err := inventory.AdjustStockBatch(tx, storeCustomer.ID, adjItems, multiplier); err != nil {
						return err
					}
				}
			}
		}

		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("新增成功").SetData(retailSell).Send()
}

// UpdateRetailSell 更新零售銷售單
func UpdateRetailSell(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.RetailSell
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusNotFound, "銷售單不存在").Send()
		return
	}

	var req struct {
		SellDate      string  `json:"sell_date"`
		CustomerID    int64   `json:"customer_id"`
		SellStore     string  `json:"sell_store"`
		SellPersonID  *int64  `json:"sell_person_id"`
		CashAmount    float64 `json:"cash_amount"`
		CardAmount    float64 `json:"card_amount"`
		GiftAmount    float64 `json:"gift_amount"`
		TaxRate       float64 `json:"tax_rate"`
		TaxID         string  `json:"tax_id"`
		InvoiceAmount float64 `json:"invoice_amount"`
		CardType      string  `json:"card_type"`
		GiftType      string  `json:"gift_type"`
		CreditCardNo  string  `json:"credit_card_no"`
		IsAbnormal    bool    `json:"is_abnormal"`
		Remark        string  `json:"remark"`
		Items         []struct {
			ProductID   int64   `json:"product_id"`
			SizeGroupID *int64  `json:"size_group_id"`
			MemberID    *int64  `json:"member_id"`
			ItemOrder   int     `json:"item_order"`
			SellPrice   float64 `json:"sell_price"`
			Discount    float64 `json:"discount"`
			SellMode    int     `json:"sell_mode"`
			CashAmount  float64 `json:"cash_amount"`
			CardAmount  float64 `json:"card_amount"`
			GiftAmount  float64 `json:"gift_amount"`
			Sizes       []struct {
				SizeOptionID int64 `json:"size_option_id"`
				Qty          int   `json:"qty"`
			} `json:"sizes"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 還原舊庫存
		var storeCustomerOld models.RetailCustomer
		if existing.SellStore != "" {
			if err := tx.Where("branch_code = ?", existing.SellStore).First(&storeCustomerOld).Error; err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
		}
		if storeCustomerOld.ID > 0 {
			var oldItems []models.RetailSellItem
			if err := tx.Preload("Sizes").Where("retail_sell_id = ?", id).Find(&oldItems).Error; err != nil {
				return err
			}
			for _, oi := range oldItems {
				sellMode := oi.SellMode
				if sellMode == 0 {
					sellMode = 1
				}
				oldMul := 1 // 銷貨/贈品的舊資料要加回
				if sellMode == 2 {
					oldMul = -1 // 退貨的舊資料要扣回
				}
				var sizes []inventory.StockAdjustSize
				for _, s := range oi.Sizes {
					if s.Qty > 0 {
						sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
					}
				}
				if len(sizes) > 0 {
					adjItems := []inventory.StockAdjustItem{{ProductID: oi.ProductID, Sizes: sizes}}
					if err := inventory.AdjustStockBatch(tx, storeCustomerOld.ID, adjItems, oldMul); err != nil {
						return err
					}
				}
			}
		}

		// 刪除舊明細
		var oldItemIDs []int64
		if err := tx.Model(&models.RetailSellItem{}).Where("retail_sell_id = ?", id).Pluck("id", &oldItemIDs).Error; err != nil {
			return err
		}
		if len(oldItemIDs) > 0 {
			if err := tx.Where("retail_sell_item_id IN ?", oldItemIDs).Delete(&models.RetailSellItemSize{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("retail_sell_id = ?", id).Delete(&models.RetailSellItem{}).Error; err != nil {
			return err
		}

		// 更新主表（付款金額在明細建立後彙總）
		updates := map[string]interface{}{
			"sell_date":      req.SellDate,
			"customer_id":    req.CustomerID,
			"sell_store":     req.SellStore,
			"sell_person_id": req.SellPersonID,
			"tax_rate":       req.TaxRate,
			"tax_id":         req.TaxID,
			"invoice_amount": req.InvoiceAmount,
			"card_type":      req.CardType,
			"gift_type":      req.GiftType,
			"credit_card_no": req.CreditCardNo,
			"is_abnormal":    req.IsAbnormal,
			"remark":         req.Remark,
		}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}

		// 新增明細
		var sumCash, sumCard, sumGift float64
		for _, reqItem := range req.Items {
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}

			sellMode := reqItem.SellMode
			if sellMode == 0 {
				sellMode = 1
			}

			unitPrice := reqItem.SellPrice
			if reqItem.Discount > 0 && reqItem.Discount < 100 {
				unitPrice = math.Round(reqItem.SellPrice * reqItem.Discount / 100)
			}
			totalAmount := math.Round(float64(totalQty) * unitPrice)
			if sellMode == 7 {
				totalAmount = 0
			}

			sumCash += reqItem.CashAmount
			sumCard += reqItem.CardAmount
			sumGift += reqItem.GiftAmount

			item := models.RetailSellItem{
				RetailSellID: id,
				ItemOrder:    reqItem.ItemOrder,
				ProductID:    reqItem.ProductID,
				SizeGroupID:  reqItem.SizeGroupID,
				MemberID:     reqItem.MemberID,
				SellPrice:    reqItem.SellPrice,
				Discount:     reqItem.Discount,
				TotalQty:     totalQty,
				TotalAmount:  totalAmount,
				CashAmount:   reqItem.CashAmount,
				CardAmount:   reqItem.CardAmount,
				GiftAmount:   reqItem.GiftAmount,
				SellMode:     sellMode,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			for _, s := range reqItem.Sizes {
				size := models.RetailSellItemSize{
					RetailSellItemID: item.ID,
					SizeOptionID:     s.SizeOptionID,
					Qty:              s.Qty,
				}
				if err := tx.Create(&size).Error; err != nil {
					return err
				}
			}
		}

		// 彙總明細付款到主表
		tx.Model(&existing).Updates(map[string]interface{}{
			"cash_amount": sumCash,
			"card_amount": sumCard,
			"gift_amount": sumGift,
		})

		// 套用新庫存
		var storeCustomerNew models.RetailCustomer
		if req.SellStore != "" {
			if err := tx.Where("branch_code = ?", req.SellStore).First(&storeCustomerNew).Error; err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
		}
		if storeCustomerNew.ID > 0 {
			for _, reqItem := range req.Items {
				sellMode := reqItem.SellMode
				if sellMode == 0 {
					sellMode = 1
				}
				newMul := -1 // 銷貨/贈品扣庫存
				if sellMode == 2 {
					newMul = 1 // 退貨加庫存
				}
				var sizes []inventory.StockAdjustSize
				for _, s := range reqItem.Sizes {
					if s.Qty > 0 {
						sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
					}
				}
				if len(sizes) > 0 {
					adjItems := []inventory.StockAdjustItem{{ProductID: reqItem.ProductID, Sizes: sizes}}
					if err := inventory.AdjustStockBatch(tx, storeCustomerNew.ID, adjItems, newMul); err != nil {
						return err
					}
				}
			}
		}

		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("更新成功").Send()
}

// DeleteRetailSell 軟刪除零售銷售單
func DeleteRetailSell(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var sell models.RetailSell
	if err := db.GetRead().Where("id = ?", id).First(&sell).Error; err != nil {
		resp.Fail(http.StatusNotFound, "銷售單不存在").Send()
		return
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 還原庫存
		var storeCustomer models.RetailCustomer
		if sell.SellStore != "" {
			if err := tx.Where("branch_code = ?", sell.SellStore).First(&storeCustomer).Error; err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
		}
		if storeCustomer.ID > 0 {
			var items []models.RetailSellItem
			if err := tx.Preload("Sizes").Where("retail_sell_id = ?", id).Find(&items).Error; err != nil {
				return err
			}
			for _, item := range items {
				sellMode := item.SellMode
				if sellMode == 0 {
					sellMode = 1
				}
				multiplier := 1 // 銷貨/贈品刪除 → 加回庫存
				if sellMode == 2 {
					multiplier = -1 // 退貨刪除 → 扣回庫存
				}
				var sizes []inventory.StockAdjustSize
				for _, s := range item.Sizes {
					if s.Qty > 0 {
						sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
					}
				}
				if len(sizes) > 0 {
					adjItems := []inventory.StockAdjustItem{{ProductID: item.ProductID, Sizes: sizes}}
					if err := inventory.AdjustStockBatch(tx, storeCustomer.ID, adjItems, multiplier); err != nil {
						return err
					}
				}
			}
		}

		if err := tx.Delete(&models.RetailSell{}, id).Error; err != nil {
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
