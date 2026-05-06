package controllers

import (
	"fmt"
	"math"
	"net/http"
	"project/models"
	response "project/services/responses"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetOrders 客戶訂貨單列表
func GetOrders(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var items []models.Order
	query := db.GetRead().
		Select("orders.*").
		Joins("JOIN retail_customers ON retail_customers.id = orders.customer_id AND retail_customers.is_visible = true").
		Preload("Customer").
		Preload("Brand").
		Order("orders.order_date DESC, orders.id DESC")

	query = ApplySearch(query, c.Query("search"), "order_no")

	if v := c.Query("customer_id"); v != "" {
		query = query.Where("orders.customer_id = ?", v)
	}
	if v := c.Query("date_from"); v != "" {
		query = query.Where("orders.order_date >= ?", v)
	}
	if v := c.Query("date_to"); v != "" {
		query = query.Where("orders.order_date <= ?", v)
	}
	if v := c.Query("delivery_status"); v != "" {
		query = query.Where("delivery_status = ?", v)
	}

	paged, total := Paginate(c, query, &models.Order{})
	paged.Find(&items)
	resp.Success("成功").SetData(items).SetTotal(total).Send()
}

// GetOrder 客戶訂貨單詳情
func GetOrder(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var item models.Order
	err = db.GetRead().
		Joins("JOIN retail_customers ON retail_customers.id = orders.customer_id AND retail_customers.is_visible = true").
		Preload("Customer").
		Preload("Brand").
		Preload("FillPerson").
		Preload("Recorder").
		Preload("Items", func(db *gorm.DB) *gorm.DB {
			return db.Order("item_order ASC")
		}).
		Preload("Items.Product").
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
		Where("orders.id = ?", id).
		First(&item).Error
	if err != nil {
		resp.Fail(http.StatusNotFound, "訂貨單不存在").Send()
		return
	}

	// 補載入庫存（依庫點篩選）
	for i, it := range item.Items {
		if it.Product != nil {
			var stocks []models.ProductSizeStock
			if item.OrderStore != "" {
				db.GetRead().Where("product_id = ? AND customer_id IN (SELECT id FROM retail_customers WHERE branch_code = ?)", it.ProductID, item.OrderStore).Find(&stocks)
			} else {
				db.GetRead().Where("product_id = ? AND customer_id = ?", it.ProductID, item.CustomerID).Find(&stocks)
			}
			item.Items[i].Product.SizeStocks = stocks
		}
	}

	// 查已出貨數量
	var allItemIDs []int64
	for _, it := range item.Items {
		allItemIDs = append(allItemIDs, it.ID)
	}
	shipped := ShippedQtyMap(db.GetRead(), allItemIDs)

	// 查關聯出貨單號（每個 order_item 對應的 shipment_no 列表）
	type shipInfo struct {
		OrderItemID int64
		ShipmentNo  string
	}
	var shipInfos []shipInfo
	db.GetRead().Model(&models.ShipmentItem{}).
		Select("shipment_items.order_item_id, shipments.shipment_no").
		Joins("JOIN shipments ON shipments.id = shipment_items.shipment_id AND shipments.deleted_at IS NULL").
		Where("shipment_items.order_item_id IN ?", allItemIDs).
		Scan(&shipInfos)

	// 組成 map: orderItemId -> []shipmentNo
	shipmentNos := map[int64][]string{}
	for _, si := range shipInfos {
		shipmentNos[si.OrderItemID] = append(shipmentNos[si.OrderItemID], si.ShipmentNo)
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"order":        item,
		"shipped":      shipped,
		"shipment_nos": shipmentNos,
	}).Send()
}

// CreateOrder 新增客戶訂貨單
func CreateOrder(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	var req struct {
		OrderDate     string  `json:"order_date" binding:"required"`
		CustomerID    int64   `json:"customer_id" binding:"required"`
		FillPersonID  *int64  `json:"fill_person_id"`
		SalesmanID    *int64  `json:"salesman_id"`
		DealMode      int     `json:"deal_mode"`
		ClientOrderID string  `json:"client_order_id"`
		BrandID       *int64  `json:"brand_id"`
		OrderStore    string  `json:"order_store"`
		Remark        string  `json:"remark"`
		TaxMode       int     `json:"tax_mode"`
		TaxRate       float64 `json:"tax_rate"`
		Items         []struct {
			ProductID    int64   `json:"product_id"`
			SizeGroupID  *int64  `json:"size_group_id"`
			ItemOrder    int     `json:"item_order"`
			AdvicePrice  float64 `json:"advice_price"`
			Discount     float64 `json:"discount"`
			OrderPrice   float64 `json:"order_price"`
			NonTaxPrice  float64 `json:"non_tax_price"`
			Supplement   int     `json:"supplement"`
			ExpectedDate string  `json:"expected_date"`
			ClientGoodID string  `json:"client_good_id"`
			CancelFlag   int     `json:"cancel_flag"`
			Sizes        []struct {
				SizeOptionID int64 `json:"size_option_id"`
				Qty          int   `json:"qty"`
			} `json:"sizes"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	if _, verr := EnsureCustomerVisible(db.GetRead(), req.CustomerID); verr != nil {
		resp.Fail(http.StatusBadRequest, ErrMsgCustomerNotVisible).Send()
		return
	}

	// 產生訂貨單號: O + 訂貨庫點BranchCode + YYMM + 流水號3碼
	yymm := ""
	if len(req.OrderDate) >= 6 {
		yymm = req.OrderDate[2:6]
	}
	prefix := "O" + req.OrderStore + yymm

	var maxNo string
	db.GetRead().Unscoped().Model(&models.Order{}).
		Where("order_no LIKE ?", prefix+"%").
		Select("MAX(order_no)").
		Scan(&maxNo)

	seq := 1
	if maxNo != "" && len(maxNo) > len(prefix) {
		tail := maxNo[len(prefix):]
		if n, err := strconv.Atoi(tail); err == nil {
			seq = n + 1
		}
	}
	orderNo := fmt.Sprintf("%s%03d", prefix, seq)

	if req.DealMode == 0 {
		req.DealMode = 1
	}
	if req.TaxMode == 0 {
		req.TaxMode = 2
	}

	// 系統紀錄者
	adminId, _ := c.Get("AdminId")
	recorderID := int64(0)
	if id, ok := adminId.(float64); ok {
		recorderID = int64(id)
	}

	order := models.Order{
		OrderNo:       orderNo,
		OrderDate:     req.OrderDate,
		CustomerID:    req.CustomerID,
		FillPersonID:  req.FillPersonID,
		SalesmanID:    req.SalesmanID,
		RecorderID:    recorderID,
		DealMode:      req.DealMode,
		ClientOrderID: req.ClientOrderID,
		BrandID:       req.BrandID,
		OrderStore:    req.OrderStore,
		Remark:        req.Remark,
		TaxMode:       req.TaxMode,
		TaxRate:       req.TaxRate,
	}

	err := db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		for _, reqItem := range req.Items {
			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := math.Round(float64(totalQty) * reqItem.OrderPrice)

			item := models.OrderItem{
				OrderID:      order.ID,
				ProductID:    reqItem.ProductID,
				SizeGroupID:  reqItem.SizeGroupID,
				ItemOrder:    reqItem.ItemOrder,
				AdvicePrice:  reqItem.AdvicePrice,
				Discount:     reqItem.Discount,
				OrderPrice:   reqItem.OrderPrice,
				NonTaxPrice:  reqItem.NonTaxPrice,
				TotalQty:     totalQty,
				TotalAmount:  totalAmount,
				Supplement:   reqItem.Supplement,
				ExpectedDate: reqItem.ExpectedDate,
				ClientGoodID: reqItem.ClientGoodID,
				CancelFlag:   reqItem.CancelFlag,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			for _, s := range reqItem.Sizes {
				size := models.OrderItemSize{
					OrderItemID:  item.ID,
					SizeOptionID: s.SizeOptionID,
					Qty:          s.Qty,
				}
				if err := tx.Create(&size).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("新增成功").SetData(order).Send()
}

// UpdateOrder 更新客戶訂貨單
func UpdateOrder(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.Order
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusNotFound, "訂貨單不存在").Send()
		return
	}
	var req struct {
		OrderDate     string  `json:"order_date"`
		CustomerID    int64   `json:"customer_id"`
		FillPersonID  *int64  `json:"fill_person_id"`
		SalesmanID    *int64  `json:"salesman_id"`
		DealMode      int     `json:"deal_mode"`
		ClientOrderID string  `json:"client_order_id"`
		BrandID       *int64  `json:"brand_id"`
		OrderStore    string  `json:"order_store"`
		Remark        string  `json:"remark"`
		TaxMode       int     `json:"tax_mode"`
		TaxRate       float64 `json:"tax_rate"`
		Items         []struct {
			ProductID    int64   `json:"product_id"`
			SizeGroupID  *int64  `json:"size_group_id"`
			ItemOrder    int     `json:"item_order"`
			AdvicePrice  float64 `json:"advice_price"`
			Discount     float64 `json:"discount"`
			OrderPrice   float64 `json:"order_price"`
			NonTaxPrice  float64 `json:"non_tax_price"`
			Supplement   int     `json:"supplement"`
			ExpectedDate string  `json:"expected_date"`
			ClientGoodID string  `json:"client_good_id"`
			CancelFlag   int     `json:"cancel_flag"`
			Sizes        []struct {
				SizeOptionID int64 `json:"size_option_id"`
				Qty          int   `json:"qty"`
			} `json:"sizes"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "資料格式錯誤").Send()
		return
	}

	if req.CustomerID != 0 {
		if _, verr := EnsureCustomerVisible(db.GetRead(), req.CustomerID); verr != nil {
			resp.Fail(http.StatusBadRequest, ErrMsgCustomerNotVisible).Send()
			return
		}
	}

	// 查出已停貨的舊明細（cancel_flag >= 2），保護不被修改
	var stoppedItems []models.OrderItem
	db.GetRead().Where("order_id = ? AND cancel_flag >= 2", id).
		Preload("Sizes").
		Find(&stoppedItems)

	// 建立已停貨 product_id set，用於過濾前端傳入的資料
	stoppedProductIDs := map[int64]bool{}
	for _, si := range stoppedItems {
		stoppedProductIDs[si.ProductID] = true
	}

	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		// 只刪除未停貨的明細
		var activeItems []models.OrderItem
		if err := tx.Where("order_id = ? AND cancel_flag < 2", id).Find(&activeItems).Error; err != nil {
			return err
		}
		var activeItemIDs []int64
		// 記錄舊 orderItemID → productID 的對照，用於重新關聯出貨明細
		oldItemProductMap := map[int64]int64{} // orderItemID → productID
		for _, ai := range activeItems {
			activeItemIDs = append(activeItemIDs, ai.ID)
			oldItemProductMap[ai.ID] = ai.ProductID
		}

		// 收集被出貨明細引用的 orderItemID → shipmentItemIDs
		shipmentItemLinks := map[int64][]int64{} // productID → []shipmentItemID
		if len(activeItemIDs) > 0 {
			var linkedShipItems []models.ShipmentItem
			tx.Where("order_item_id IN ?", activeItemIDs).Find(&linkedShipItems)
			for _, si := range linkedShipItems {
				if si.OrderItemID != nil {
					pid := oldItemProductMap[*si.OrderItemID]
					shipmentItemLinks[pid] = append(shipmentItemLinks[pid], si.ID)
				}
			}
			// 先解除 FK 關聯，避免刪除 OrderItem 時 FK violation
			if err := tx.Model(&models.ShipmentItem{}).Where("order_item_id IN ?", activeItemIDs).Update("order_item_id", nil).Error; err != nil {
				return err
			}
			if err := tx.Where("order_item_id IN ?", activeItemIDs).Delete(&models.OrderItemSize{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("order_id = ? AND cancel_flag < 2", id).Delete(&models.OrderItem{}).Error; err != nil {
			return err
		}

		adminId, _ := c.Get("AdminId")
		recorderID := existing.RecorderID
		if aid, ok := adminId.(float64); ok {
			recorderID = int64(aid)
		}

		updates := map[string]interface{}{
			"order_date":      req.OrderDate,
			"fill_person_id":  req.FillPersonID,
			"salesman_id":     req.SalesmanID,
			"recorder_id":     recorderID,
			"deal_mode":       req.DealMode,
			"client_order_id": req.ClientOrderID,
			"brand_id":        req.BrandID,
			"remark":          req.Remark,
			"tax_mode":        req.TaxMode,
			"tax_rate":        req.TaxRate,
		}
		if err := tx.Model(&existing).Updates(updates).Error; err != nil {
			return err
		}

		for _, reqItem := range req.Items {
			// 跳過已停貨的明細（由 DB 保留原始資料）
			if stoppedProductIDs[reqItem.ProductID] {
				continue
			}

			totalQty := 0
			for _, s := range reqItem.Sizes {
				totalQty += s.Qty
			}
			totalAmount := math.Round(float64(totalQty) * reqItem.OrderPrice)

			item := models.OrderItem{
				OrderID:      id,
				ProductID:    reqItem.ProductID,
				SizeGroupID:  reqItem.SizeGroupID,
				ItemOrder:    reqItem.ItemOrder,
				AdvicePrice:  reqItem.AdvicePrice,
				Discount:     reqItem.Discount,
				OrderPrice:   reqItem.OrderPrice,
				NonTaxPrice:  reqItem.NonTaxPrice,
				TotalQty:     totalQty,
				TotalAmount:  totalAmount,
				Supplement:   reqItem.Supplement,
				ExpectedDate: reqItem.ExpectedDate,
				ClientGoodID: reqItem.ClientGoodID,
				CancelFlag:   reqItem.CancelFlag,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			for _, s := range reqItem.Sizes {
				size := models.OrderItemSize{
					OrderItemID:  item.ID,
					SizeOptionID: s.SizeOptionID,
					Qty:          s.Qty,
				}
				if err := tx.Create(&size).Error; err != nil {
					return err
				}
			}

			// 重新關聯出貨明細到新的 orderItem
			if siIDs, ok := shipmentItemLinks[reqItem.ProductID]; ok && len(siIDs) > 0 {
				if err := tx.Model(&models.ShipmentItem{}).Where("id IN ?", siIDs).Update("order_item_id", item.ID).Error; err != nil {
					return err
				}
			}
		}
		// 更新交貨狀態（考慮停貨明細）
		return UpdateOrderDeliveryStatus(tx, id)
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("更新成功").Send()
}

// DeleteOrder 軟刪除客戶訂貨單
func DeleteOrder(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	if err := db.GetWrite().Delete(&models.Order{}, id).Error; err != nil {
		resp.Panic(err).Send()
		return
	}
	resp.Success("刪除成功").Send()
}

// SearchOrders 搜尋訂貨單（供出貨單選擇關聯訂貨）
func SearchOrders(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	query := db.GetRead().
		Joins("JOIN retail_customers ON retail_customers.id = orders.customer_id AND retail_customers.is_visible = true").
		Where("orders.delivery_status < 2").
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
		Preload("Items.Product.ProductVendors").
		Preload("Items.SizeGroup.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Items.Sizes.SizeOption").
		Order("orders.order_date DESC, orders.order_no DESC")

	if v := c.Query("customer_id"); v != "" {
		query = query.Where("orders.customer_id = ?", v)
	}
	if v := c.Query("search"); v != "" {
		query = ApplySearch(query, v, "orders.order_no")
	}

	var orders []models.Order
	query.Limit(20).Find(&orders)

	var allItemIDs []int64
	for _, o := range orders {
		for _, item := range o.Items {
			allItemIDs = append(allItemIDs, item.ID)
		}
	}

	shipped := ShippedQtyMap(db.GetRead(), allItemIDs)

	resp.Success("成功").SetData(map[string]interface{}{
		"orders":  orders,
		"shipped": shipped,
	}).Send()
}

// ShippedQtyMap 查詢指定 orderItemIDs 的已出貨數量
func ShippedQtyMap(db *gorm.DB, orderItemIDs []int64) map[string]int {
	result := map[string]int{}
	if len(orderItemIDs) == 0 {
		return result
	}

	type row struct {
		OrderItemID  int64
		SizeOptionID int64
		TotalQty     int
	}
	var rows []row
	db.Model(&models.ShipmentItemSize{}).
		Select("shipment_items.order_item_id, shipment_item_sizes.size_option_id, SUM(shipment_item_sizes.qty) as total_qty").
		Joins("JOIN shipment_items ON shipment_items.id = shipment_item_sizes.shipment_item_id").
		Joins("JOIN shipments ON shipments.id = shipment_items.shipment_id AND shipments.deleted_at IS NULL").
		Where("shipment_items.order_item_id IN ?", orderItemIDs).
		Group("shipment_items.order_item_id, shipment_item_sizes.size_option_id").
		Scan(&rows)

	for _, r := range rows {
		key := fmt.Sprintf("%d-%d", r.OrderItemID, r.SizeOptionID)
		result[key] = r.TotalQty
	}
	return result
}

// UpdateOrderDeliveryStatus 比對訂貨單的訂貨量 vs 已出貨量，更新 Order.DeliveryStatus
func UpdateOrderDeliveryStatus(tx *gorm.DB, orderID int64) error {
	type sizeQty struct {
		OrderItemID  int64
		SizeOptionID int64
		Qty          int
	}
	// 檢查是否所有明細都已停貨（cancel_flag >= 2），若是則視為已交齊
	var totalItemCount int64
	tx.Model(&models.OrderItem{}).Where("order_id = ?", orderID).Count(&totalItemCount)
	var stoppedItemCount int64
	tx.Model(&models.OrderItem{}).Where("order_id = ? AND cancel_flag >= 2", orderID).Count(&stoppedItemCount)
	if totalItemCount > 0 && stoppedItemCount == totalItemCount {
		return tx.Model(&models.Order{}).Where("id = ?", orderID).Update("delivery_status", 2).Error
	}

	// 只計算未停貨的明細
	var orderSizes []sizeQty
	tx.Model(&models.OrderItemSize{}).
		Select("order_item_sizes.order_item_id, order_item_sizes.size_option_id, order_item_sizes.qty").
		Joins("JOIN order_items ON order_items.id = order_item_sizes.order_item_id").
		Where("order_items.order_id = ? AND order_items.cancel_flag < 2", orderID).
		Scan(&orderSizes)

	if len(orderSizes) == 0 {
		return tx.Model(&models.Order{}).Where("id = ?", orderID).Update("delivery_status", 0).Error
	}

	idSet := map[int64]bool{}
	for _, os := range orderSizes {
		idSet[os.OrderItemID] = true
	}
	var itemIDs []int64
	for id := range idSet {
		itemIDs = append(itemIDs, id)
	}

	shippedMap := ShippedQtyMap(tx, itemIDs)

	allDelivered := true
	anyDelivered := false
	for _, os := range orderSizes {
		if os.Qty <= 0 {
			continue
		}
		key := fmt.Sprintf("%d-%d", os.OrderItemID, os.SizeOptionID)
		shipped := shippedMap[key]
		if shipped > 0 {
			anyDelivered = true
		}
		if shipped < os.Qty {
			allDelivered = false
		}
	}

	status := 0
	if allDelivered && anyDelivered {
		status = 2
	} else if anyDelivered {
		status = 1
	}

	return tx.Model(&models.Order{}).Where("id = ?", orderID).Update("delivery_status", status).Error
}

// StopOrder 停貨：將所有明細 cancel_flag 設為 2(停)
func StopOrder(c *gin.Context) {
	resp := response.New(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		resp.Fail(http.StatusBadRequest, "無效的 ID").Send()
		return
	}

	db := models.PostgresNew()
	defer db.Close()

	var existing models.Order
	if err := db.GetRead().Where("id = ?", id).First(&existing).Error; err != nil {
		resp.Fail(http.StatusNotFound, "訂貨單不存在").Send()
		return
	}

	// 所有明細的 cancel_flag 設為 2 (停)，並更新交貨狀態
	err = db.GetWrite().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.OrderItem{}).Where("order_id = ? AND cancel_flag < 2", id).Update("cancel_flag", 2).Error; err != nil {
			return err
		}
		// 舊資料可能 cancel_flag=3，統一正規化為 2
		if err := tx.Model(&models.OrderItem{}).Where("order_id = ? AND cancel_flag = 3", id).Update("cancel_flag", 2).Error; err != nil {
			return err
		}
		return UpdateOrderDeliveryStatus(tx, id)
	})
	if err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("停貨成功").Send()
}

// SearchOrderItems 搜尋客戶未交齊的訂貨明細（供出貨單選擇用）
// 同一型號可能來自多張訂貨單，每筆都獨立列出
func SearchOrderItems(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	customerID := c.Query("customer_id")
	if customerID == "" {
		resp.Fail(400, "請提供 customer_id").Send()
		return
	}

	// 查該客戶未交齊的訂貨單中，未停貨的明細
	query := db.GetRead().
		Where("order_items.cancel_flag < 2").
		Joins("JOIN orders ON orders.id = order_items.order_id AND orders.deleted_at IS NULL AND orders.delivery_status < 2 AND orders.customer_id = ?", customerID).
		Joins("JOIN retail_customers ON retail_customers.id = orders.customer_id AND retail_customers.is_visible = true").
		Preload("Product").
		Preload("Product.Size1Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Product.Size2Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Product.Size3Group.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Product.CategoryMaps", func(db *gorm.DB) *gorm.DB {
			return db.Where("category_type = 5")
		}).
		Preload("Product.CategoryMaps.Category5").
		Preload("SizeGroup.Options", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC")
		}).
		Preload("Sizes").
		Order("order_items.id DESC")

	// 型號搜尋
	if v := c.Query("search"); v != "" {
		like := "%" + v + "%"
		query = query.Where("order_items.product_id IN (SELECT id FROM products WHERE deleted_at IS NULL AND (model_code ILIKE ? OR name_spec ILIKE ?))", like, like)
	}

	var items []models.OrderItem
	query.Limit(50).Find(&items)

	// 查每筆明細的訂貨單號
	type orderRef struct {
		ID      int64
		OrderNo string
	}
	var orderIDs []int64
	for _, item := range items {
		orderIDs = append(orderIDs, item.OrderID)
	}
	orderNoMap := map[int64]string{}
	if len(orderIDs) > 0 {
		var refs []orderRef
		db.GetRead().Model(&models.Order{}).Select("id, order_no").Where("id IN ?", orderIDs).Scan(&refs)
		for _, r := range refs {
			orderNoMap[r.ID] = r.OrderNo
		}
	}

	// 查已出貨數量
	var allItemIDs []int64
	for _, item := range items {
		allItemIDs = append(allItemIDs, item.ID)
	}
	shipped := ShippedQtyMap(db.GetRead(), allItemIDs)

	// 查庫存（依客戶）
	var productIDs []int64
	for _, item := range items {
		productIDs = append(productIDs, item.ProductID)
	}
	var stocks []models.ProductSizeStock
	if len(productIDs) > 0 {
		db.GetRead().Where("product_id IN ? AND customer_id = ?", productIDs, customerID).Find(&stocks)
	}
	stockMap := map[string]int{} // "productId-sizeOptionId" → qty
	for _, s := range stocks {
		key := fmt.Sprintf("%d-%d", s.ProductID, s.SizeOptionID)
		stockMap[key] += s.Qty
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"items":        items,
		"order_no_map": orderNoMap,
		"shipped":      shipped,
		"stock_map":    stockMap,
	}).Send()
}
