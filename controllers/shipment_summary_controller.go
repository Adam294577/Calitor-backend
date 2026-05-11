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

// shipmentSummaryRow 客戶出貨統計列
type shipmentSummaryRow struct {
	GroupLabel   string `json:"group_label"`
	CustomerID   int64  `json:"customer_id,omitempty"`
	CustomerCode string `json:"customer_code,omitempty"`
	CustomerName string `json:"customer_name,omitempty"`
	ModelCode    string `json:"model_code,omitempty"`

	ShipQty      int     `json:"ship_qty"`
	ShipAmount   float64 `json:"ship_amount"`
	ReturnQty    int     `json:"return_qty"`
	ReturnAmount float64 `json:"return_amount"`
	NetQty       int     `json:"net_qty"`
	NetAmount    float64 `json:"net_amount"`
	TaxAmount    float64 `json:"tax_amount"`
	TotalAmount  float64 `json:"total_amount"`
	Cost         float64 `json:"cost"`
	Gross        float64 `json:"gross"`
	GrossRate    float64 `json:"gross_rate"`

	// detail 專用
	ShipmentID   int64   `json:"shipment_id,omitempty"`
	ShipmentNo   string  `json:"shipment_no,omitempty"`
	ShipmentDate string  `json:"shipment_date,omitempty"`
	ShipmentMode int     `json:"shipment_mode,omitempty"` // 3=出貨 4=退貨
	UnitPrice    float64 `json:"unit_price,omitempty"`
	Discount     float64 `json:"discount,omitempty"`
}

// GetShipmentSummary 客戶出貨統計
// GET /api/admin/shipments/summary
func GetShipmentSummary(c *gin.Context) {
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
	brandIDStrs := c.QueryArray("brand_id")
	shipModeStr := c.Query("ship_mode")    // "" | "3" | "4"
	supplementStr := c.Query("supplement") // "" | "1" | "2"
	dealModeStr := c.Query("deal_mode")    // "" | "1" | "2"
	remark := strings.TrimSpace(c.Query("remark"))

	var brandIDs []int64
	for _, s := range brandIDStrs {
		if bid, err := strconv.ParseInt(s, 10, 64); err == nil {
			brandIDs = append(brandIDs, bid)
		}
	}

	query := db.GetRead().Model(&models.Shipment{}).
		Select("shipments.*").
		Joins("JOIN retail_customers ON retail_customers.id = shipments.customer_id AND retail_customers.is_visible = true")

	if dateFrom != "" {
		query = query.Where("shipments.shipment_date >= ?", dateFrom)
	}
	if dateTo != "" {
		query = query.Where("shipments.shipment_date <= ?", dateTo)
	}
	if len(customerIDs) > 0 {
		var cids []int64
		for _, s := range customerIDs {
			if cid, err := strconv.ParseInt(s, 10, 64); err == nil {
				cids = append(cids, cid)
			}
		}
		if len(cids) > 0 {
			query = query.Where("shipments.customer_id IN (?)", cids)
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
			query = query.Where("shipments.salesman_id IN (?)", sids)
		}
	}
	if shipModeStr == "3" || shipModeStr == "4" {
		query = query.Where("shipments.shipment_mode = ?", shipModeStr)
	} else {
		query = query.Where("shipments.shipment_mode IN (3, 4)")
	}
	if dealModeStr == "1" || dealModeStr == "2" {
		query = query.Where("shipments.deal_mode = ?", dealModeStr)
	}
	if remark != "" {
		query = query.Where("shipments.remark ILIKE ?", "%"+remark+"%")
	}

	// 明細層過濾：舖補 / 型號 / 品牌 —— 改從 SQL WHERE 過濾，避免先 Preload 全部再於應用層 filter 造成 N+1
	modelFrag, modelArgs := BuildModelCodeRangeWhere("products.model_code", modelCodeFrom, modelCodeTo)
	applyItemFilter := func(q *gorm.DB) *gorm.DB {
		if supplementStr == "1" || supplementStr == "2" {
			q = q.Where("shipment_items.supplement = ?", supplementStr)
		}
		if modelFrag != "" || len(brandIDs) > 0 {
			q = q.Joins("JOIN products ON products.id = shipment_items.product_id")
			if modelFrag != "" {
				q = q.Where(modelFrag, modelArgs...)
			}
			if len(brandIDs) > 0 {
				q = q.Where("products.brand_id IN ?", brandIDs)
			}
		}
		return q
	}
	hasItemFilter := (supplementStr == "1" || supplementStr == "2") || modelFrag != "" || len(brandIDs) > 0
	if hasItemFilter {
		sub := applyItemFilter(db.GetRead().Model(&models.ShipmentItem{}).Select("shipment_items.shipment_id"))
		query = query.Where("shipments.id IN (?)", sub)
	}

	var shipments []models.Shipment
	query.Preload("Customer").
		Preload("Items", func(q *gorm.DB) *gorm.DB {
			if hasItemFilter {
				return applyItemFilter(q).Select("shipment_items.*")
			}
			return q
		}).
		Preload("Items.Product.Brand").
		Find(&shipments)

	if len(shipments) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"rows":   []shipmentSummaryRow{},
			"footer": emptySummaryFooter(),
		}).Send()
		return
	}

	// 成本以「出貨日(含)之前的所有進貨」加權平均計算,而非讀 shipment_items.ship_cost
	productIDSet := map[int64]struct{}{}
	for _, s := range shipments {
		for _, item := range s.Items {
			if item.Product == nil {
				continue
			}
			productIDSet[item.ProductID] = struct{}{}
		}
	}
	type purchaseRow struct {
		ProductID     int64   `gorm:"column:product_id"`
		PurchaseDate  string  `gorm:"column:purchase_date"`
		PurchasePrice float64 `gorm:"column:purchase_price"`
		TotalQty      int     `gorm:"column:total_qty"`
	}
	purchasesByProduct := map[int64][]purchaseRow{}
	if len(productIDSet) > 0 {
		pids := make([]int64, 0, len(productIDSet))
		for pid := range productIDSet {
			pids = append(pids, pid)
		}
		var purchaseRows []purchaseRow
		db.GetRead().
			Table("purchase_items").
			Select("purchase_items.product_id, purchases.purchase_date, purchase_items.purchase_price, purchase_items.total_qty").
			Joins("JOIN purchases ON purchases.id = purchase_items.purchase_id AND purchases.deleted_at IS NULL").
			Where("purchase_items.product_id IN ?", pids).
			Where("purchase_items.cancel_flag = ?", 1).
			Where("purchase_items.total_qty > 0").
			Find(&purchaseRows)
		for _, r := range purchaseRows {
			purchasesByProduct[r.ProductID] = append(purchasesByProduct[r.ProductID], r)
		}
		for pid := range purchasesByProduct {
			rows := purchasesByProduct[pid]
			sort.Slice(rows, func(i, j int) bool {
				return rows[i].PurchaseDate < rows[j].PurchaseDate
			})
			purchasesByProduct[pid] = rows
		}
	}
	// 回傳出貨日(含)以前的加權平均單位成本;無進貨紀錄回 0
	weightedAvgCost := func(productID int64, asOfDate string) float64 {
		rows := purchasesByProduct[productID]
		if len(rows) == 0 {
			return 0
		}
		var totalAmount float64
		var totalQty int
		for _, r := range rows {
			if r.PurchaseDate > asOfDate {
				break
			}
			totalAmount += r.PurchasePrice * float64(r.TotalQty)
			totalQty += r.TotalQty
		}
		if totalQty == 0 {
			return 0
		}
		return totalAmount / float64(totalQty)
	}

	type lineEntry struct {
		shipmentID   int64
		shipmentNo   string
		shipmentDate string
		shipmentMode int
		customerID   int64
		customerCode string
		customerName string
		modelCode    string
		qty          int
		unitPrice    float64
		discount     float64
		amount       float64 // 未稅 (TotalAmount on ShipmentItem)
		cost         float64 // 加權平均進價 * qty(出貨日含以前的進貨)
		taxRate      float64
		taxAmount    float64
	}

	var lines []lineEntry
	for _, s := range shipments {
		customerCode := ""
		customerName := ""
		if s.Customer != nil {
			customerCode = s.Customer.Code
			customerName = s.Customer.ShortName
			if customerName == "" {
				customerName = s.Customer.Name
			}
		}
		for _, item := range s.Items {
			if item.Product == nil {
				continue
			}
			amount := math.Round(item.TotalAmount)
			cost := math.Round(weightedAvgCost(item.ProductID, s.ShipmentDate) * float64(item.TotalQty))
			tax := 0.0
			if s.TaxMode == 2 {
				tax = math.Round(amount * s.TaxRate / 100)
			}
			lines = append(lines, lineEntry{
				shipmentID:   s.ID,
				shipmentNo:   s.ShipmentNo,
				shipmentDate: s.ShipmentDate,
				shipmentMode: s.ShipmentMode,
				customerID:   s.CustomerID,
				customerCode: customerCode,
				customerName: customerName,
				modelCode:    item.Product.ModelCode,
				qty:          item.TotalQty,
				unitPrice:    item.ShipPrice,
				discount:     item.Discount,
				amount:       amount,
				cost:         cost,
				taxRate:      s.TaxRate,
				taxAmount:    tax,
			})
		}
	}

	var rows []shipmentSummaryRow
	footer := shipmentSummaryRow{GroupLabel: "合計"}

	if tab == "detail" {
		// detail 排序：出貨日期 asc，其次單號
		sort.Slice(lines, func(i, j int) bool {
			if lines[i].shipmentDate != lines[j].shipmentDate {
				return lines[i].shipmentDate < lines[j].shipmentDate
			}
			return lines[i].shipmentNo < lines[j].shipmentNo
		})
		for _, l := range lines {
			// 符號規範：出貨一律正、退貨一律負，避免歷史資料正負不一造成「負負得正」
			absQty := int(math.Abs(float64(l.qty)))
			absAmount := math.Abs(l.amount)
			absCost := math.Abs(l.cost)
			absTax := math.Abs(l.taxAmount)
			row := shipmentSummaryRow{
				CustomerID:   l.customerID,
				CustomerCode: l.customerCode,
				CustomerName: l.customerName,
				ModelCode:    l.modelCode,
				ShipmentID:   l.shipmentID,
				ShipmentNo:   l.shipmentNo,
				ShipmentDate: l.shipmentDate,
				ShipmentMode: l.shipmentMode,
				UnitPrice:    l.unitPrice,
				Discount:     l.discount,
			}
			if l.shipmentMode == 4 {
				row.ReturnQty = -absQty
				row.ReturnAmount = -absAmount
				row.NetQty = -absQty
				row.NetAmount = -absAmount
				row.Cost = -absCost
				row.TaxAmount = -absTax
			} else {
				row.ShipQty = absQty
				row.ShipAmount = absAmount
				row.NetQty = absQty
				row.NetAmount = absAmount
				row.Cost = absCost
				row.TaxAmount = absTax
			}
			row.TotalAmount = row.NetAmount + row.TaxAmount
			row.Gross = row.NetAmount - row.Cost
			row.GrossRate = grossRate(row.Gross, row.NetAmount)
			// detail 模式下 group_label 依 groupBy 顯示
			if groupBy == "customer" {
				row.GroupLabel = l.modelCode
			} else {
				row.GroupLabel = l.customerName
			}
			rows = append(rows, row)
			accumulate(&footer, &row)
		}
	} else {
		type aggEntry struct {
			groupLabel   string
			customerID   int64
			customerCode string
			customerName string
			modelCode    string
			row          shipmentSummaryRow
		}
		aggMap := map[string]*aggEntry{}
		var order []string
		for _, l := range lines {
			var key string
			var label string
			var entry *aggEntry
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
			entry = e
			// 符號規範：出貨一律正、退貨一律負
			absQty := int(math.Abs(float64(l.qty)))
			absAmount := math.Abs(l.amount)
			absCost := math.Abs(l.cost)
			absTax := math.Abs(l.taxAmount)
			per := shipmentSummaryRow{}
			if l.shipmentMode == 4 {
				per.ReturnQty = -absQty
				per.ReturnAmount = -absAmount
				per.NetQty = -absQty
				per.NetAmount = -absAmount
				per.Cost = -absCost
				per.TaxAmount = -absTax
			} else {
				per.ShipQty = absQty
				per.ShipAmount = absAmount
				per.NetQty = absQty
				per.NetAmount = absAmount
				per.Cost = absCost
				per.TaxAmount = absTax
			}
			per.TotalAmount = per.NetAmount + per.TaxAmount
			accumulateRow(&entry.row, &per)
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
			accumulate(&footer, &e.row)
		}
	}

	footer.Gross = footer.NetAmount - footer.Cost
	footer.GrossRate = grossRate(footer.Gross, footer.NetAmount)

	resp.Success("成功").SetData(map[string]interface{}{
		"rows":   rows,
		"footer": footer,
	}).Send()
}

func grossRate(gross, base float64) float64 {
	if base == 0 {
		return 0
	}
	return math.Round(gross / base * 100)
}

func accumulateRow(dst, src *shipmentSummaryRow) {
	dst.ShipQty += src.ShipQty
	dst.ShipAmount += src.ShipAmount
	dst.ReturnQty += src.ReturnQty
	dst.ReturnAmount += src.ReturnAmount
	dst.NetQty += src.NetQty
	dst.NetAmount += src.NetAmount
	dst.TaxAmount += src.TaxAmount
	dst.TotalAmount += src.TotalAmount
	dst.Cost += src.Cost
}

func accumulate(footer, row *shipmentSummaryRow) {
	accumulateRow(footer, row)
}

func emptySummaryFooter() shipmentSummaryRow {
	return shipmentSummaryRow{GroupLabel: "合計"}
}
