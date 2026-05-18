package controllers

import (
	"math"
	"project/models"
	response "project/services/responses"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// orderSummaryRow 客戶訂貨統計列
type orderSummaryRow struct {
	GroupLabel   string `json:"group_label"`
	CustomerID   int64  `json:"customer_id,omitempty"`
	CustomerCode string `json:"customer_code,omitempty"`
	CustomerName string `json:"customer_name,omitempty"`
	ModelCode    string `json:"model_code,omitempty"`

	OrderQty    int     `json:"order_qty"`
	NetAmount   float64 `json:"net_amount"`
	TaxAmount   float64 `json:"tax_amount"`
	TotalAmount float64 `json:"total_amount"`
	Cost        float64 `json:"cost"`
	Gross       float64 `json:"gross"`
	GrossRate   float64 `json:"gross_rate"`

	// detail 專用
	OrderID   int64   `json:"order_id,omitempty"`
	OrderNo   string  `json:"order_no,omitempty"`
	OrderDate string  `json:"order_date,omitempty"`
	UnitPrice float64 `json:"unit_price,omitempty"`
	Discount  float64 `json:"discount,omitempty"`
}

// GetOrderSummary 客戶訂貨統計
// GET /api/admin/orders/summary
func GetOrderSummary(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	tab := c.DefaultQuery("tab", "summary")
	groupBy := c.DefaultQuery("group_by", "model")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	customerIDs := c.QueryArray("customer_id")
	salesmanIDs := c.QueryArray("salesman_id")
	modelCodeFrom := c.Query("model_code_from")
	modelCodeTo := c.Query("model_code_to")
	brandCodeFrom := strings.TrimSpace(c.Query("brand_code_from"))
	brandCodeTo := strings.TrimSpace(c.Query("brand_code_to"))
	brandIDStrs := c.QueryArray("brand_id")
	supplementStr := c.Query("supplement") // "" | "1" | "2"
	dealModeStr := c.Query("deal_mode")    // "" | "1" | "2"
	remark := strings.TrimSpace(c.Query("remark"))

	var brandIDs []int64
	for _, s := range brandIDStrs {
		if bid, err := strconv.ParseInt(s, 10, 64); err == nil {
			brandIDs = append(brandIDs, bid)
		}
	}

	query := db.GetRead().Model(&models.Order{}).
		Select("orders.*").
		Joins("JOIN retail_customers ON retail_customers.id = orders.customer_id AND retail_customers.is_visible = true")

	if dateFrom != "" {
		query = query.Where("orders.order_date >= ?", dateFrom)
	}
	if dateTo != "" {
		query = query.Where("orders.order_date <= ?", dateTo)
	}
	if len(customerIDs) > 0 {
		var cids []int64
		for _, s := range customerIDs {
			if cid, err := strconv.ParseInt(s, 10, 64); err == nil {
				cids = append(cids, cid)
			}
		}
		if len(cids) > 0 {
			query = query.Where("orders.customer_id IN (?)", cids)
		}
	}
	if len(salesmanIDs) > 0 {
		var sids []int64
		for _, s := range salesmanIDs {
			if sid, err := strconv.ParseInt(s, 10, 64); err == nil {
				sids = append(sids, sid)
			}
		}
		if len(sids) > 0 {
			query = query.Where("orders.salesman_id IN (?)", sids)
		}
	}
	if dealModeStr == "1" || dealModeStr == "2" {
		query = query.Where("orders.deal_mode = ?", dealModeStr)
	}
	if remark != "" {
		query = query.Where("orders.remark ILIKE ?", "%"+remark+"%")
	}

	// 明細層過濾：舖補 / 型號 / 品牌(對帳品牌 brand_id 多選) / 商品品牌區間(product_brands.code) + 排除已清除/停 (cancel_flag NOT IN (2,3))
	modelFrag, modelArgs := BuildModelCodeRangeWhere("products.model_code", modelCodeFrom, modelCodeTo)
	brandFrag, brandArgs := BuildModelCodeRangeWhere("product_brands.code", brandCodeFrom, brandCodeTo)
	applyItemFilter := func(q *gorm.DB) *gorm.DB {
		q = q.Where("order_items.cancel_flag NOT IN (?)", []int{2, 3})
		if supplementStr == "1" || supplementStr == "2" {
			q = q.Where("order_items.supplement = ?", supplementStr)
		}
		if modelFrag != "" || len(brandIDs) > 0 || brandFrag != "" {
			q = q.Joins("JOIN products ON products.id = order_items.product_id")
			if modelFrag != "" {
				q = q.Where(modelFrag, modelArgs...)
			}
			if len(brandIDs) > 0 {
				q = q.Where("products.brand_id IN ?", brandIDs)
			}
			if brandFrag != "" {
				q = q.Joins("LEFT JOIN product_brands ON product_brands.id = products.product_brand_id")
				q = q.Where(brandFrag, brandArgs...)
			}
		}
		return q
	}
	// 永遠排除已清除明細,故 hasItemFilter 一定為 true
	sub := applyItemFilter(db.GetRead().Model(&models.OrderItem{}).Select("order_items.order_id"))
	query = query.Where("orders.id IN (?)", sub)

	var orders []models.Order
	query.Preload("Customer").
		Preload("Items", func(q *gorm.DB) *gorm.DB {
			return applyItemFilter(q).Select("order_items.*")
		}).
		Preload("Items.Product.Brand").
		Find(&orders)

	if len(orders) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"rows":   []orderSummaryRow{},
			"footer": emptyOrderSummaryFooter(),
		}).Send()
		return
	}

	// 收集所有 product_id 與最大訂單日,批次撈進貨資料用以計算「訂單日(含)以前最近一筆進貨價」。
	// 與「客戶出貨統計」(shipment_summary_controller) 對齊邏輯,不再讀 product_vendors.cost_last。
	productIDSet := map[int64]bool{}
	maxOrderDate := ""
	for _, o := range orders {
		if o.OrderDate > maxOrderDate {
			maxOrderDate = o.OrderDate
		}
		for _, item := range o.Items {
			if item.ProductID > 0 {
				productIDSet[item.ProductID] = true
			}
		}
	}
	productIDs := make([]int64, 0, len(productIDSet))
	for id := range productIDSet {
		productIDs = append(productIDs, id)
	}
	type stockRow struct {
		ProductID     int64   `gorm:"column:product_id"`
		StockDate     string  `gorm:"column:stock_date"`
		PurchasePrice float64 `gorm:"column:purchase_price"`
	}
	stocksByProduct := map[int64][]stockRow{}
	if len(productIDs) > 0 {
		var stockRows []stockRow
		sq := db.GetRead().
			Table("stock_items").
			Select("stock_items.product_id, stocks.stock_date, stock_items.purchase_price").
			Joins("JOIN stocks ON stocks.id = stock_items.stock_id AND stocks.deleted_at IS NULL AND stocks.stock_mode = 1").
			Where("stock_items.product_id IN ?", productIDs).
			Where("stock_items.total_qty > 0")
		if maxOrderDate != "" {
			sq = sq.Where("stocks.stock_date <= ?", maxOrderDate)
		}
		sq.Find(&stockRows)
		for _, r := range stockRows {
			stocksByProduct[r.ProductID] = append(stocksByProduct[r.ProductID], r)
		}
		for pid := range stocksByProduct {
			rows := stocksByProduct[pid]
			sort.Slice(rows, func(i, j int) bool {
				return rows[i].StockDate < rows[j].StockDate
			})
			stocksByProduct[pid] = rows
		}
	}
	// 回傳訂單日(含)以前最近一筆進貨單的單位進價;無進貨紀錄回 0
	latestStockPrice := func(productID int64, asOfDate string) float64 {
		rows := stocksByProduct[productID]
		if len(rows) == 0 {
			return 0
		}
		price := 0.0
		for _, r := range rows {
			if r.StockDate > asOfDate {
				break
			}
			price = r.PurchasePrice
		}
		return price
	}

	type lineEntry struct {
		orderID      int64
		orderNo      string
		orderDate    string
		customerID   int64
		customerCode string
		customerName string
		modelCode    string
		qty          int
		unitPrice    float64
		discount     float64
		amount       float64 // 未稅 (TotalAmount on OrderItem)
		cost         float64 // 訂單日(含)以前最近一筆進貨價 * qty
		taxAmount    float64
	}

	var lines []lineEntry
	for _, o := range orders {
		customerCode := ""
		customerName := ""
		if o.Customer != nil {
			customerCode = o.Customer.Code
			customerName = o.Customer.ShortName
			if customerName == "" {
				customerName = o.Customer.Name
			}
		}
		for _, item := range o.Items {
			if item.Product == nil {
				continue
			}
			amount := math.Round(item.TotalAmount)
			cost := math.Round(latestStockPrice(item.ProductID, o.OrderDate) * float64(item.TotalQty))
			tax := 0.0
			if o.TaxMode == 2 {
				tax = math.Round(amount * o.TaxRate / 100)
			}
			lines = append(lines, lineEntry{
				orderID:      o.ID,
				orderNo:      o.OrderNo,
				orderDate:    o.OrderDate,
				customerID:   o.CustomerID,
				customerCode: customerCode,
				customerName: customerName,
				modelCode:    item.Product.ModelCode,
				qty:          item.TotalQty,
				unitPrice:    item.OrderPrice,
				discount:     item.Discount,
				amount:       amount,
				cost:         cost,
				taxAmount:    tax,
			})
		}
	}

	var rows []orderSummaryRow
	footer := orderSummaryRow{GroupLabel: "合計"}

	if tab == "detail" {
		sort.Slice(lines, func(i, j int) bool {
			if lines[i].orderDate != lines[j].orderDate {
				return lines[i].orderDate < lines[j].orderDate
			}
			return lines[i].orderNo < lines[j].orderNo
		})
		for _, l := range lines {
			row := orderSummaryRow{
				CustomerID:   l.customerID,
				CustomerCode: l.customerCode,
				CustomerName: l.customerName,
				ModelCode:    l.modelCode,
				OrderID:      l.orderID,
				OrderNo:      l.orderNo,
				OrderDate:    l.orderDate,
				UnitPrice:    l.unitPrice,
				Discount:     l.discount,
				OrderQty:     l.qty,
				NetAmount:    l.amount,
				TaxAmount:    l.taxAmount,
				Cost:         l.cost,
			}
			row.TotalAmount = row.NetAmount + row.TaxAmount
			row.Gross = row.NetAmount - row.Cost
			row.GrossRate = grossRate(row.Gross, row.NetAmount)
			if groupBy == "customer" {
				row.GroupLabel = l.modelCode
			} else {
				row.GroupLabel = l.customerName
			}
			rows = append(rows, row)
			accumulateOrderRow(&footer, &row)
		}
	} else {
		type aggEntry struct {
			groupLabel   string
			customerID   int64
			customerCode string
			customerName string
			modelCode    string
			row          orderSummaryRow
		}
		aggMap := map[string]*aggEntry{}
		var order []string
		for _, l := range lines {
			var key, label string
			switch groupBy {
			case "customer":
				key = l.customerCode + "|" + l.customerName
				label = l.customerName
			default:
				key = l.modelCode
				label = l.modelCode
			}
			e, ok := aggMap[key]
			if !ok {
				e = &aggEntry{
					groupLabel:   label,
					customerID:   l.customerID,
					customerCode: l.customerCode,
					customerName: l.customerName,
					modelCode:    l.modelCode,
				}
				e.row.GroupLabel = label
				aggMap[key] = e
				order = append(order, key)
			}
			per := orderSummaryRow{
				OrderQty:  l.qty,
				NetAmount: l.amount,
				TaxAmount: l.taxAmount,
				Cost:      l.cost,
			}
			per.TotalAmount = per.NetAmount + per.TaxAmount
			accumulateOrderRow(&e.row, &per)
		}
		if groupBy == "customer" {
			sort.Strings(order)
		} else {
			sort.Slice(order, func(i, j int) bool {
				return ModelCodeNaturalLess(order[i], order[j])
			})
		}
		for _, k := range order {
			e := aggMap[k]
			e.row.Gross = e.row.NetAmount - e.row.Cost
			e.row.GrossRate = grossRate(e.row.Gross, e.row.NetAmount)
			if groupBy == "customer" {
				e.row.CustomerID = e.customerID
				e.row.CustomerCode = e.customerCode
				e.row.CustomerName = e.customerName
			} else {
				e.row.ModelCode = e.modelCode
			}
			rows = append(rows, e.row)
			accumulateOrderRow(&footer, &e.row)
		}
	}

	footer.Gross = footer.NetAmount - footer.Cost
	footer.GrossRate = grossRate(footer.Gross, footer.NetAmount)

	resp.Success("成功").SetData(map[string]interface{}{
		"rows":   rows,
		"footer": footer,
	}).Send()
}

func accumulateOrderRow(dst, src *orderSummaryRow) {
	dst.OrderQty += src.OrderQty
	dst.NetAmount += src.NetAmount
	dst.TaxAmount += src.TaxAmount
	dst.TotalAmount += src.TotalAmount
	dst.Cost += src.Cost
}

func emptyOrderSummaryFooter() orderSummaryRow {
	return orderSummaryRow{GroupLabel: "合計"}
}
