package controllers

import (
	"fmt"
	"math"
	"net/http"
	"project/models"
	"project/services/inventory"
	"project/services/receivable"
	response "project/services/responses"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetShipments 客戶出貨單列表
func GetShipments(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Shipment
	query := db.GetRead().
		Select("shipments.*").
		Joins("JOIN retail_customers ON retail_customers.id = shipments.customer_id AND retail_customers.is_visible = true").
		Preload("Customer").
		Preload("FillPerson").
		Preload("Recorder").
		Order("shipments.shipment_date DESC, shipments.id DESC")

	if v := c.Query("search"); v != "" {
		query = ApplySearch(query, v, "shipment_no")
	}
	if v := c.Query("customer_id"); v != "" {
		query = query.Where("shipments.customer_id = ?", v)
	}
	if v := c.Query("date_from"); v != "" {
		query = query.Where("shipments.shipment_date >= ?", v)
	}
	if v := c.Query("date_to"); v != "" {
		query = query.Where("shipments.shipment_date <= ?", v)
	}
	if v := c.Query("shipment_mode"); v != "" {
		query = query.Where("shipments.shipment_mode = ?", v)
	}

	paged, total := Paginate(c, query, &models.Shipment{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// GetShipment 客戶出貨單詳情
func GetShipment(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Shipment
	err = db.GetRead().
		Preload("Customer").
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
		Preload("Items.Product.CategoryMaps", func(db *gorm.DB) *gorm.DB {
			return db.Where("category_type = 5")
		}).
		Preload("Items.Product.CategoryMaps.Category5").
		Preload("Items.SizeGroup.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Sizes.SizeOption").
		Preload("Items.OrderItem").
		Where("id = ?", id).
		First(&item).Error
	if err != nil {
		resp.Fail(http.StatusNotFound, "出貨單不存在").Send()
		return
	}

	// 查關聯訂貨單號（透過 OrderItem → Order）
	type orderInfo struct {
		OrderItemID int64
		OrderNo     string
	}
	var orderInfos []orderInfo
	var orderItemIDs []int64
	for _, it := range item.Items {
		if it.OrderItemID != nil {
			orderItemIDs = append(orderItemIDs, *it.OrderItemID)
		}
	}
	if len(orderItemIDs) > 0 {
		db.GetRead().Model(&models.OrderItem{}).
			Select("order_items.id as order_item_id, orders.order_no").
			Joins("JOIN orders ON orders.id = order_items.order_id AND orders.deleted_at IS NULL").
			Where("order_items.id IN ?", orderItemIDs).
			Scan(&orderInfos)
	}
	orderNoMap := map[int64]string{}
	for _, oi := range orderInfos {
		orderNoMap[oi.OrderItemID] = oi.OrderNo
	}

	// 計算未沖銷總額
	var customer models.RetailCustomer
	db.GetRead().Where("id = ?", item.CustomerID).First(&customer)
	type balRow struct {
		TotalDeal   float64
		TotalCharge float64
	}
	var bal balRow
	db.GetRead().Model(&models.Shipment{}).
		Select("COALESCE(SUM(deal_amount), 0) as total_deal, COALESCE(SUM(charge_amount), 0) as total_charge").
		Where("customer_id = ? AND deleted_at IS NULL", item.CustomerID).
		Scan(&bal)
	outstanding := bal.TotalDeal - bal.TotalCharge

	resp.Success("成功").SetData(map[string]interface{}{
		"shipment":    item,
		"order_no_map": orderNoMap,
		"outstanding":  outstanding,
		"credit_limit": customer.CreditLimit,
	}).Send()
}

// CreateShipment 新增客戶出貨單
func CreateShipment(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		ShipmentDate    string  `json:"shipment_date" binding:"required"`
		CustomerID      int64   `json:"customer_id" binding:"required"`
		ShipmentMode    int     `json:"shipment_mode"`
		DealMode        int     `json:"deal_mode"`
		ShipStore       string  `json:"ship_store"`
		FillPersonID    *int64  `json:"fill_person_id"`
		CloseMonth      string  `json:"close_month"`
		Remark          string  `json:"remark"`
		TaxMode         int     `json:"tax_mode"`
		TaxRate         float64 `json:"tax_rate"`
		TaxAmount       float64 `json:"tax_amount"`
		DiscountPercent float64 `json:"discount_percent"`
		DiscountAmount  float64 `json:"discount_amount"`
		InvoiceDate     string  `json:"invoice_date"`
		InvoiceNo       string  `json:"invoice_no"`
		InvoiceAmount   float64 `json:"invoice_amount"`
		ChargeAmount    float64 `json:"charge_amount"`
		ClientGoodID    string  `json:"client_good_id"`
		InputMode       int     `json:"input_mode"`
		Items           []struct {
			ProductID   int64   `json:"product_id"`
			SizeGroupID *int64  `json:"size_group_id"`
			OrderItemID *int64  `json:"order_item_id"`
			ItemOrder   int     `json:"item_order"`
			SellPrice   float64 `json:"sell_price"`
			Discount    float64 `json:"discount"`
			ShipPrice   float64 `json:"ship_price"`
			NonTaxPrice float64 `json:"non_tax_price"`
			ShipCost    float64 `json:"ship_cost"`
			Supplement  int     `json:"supplement"`
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

	// 查詢客戶
	var customer models.RetailCustomer
	if err := db.GetRead().Where("id = ?", req.CustomerID).First(&customer).Error; err != nil {
		resp.Fail(http.StatusBadRequest, "客戶不存在").Send()
		return
	}

	// 出貨模式檢查信用額度和月份限制
	if req.ShipmentMode != 4 {
		// 信用額度檢查：CreditLimit > 0 時，未沖銷餘額不可超過
		if customer.CreditLimit > 0 {
			type balRow struct {
				TotalDeal   float64
				TotalCharge float64
			}
			var bal balRow
			db.GetRead().Model(&models.Shipment{}).
				Select("COALESCE(SUM(deal_amount), 0) as total_deal, COALESCE(SUM(charge_amount), 0) as total_charge").
				Where("customer_id = ? AND deleted_at IS NULL", req.CustomerID).
				Scan(&bal)
			outstanding := bal.TotalDeal - bal.TotalCharge
			if outstanding >= customer.CreditLimit {
				resp.Fail(http.StatusBadRequest, fmt.Sprintf("已超過信用額度（額度: %.0f，未沖銷: %.0f）", customer.CreditLimit, outstanding)).Send()
				return
			}
		}

		// 月份限制檢查：Month 有值時，往前推 N 個月的出貨不得有未繳清
		if customer.Month != "" && customer.Month != "0" {
			months, _ := strconv.Atoi(customer.Month)
			if months > 0 {
				// 計算 N 個月前的 YYYYMM
				now := req.ShipmentDate
				if len(now) >= 6 {
					y, _ := strconv.Atoi(now[:4])
					m, _ := strconv.Atoi(now[4:6])
					m -= months
					for m <= 0 {
						m += 12
						y--
					}
					cutoffMonth := fmt.Sprintf("%04d%02d", y, m)

					var unclearedCount int64
					db.GetRead().Raw(`
						SELECT COUNT(*) FROM shipments s
						`+receivable.GatherDetailsAggJoin+`
						WHERE s.customer_id = ? AND s.deleted_at IS NULL AND s.shipment_mode = 3 AND s.close_month <= ?
						AND (`+receivable.OutstandingRoundedExpr+`) > 0
					`, req.CustomerID, cutoffMonth).Scan(&unclearedCount)
					if unclearedCount > 0 {
						resp.Fail(http.StatusBadRequest, fmt.Sprintf("%s 月以前尚有 %d 筆未繳清出貨單，請先完成沖帳", cutoffMonth, unclearedCount)).Send()
						return
					}
				}
			}
		}
	}

	// 決定前綴: 3=出貨(S), 4=退貨(R)
	if req.ShipmentMode == 0 {
		req.ShipmentMode = 3
	}
	prefix := "S"
	if req.ShipmentMode == 4 {
		prefix = "R"
	}

	yyyymm := ""
	if len(req.ShipmentDate) >= 6 {
		yyyymm = req.ShipmentDate[:6]
	}
	noPrefix := prefix + req.ShipStore + yyyymm

	var maxNo string
	db.GetRead().Unscoped().Model(&models.Shipment{}).
		Where("shipment_no LIKE ?", noPrefix+"%").
		Select("MAX(shipment_no)").
		Scan(&maxNo)

	seq := 1
	if maxNo != "" && len(maxNo) > len(noPrefix) {
		tail := maxNo[len(noPrefix):]
		if n, err := strconv.Atoi(tail); err == nil {
			seq = n + 1
		}
	}
	shipmentNo := fmt.Sprintf("%s%04d", noPrefix, seq)

	if req.DealMode == 0 {
		req.DealMode = 1
	}
	if req.TaxMode == 0 {
		req.TaxMode = 2
	}
	if req.DiscountPercent == 0 {
		req.DiscountPercent = 100
	}

	closeMonth := req.CloseMonth
	if closeMonth == "" && len(req.ShipmentDate) >= 8 {
		// 根據客戶結帳日計算入帳月份
		// 出貨日 > 結帳日 → 入帳月份 = 下個月
		// 出貨日 <= 結帳日 → 入帳月份 = 當月
		y, _ := strconv.Atoi(req.ShipmentDate[:4])
		m, _ := strconv.Atoi(req.ShipmentDate[4:6])
		d, _ := strconv.Atoi(req.ShipmentDate[6:8])
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
	} else if closeMonth == "" && len(req.ShipmentDate) >= 6 {
		closeMonth = req.ShipmentDate[:6]
	}

	adminId, _ := c.Get("AdminId")
	recorderID := int64(0)
	if id, ok := adminId.(float64); ok {
		recorderID = int64(id)
	}

	shipment := models.Shipment{
		ShipmentNo:      shipmentNo,
		ShipmentDate:    req.ShipmentDate,
		CustomerID:      req.CustomerID,
		ShipmentMode:    req.ShipmentMode,
		DealMode:        req.DealMode,
		ShipStore:       req.ShipStore,
		FillPersonID:    req.FillPersonID,
		RecorderID:      recorderID,
		CloseMonth:      closeMonth,
		Remark:          req.Remark,
		TaxMode:         req.TaxMode,
		TaxRate:         req.TaxRate,
		TaxAmount:       req.TaxAmount,
		DiscountPercent: req.DiscountPercent,
		DiscountAmount:  req.DiscountAmount,
		InvoiceDate:     req.InvoiceDate,
		InvoiceNo:       req.InvoiceNo,
		InvoiceAmount:   req.InvoiceAmount,
		ChargeAmount:    req.ChargeAmount,
		ClientGoodID:    req.ClientGoodID,
		InputMode:       req.InputMode,
	}
	if shipment.InputMode == 0 {
		shipment.InputMode = 1
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&shipment).Error; err != nil {
			return err
		}

		// 收集需要更新 delivery status 的 orderIDs
		orderIDSet := map[int64]bool{}

		for _, reqItem := range req.Items {
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := float64(totalQty) * reqItem.ShipPrice

			item := models.ShipmentItem{
				ShipmentID:  shipment.ID,
				ProductID:   reqItem.ProductID,
				SizeGroupID: reqItem.SizeGroupID,
				OrderItemID: reqItem.OrderItemID,
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
				return err
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

			// 追溯訂貨單
			if reqItem.OrderItemID != nil {
				var orderItem models.OrderItem
				if err := tx.Where("id = ?", *reqItem.OrderItemID).First(&orderItem).Error; err == nil {
					orderIDSet[orderItem.OrderID] = true
				} else if err != gorm.ErrRecordNotFound {
					return err
				}
			}
		}

		// 更新關聯訂貨單交貨狀態
		for orderID := range orderIDSet {
			if err := UpdateOrderDeliveryStatus(tx, orderID); err != nil {
				return err
			}
		}

		// 調整庫存：出貨扣、退貨加
		// 找庫點對應的客戶 ID
		var storeCustomer models.RetailCustomer
		if req.ShipStore != "" {
			if err := tx.Where("branch_code = ?", req.ShipStore).First(&storeCustomer).Error; err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
		}
		if storeCustomer.ID > 0 {
			multiplier := -1 // 出貨扣庫存
			if req.ShipmentMode == 4 {
				multiplier = 1 // 退貨加庫存
			}
			var adjustItems []inventory.StockAdjustItem
			for _, reqItem := range req.Items {
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
				return err
			}
		}

		// 計算應收金額（含稅合計），退貨為負數
		var totalShipAmount float64
		for _, reqItem := range req.Items {
			qty := 0
			for _, s := range reqItem.Sizes {
				qty += s.Qty
			}
			totalShipAmount += float64(qty) * reqItem.ShipPrice
		}
		taxAmt := float64(0)
		if req.TaxMode == 2 {
			taxAmt = math.Round(totalShipAmount * req.TaxRate / 100)
		}
		dealAmount := totalShipAmount + taxAmt
		if req.ShipmentMode == 4 {
			dealAmount = -dealAmount // 退貨為負數
		}
		tx.Model(&shipment).Update("deal_amount", dealAmount)

		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("新增成功").SetData(shipment).Send()
}

// UpdateShipment 更新客戶出貨單
func UpdateShipment(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.Shipment
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusNotFound, "出貨單不存在").Send()
		return
	}

	var req struct {
		ShipmentDate    string  `json:"shipment_date"`
		CustomerID      int64   `json:"customer_id"`
		ShipmentMode    int     `json:"shipment_mode"`
		DealMode        int     `json:"deal_mode"`
		ShipStore       string  `json:"ship_store"`
		FillPersonID    *int64  `json:"fill_person_id"`
		CloseMonth      string  `json:"close_month"`
		Remark          string  `json:"remark"`
		TaxMode         int     `json:"tax_mode"`
		TaxRate         float64 `json:"tax_rate"`
		TaxAmount       float64 `json:"tax_amount"`
		DiscountPercent float64 `json:"discount_percent"`
		DiscountAmount  float64 `json:"discount_amount"`
		InvoiceDate     string  `json:"invoice_date"`
		InvoiceNo       string  `json:"invoice_no"`
		InvoiceAmount   float64 `json:"invoice_amount"`
		ChargeAmount    float64 `json:"charge_amount"`
		ClientGoodID    string  `json:"client_good_id"`
		Items           []struct {
			ProductID   int64   `json:"product_id"`
			SizeGroupID *int64  `json:"size_group_id"`
			OrderItemID *int64  `json:"order_item_id"`
			ItemOrder   int     `json:"item_order"`
			SellPrice   float64 `json:"sell_price"`
			Discount    float64 `json:"discount"`
			ShipPrice   float64 `json:"ship_price"`
			NonTaxPrice float64 `json:"non_tax_price"`
			ShipCost    float64 `json:"ship_cost"`
			Supplement  int     `json:"supplement"`
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

	// 收集舊的關聯 orderIDs
	oldOrderIDSet := map[int64]bool{}
	var oldItems []models.ShipmentItem
	db.GetRead().Where("shipment_id = ?", id).Find(&oldItems)
	for _, oi := range oldItems {
		if oi.OrderItemID != nil {
			var orderItem models.OrderItem
			if db.GetRead().Where("id = ?", *oi.OrderItemID).First(&orderItem).Error == nil {
				oldOrderIDSet[orderItem.OrderID] = true
			}
		}
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 還原舊庫存
		var storeCustomerOld models.RetailCustomer
		if existing.ShipStore != "" {
			if err := tx.Where("branch_code = ?", existing.ShipStore).First(&storeCustomerOld).Error; err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
		}
		if storeCustomerOld.ID > 0 {
			oldMul := 1 // 出貨的舊資料要加回
			if existing.ShipmentMode == 4 {
				oldMul = -1 // 退貨的舊資料要扣回
			}
			var oldShipItems []models.ShipmentItem
			if err := tx.Preload("Sizes").Where("shipment_id = ?", id).Find(&oldShipItems).Error; err != nil {
				return err
			}
			var oldAdj []inventory.StockAdjustItem
			for _, oi := range oldShipItems {
				var sizes []inventory.StockAdjustSize
				for _, s := range oi.Sizes {
					if s.Qty > 0 {
						sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
					}
				}
				if len(sizes) > 0 {
					oldAdj = append(oldAdj, inventory.StockAdjustItem{ProductID: oi.ProductID, Sizes: sizes})
				}
			}
			if len(oldAdj) > 0 {
				if err := inventory.AdjustStockBatch(tx, storeCustomerOld.ID, oldAdj, oldMul); err != nil {
					return err
				}
			}
		}

		var oldItemIDs []int64
		if err := tx.Model(&models.ShipmentItem{}).Where("shipment_id = ?", id).Pluck("id", &oldItemIDs).Error; err != nil {
			return err
		}
		if len(oldItemIDs) > 0 {
			if err := tx.Where("shipment_item_id IN ?", oldItemIDs).Delete(&models.ShipmentItemSize{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("shipment_id = ?", id).Delete(&models.ShipmentItem{}).Error; err != nil {
			return err
		}

		adminId, _ := c.Get("AdminId")
		recorderID := existing.RecorderID
		if aid, ok := adminId.(float64); ok {
			recorderID = int64(aid)
		}

		updates := map[string]interface{}{
			"shipment_date":    req.ShipmentDate,
			"customer_id":      req.CustomerID,
			"shipment_mode":    req.ShipmentMode,
			"deal_mode":        req.DealMode,
			"ship_store":       req.ShipStore,
			"fill_person_id":   req.FillPersonID,
			"recorder_id":      recorderID,
			"close_month":      req.CloseMonth,
			"remark":           req.Remark,
			"tax_mode":         req.TaxMode,
			"tax_rate":         req.TaxRate,
			"tax_amount":       req.TaxAmount,
			"discount_percent": req.DiscountPercent,
			"discount_amount":  req.DiscountAmount,
			"invoice_date":     req.InvoiceDate,
			"invoice_no":       req.InvoiceNo,
			"invoice_amount":   req.InvoiceAmount,
			"charge_amount":    req.ChargeAmount,
			"client_good_id":   req.ClientGoodID,
		}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}

		newOrderIDSet := map[int64]bool{}

		for _, reqItem := range req.Items {
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := float64(totalQty) * reqItem.ShipPrice

			item := models.ShipmentItem{
				ShipmentID:  id,
				ProductID:   reqItem.ProductID,
				SizeGroupID: reqItem.SizeGroupID,
				OrderItemID: reqItem.OrderItemID,
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
				return err
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

			if reqItem.OrderItemID != nil {
				var orderItem models.OrderItem
				if tx.Where("id = ?", *reqItem.OrderItemID).First(&orderItem).Error == nil {
					newOrderIDSet[orderItem.OrderID] = true
				}
			}
		}

		// 加入新庫存
		var storeCustomerNew models.RetailCustomer
		if req.ShipStore != "" {
			if err := tx.Where("branch_code = ?", req.ShipStore).First(&storeCustomerNew).Error; err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
		}
		if storeCustomerNew.ID > 0 {
			newMul := -1 // 出貨扣
			if req.ShipmentMode == 4 {
				newMul = 1 // 退貨加
			}
			var newAdj []inventory.StockAdjustItem
			for _, reqItem := range req.Items {
				var sizes []inventory.StockAdjustSize
				for _, s := range reqItem.Sizes {
					if s.Qty > 0 {
						sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
					}
				}
				if len(sizes) > 0 {
					newAdj = append(newAdj, inventory.StockAdjustItem{ProductID: reqItem.ProductID, Sizes: sizes})
				}
			}
			if err := inventory.AdjustStockBatch(tx, storeCustomerNew.ID, newAdj, newMul); err != nil {
				return err
			}
		}

		// 更新所有受影響的訂貨單交貨狀態
		allOrderIDs := map[int64]bool{}
		for k := range oldOrderIDSet {
			allOrderIDs[k] = true
		}
		for k := range newOrderIDSet {
			allOrderIDs[k] = true
		}
		for orderID := range allOrderIDs {
			if err := UpdateOrderDeliveryStatus(tx, orderID); err != nil {
				return err
			}
		}

		// 重算應收金額，退貨為負數
		var totalShipAmount float64
		for _, reqItem := range req.Items {
			qty := 0
			for _, s := range reqItem.Sizes {
				qty += s.Qty
			}
			totalShipAmount += float64(qty) * reqItem.ShipPrice
		}
		taxAmt := float64(0)
		if req.TaxMode == 2 {
			taxAmt = math.Round(totalShipAmount * req.TaxRate / 100)
		}
		dealAmount := totalShipAmount + taxAmt
		if req.ShipmentMode == 4 {
			dealAmount = -dealAmount
		}
		tx.Model(&existing).Update("deal_amount", dealAmount)

		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("更新成功").Send()
}

// DeleteShipment 軟刪除客戶出貨單
func DeleteShipment(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var shipment models.Shipment
	if err := db.GetRead().Where("id = ?", id).First(&shipment).Error; err != nil {
		resp.Fail(http.StatusNotFound, "出貨單不存在").Send()
		return
	}

	// 收集關聯 orderIDs
	orderIDSet := map[int64]bool{}
	var items []models.ShipmentItem
	db.GetRead().Where("shipment_id = ?", id).Find(&items)
	for _, it := range items {
		if it.OrderItemID != nil {
			var orderItem models.OrderItem
			if db.GetRead().Where("id = ?", *it.OrderItemID).First(&orderItem).Error == nil {
				orderIDSet[orderItem.OrderID] = true
			}
		}
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 還原庫存：出貨要加回、退貨要扣回
		var storeCustomer models.RetailCustomer
		if shipment.ShipStore != "" {
			if err := tx.Where("branch_code = ?", shipment.ShipStore).First(&storeCustomer).Error; err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
		}
		if storeCustomer.ID > 0 {
			multiplier := 1 // 出貨刪除 → 加回庫存
			if shipment.ShipmentMode == 4 {
				multiplier = -1 // 退貨刪除 → 扣回庫存
			}
			var shipItems []models.ShipmentItem
			if err := tx.Preload("Sizes").Where("shipment_id = ?", id).Find(&shipItems).Error; err != nil {
				return err
			}
			var adjItems []inventory.StockAdjustItem
			for _, si := range shipItems {
				var sizes []inventory.StockAdjustSize
				for _, s := range si.Sizes {
					if s.Qty > 0 {
						sizes = append(sizes, inventory.StockAdjustSize{SizeOptionID: s.SizeOptionID, Qty: s.Qty})
					}
				}
				if len(sizes) > 0 {
					adjItems = append(adjItems, inventory.StockAdjustItem{ProductID: si.ProductID, Sizes: sizes})
				}
			}
			if len(adjItems) > 0 {
				if err := inventory.AdjustStockBatch(tx, storeCustomer.ID, adjItems, multiplier); err != nil {
					return err
				}
			}
		}

		if err := tx.Delete(&models.Shipment{}, id).Error; err != nil {
			return err
		}
		for orderID := range orderIDSet {
			if err := UpdateOrderDeliveryStatus(tx, orderID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("刪除成功").Send()
}

// GetCustomerCredit 查詢客戶剩餘額度
func GetCustomerCredit(c *gin.Context) {
	resp := response.New(c)
	customerID := c.Param("customer_id")

	db := models.PostgresNew()
	defer db.Close()

	var customer models.RetailCustomer
	if err := db.GetRead().Where("id = ?", customerID).First(&customer).Error; err != nil {
		resp.Fail(404, "客戶不存在").Send()
		return
	}

	// 未沖銷餘額 = 應收(deal_amount) - 已收(charge_amount)
	type balRow struct {
		TotalDeal   float64
		TotalCharge float64
	}
	var bal balRow
	db.GetRead().Model(&models.Shipment{}).
		Select("COALESCE(SUM(deal_amount), 0) as total_deal, COALESCE(SUM(charge_amount), 0) as total_charge").
		Where("customer_id = ? AND deleted_at IS NULL",customerID).
		Scan(&bal)

	outstanding := bal.TotalDeal - bal.TotalCharge

	resp.Success("成功").SetData(map[string]interface{}{
		"outstanding":  outstanding,
		"credit_limit": customer.CreditLimit,
	}).Send()
}

// BarcodeParse 條碼匯入解析：解析條碼、比對訂單未交量
func BarcodeParse(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		CustomerID int64 `json:"customer_id" binding:"required"`
		Entries    []struct {
			Barcode string `json:"barcode"`
			Qty     int    `json:"qty"`
		} `json:"entries" binding:"required"`
		SearchAll bool `json:"search_all"` // true=忽略訂貨單，直接依商品匹配
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	// 1. 查所有 SizeGroup + Options（依 sort_order 排序）
	var sizeGroups []models.SizeGroup
	db.GetRead().Preload("Options", func(d *gorm.DB) *gorm.DB {
		return d.Order("sort_order ASC")
	}).Find(&sizeGroups)

	// 建立 SizeGroup.Code → SizeGroup 對照（code 從長到短排序，方便匹配）
	type sgInfo struct {
		ID      int64
		Code    string
		Options []models.SizeOption
	}
	var sgList []sgInfo
	for _, sg := range sizeGroups {
		sgList = append(sgList, sgInfo{ID: sg.ID, Code: sg.Code, Options: sg.Options})
	}
	sort.Slice(sgList, func(i, j int) bool {
		return len(sgList[i].Code) > len(sgList[j].Code)
	})

	// 2. 解析每筆條碼 → model_code + sizeGroupCode + position
	type parsedEntry struct {
		Barcode       string
		Qty           int
		ModelCode     string
		SizeGroupCode string
		Position      int // 1-based
		SizeGroupID   int64
		SizeOptionID  int64
		SizeLabel     string
	}

	type errorEntry struct {
		Barcode string `json:"barcode"`
		Reason  string `json:"reason"`
	}

	var parsed []parsedEntry
	var errors []errorEntry

	for _, entry := range req.Entries {
		bc := strings.TrimSpace(entry.Barcode)
		if bc == "" {
			continue
		}
		qty := entry.Qty
		if qty <= 0 {
			qty = 1
		}

		// 嘗試從尾部匹配: {SizeGroupCode}{Position:02d}
		matched := false
		for _, sg := range sgList {
			code := sg.Code
			// 條碼尾部至少要有 code + 2 位數字，且前面還有 model_code
			suffixLen := len(code) + 2
			if len(bc) <= suffixLen {
				continue
			}
			tail := bc[len(bc)-suffixLen:]
			if !strings.HasPrefix(tail, code) {
				continue
			}
			posStr := tail[len(code):]
			pos, err := strconv.Atoi(posStr)
			if err != nil || pos < 1 {
				continue
			}
			// 確認 position 在範圍內
			if pos > len(sg.Options) {
				errors = append(errors, errorEntry{
					Barcode: bc,
					Reason:  fmt.Sprintf("尺碼位置超出範圍: 第%d格（共%d格）", pos, len(sg.Options)),
				})
				matched = true
				break
			}
			opt := sg.Options[pos-1] // 0-based index
			modelCode := bc[:len(bc)-suffixLen]

			parsed = append(parsed, parsedEntry{
				Barcode:       bc,
				Qty:           qty,
				ModelCode:     modelCode,
				SizeGroupCode: code,
				Position:      pos,
				SizeGroupID:   sg.ID,
				SizeOptionID:  opt.ID,
				SizeLabel:     opt.Label,
			})
			matched = true
			break
		}
		if !matched {
			errors = append(errors, errorEntry{
				Barcode: bc,
				Reason:  "無法解析條碼格式",
			})
		}
	}

	// 3. 收集所有 model_code，批次查 Product
	modelCodes := map[string]bool{}
	for _, p := range parsed {
		modelCodes[p.ModelCode] = true
	}
	var mcList []string
	for mc := range modelCodes {
		mcList = append(mcList, mc)
	}

	productMap := map[string]*models.Product{} // model_code → Product
	if len(mcList) > 0 {
		var products []models.Product
		db.GetRead().Where("model_code IN ?", mcList).Find(&products)
		for i := range products {
			productMap[products[i].ModelCode] = &products[i]
		}
	}

	// 4. 篩掉查無商品的，收集有效的 product_id
	var validParsed []parsedEntry
	for _, p := range parsed {
		prod, ok := productMap[p.ModelCode]
		if !ok {
			errors = append(errors, errorEntry{
				Barcode: p.Barcode,
				Reason:  fmt.Sprintf("查無此商品: %s", p.ModelCode),
			})
			continue
		}
		_ = prod
		validParsed = append(validParsed, p)
	}

	// resultItem 與組裝結果定義（搜尋全部商品模式也要用）
	type resultItem struct {
		Barcode        string  `json:"barcode"`
		ModelCode      string  `json:"model_code"`
		ProductID      int64   `json:"product_id"`
		ProductName    string  `json:"product_name"`
		SizeGroupID    int64   `json:"size_group_id"`
		SizeGroupCode  string  `json:"size_group_code"`
		SizeOptionID   int64   `json:"size_option_id"`
		SizeLabel      string  `json:"size_label"`
		Qty            int     `json:"qty"`
		OrderItemID    int64   `json:"order_item_id"`
		OrderNo        string  `json:"order_no"`
		OutstandingQty int     `json:"outstanding_qty"`
		SellPrice      float64 `json:"sell_price"`
		Discount       float64 `json:"discount"`
		ShipPrice      float64 `json:"ship_price"`
		NonTaxPrice    float64 `json:"non_tax_price"`
		Supplement     int     `json:"supplement"`
		Status         string  `json:"status"` // "ok" or "warning"
	}

	// 搜尋全部商品模式：忽略訂貨單，直接依商品資料輸出
	if req.SearchAll {
		type mergeKey struct {
			ProductID    int64
			SizeOptionID int64
		}
		mergeMap := map[mergeKey]*resultItem{}
		var order []mergeKey
		for _, p := range validParsed {
			prod := productMap[p.ModelCode]
			k := mergeKey{ProductID: prod.ID, SizeOptionID: p.SizeOptionID}
			if r, ok := mergeMap[k]; ok {
				r.Qty += p.Qty
				continue
			}
			mergeMap[k] = &resultItem{
				Barcode:       p.Barcode,
				ModelCode:     p.ModelCode,
				ProductID:     prod.ID,
				ProductName:   prod.NameSpec,
				SizeGroupID:   p.SizeGroupID,
				SizeGroupCode: p.SizeGroupCode,
				SizeOptionID:  p.SizeOptionID,
				SizeLabel:     p.SizeLabel,
				Qty:           p.Qty,
				SellPrice:     prod.MSRP,
				ShipPrice:     prod.Wholesale,
				NonTaxPrice:   prod.Wholesale,
				Status:        "ok",
			}
			order = append(order, k)
		}
		var resultItems []resultItem
		for _, k := range order {
			resultItems = append(resultItems, *mergeMap[k])
		}
		resp.Success("成功").SetData(map[string]interface{}{
			"items":  resultItems,
			"errors": errors,
		}).Send()
		return
	}

	// 5. 查該客戶未交訂單的 OrderItem（含 Sizes）
	var productIDs []int64
	for _, mc := range mcList {
		if prod, ok := productMap[mc]; ok {
			productIDs = append(productIDs, prod.ID)
		}
	}

	var orderItems []models.OrderItem
	if len(productIDs) > 0 {
		db.GetRead().
			Where("order_items.cancel_flag < 2 AND order_items.product_id IN ?", productIDs).
			Joins("JOIN orders ON orders.id = order_items.order_id AND orders.deleted_at IS NULL AND orders.delivery_status < 2 AND orders.customer_id = ?", req.CustomerID).
			Preload("Sizes").
			Order("order_items.id ASC"). // FIFO: 舊訂單優先
			Find(&orderItems)
	}

	// 查訂單號
	orderIDSet := map[int64]bool{}
	for _, oi := range orderItems {
		orderIDSet[oi.OrderID] = true
	}
	orderNoMap := map[int64]string{}
	if len(orderIDSet) > 0 {
		var oids []int64
		for oid := range orderIDSet {
			oids = append(oids, oid)
		}
		type oref struct {
			ID      int64
			OrderNo string
		}
		var refs []oref
		db.GetRead().Model(&models.Order{}).Select("id, order_no").Where("id IN ?", oids).Scan(&refs)
		for _, r := range refs {
			orderNoMap[r.ID] = r.OrderNo
		}
	}

	// 查已出貨量
	var allItemIDs []int64
	for _, oi := range orderItems {
		allItemIDs = append(allItemIDs, oi.ID)
	}
	shipped := ShippedQtyMap(db.GetRead(), allItemIDs)

	// 6. 建立 (product_id, size_option_id) → []候選 OrderItem 的對照
	type candidate struct {
		OrderItemID  int64
		OrderID      int64
		OrderNo      string
		SizeGroupID  int64
		SizeOptionID int64
		Outstanding  int
		AdvicePrice  float64
		Discount     float64
		OrderPrice   float64
		NonTaxPrice  float64
		Supplement   int
	}
	candidateMap := map[string][]candidate{} // key: "productID-sizeOptionID"

	for _, oi := range orderItems {
		sgID := int64(0)
		if oi.SizeGroupID != nil {
			sgID = *oi.SizeGroupID
		}
		for _, sz := range oi.Sizes {
			key := fmt.Sprintf("%d-%d", oi.ProductID, sz.SizeOptionID)
			shippedKey := fmt.Sprintf("%d-%d", oi.ID, sz.SizeOptionID)
			outstanding := sz.Qty - shipped[shippedKey]
			if outstanding <= 0 {
				continue
			}
			candidateMap[key] = append(candidateMap[key], candidate{
				OrderItemID:  oi.ID,
				OrderID:      oi.OrderID,
				OrderNo:      orderNoMap[oi.OrderID],
				SizeGroupID:  sgID,
				SizeOptionID: sz.SizeOptionID,
				Outstanding:  outstanding,
				AdvicePrice:  oi.AdvicePrice,
				Discount:     oi.Discount,
				OrderPrice:   oi.OrderPrice,
				NonTaxPrice:  oi.NonTaxPrice,
				Supplement:   oi.Supplement,
			})
		}
	}

	// 7. 分配每筆掃描結果到訂單（FIFO），追蹤已分配量
	// 追蹤每個 candidate 已分配的量
	allocated := map[string]int{} // key: "orderItemID-sizeOptionID" → 已分配

	var resultItems []resultItem

	for _, p := range validParsed {
		prod := productMap[p.ModelCode]
		candKey := fmt.Sprintf("%d-%d", prod.ID, p.SizeOptionID)
		candidates, hasCand := candidateMap[candKey]

		if !hasCand || len(candidates) == 0 {
			errors = append(errors, errorEntry{
				Barcode: p.Barcode,
				Reason:  fmt.Sprintf("此客戶無此商品+尺碼的未交訂單 (%s 尺碼%s)", p.ModelCode, p.SizeLabel),
			})
			continue
		}

		remaining := p.Qty
		for _, cand := range candidates {
			if remaining <= 0 {
				break
			}
			allocKey := fmt.Sprintf("%d-%d", cand.OrderItemID, cand.SizeOptionID)
			used := allocated[allocKey]
			avail := cand.Outstanding - used
			if avail <= 0 {
				continue
			}

			take := remaining
			status := "ok"
			if take > avail {
				take = avail
				// 還有剩餘，繼續分配到下一個訂單
			}

			allocated[allocKey] += take
			remaining -= take

			resultItems = append(resultItems, resultItem{
				Barcode:        p.Barcode,
				ModelCode:      p.ModelCode,
				ProductID:      prod.ID,
				ProductName:    prod.NameSpec,
				SizeGroupID:    cand.SizeGroupID,
				SizeGroupCode:  p.SizeGroupCode,
				SizeOptionID:   cand.SizeOptionID,
				SizeLabel:      p.SizeLabel,
				Qty:            take,
				OrderItemID:    cand.OrderItemID,
				OrderNo:        cand.OrderNo,
				OutstandingQty: cand.Outstanding,
				SellPrice:      cand.AdvicePrice,
				Discount:       cand.Discount,
				ShipPrice:      cand.OrderPrice,
				NonTaxPrice:    cand.NonTaxPrice,
				Supplement:     cand.Supplement,
				Status:         status,
			})
		}

		// 如果還有剩餘，全部分到最後一個訂單但標記 warning
		if remaining > 0 {
			lastCand := candidates[len(candidates)-1]
			resultItems = append(resultItems, resultItem{
				Barcode:        p.Barcode,
				ModelCode:      p.ModelCode,
				ProductID:      prod.ID,
				ProductName:    prod.NameSpec,
				SizeGroupID:    lastCand.SizeGroupID,
				SizeGroupCode:  p.SizeGroupCode,
				SizeOptionID:   lastCand.SizeOptionID,
				SizeLabel:      p.SizeLabel,
				Qty:            remaining,
				OrderItemID:    lastCand.OrderItemID,
				OrderNo:        lastCand.OrderNo,
				OutstandingQty: lastCand.Outstanding,
				SellPrice:      lastCand.AdvicePrice,
				Discount:       lastCand.Discount,
				ShipPrice:      lastCand.OrderPrice,
				NonTaxPrice:    lastCand.NonTaxPrice,
				Supplement:     lastCand.Supplement,
				Status:         "warning",
			})
		}
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"items":  resultItems,
		"errors": errors,
	}).Send()
}
