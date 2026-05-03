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

// stockSummaryRow 廠商進貨統計列
type stockSummaryRow struct {
	GroupLabel string `json:"group_label"`
	VendorID   int64  `json:"vendor_id,omitempty"`
	VendorName string `json:"vendor_name,omitempty"`
	ModelCode  string `json:"model_code,omitempty"`

	StockQty     int     `json:"stock_qty"`
	StockAmount  float64 `json:"stock_amount"`
	ReturnQty    int     `json:"return_qty"`
	ReturnAmount float64 `json:"return_amount"`
	NetQty       int     `json:"net_qty"`
	NetAmount    float64 `json:"net_amount"`
	TaxAmount    float64 `json:"tax_amount"`
	TotalAmount  float64 `json:"total_amount"`

	// detail 專用
	StockID   int64   `json:"stock_id,omitempty"`
	StockNo   string  `json:"stock_no,omitempty"`
	StockDate string  `json:"stock_date,omitempty"`
	StockMode int     `json:"stock_mode,omitempty"` // 1=進貨 2=退貨
	UnitPrice float64 `json:"unit_price,omitempty"`
}

// GetStockSummary 廠商進貨統計
// GET /api/admin/stocks/summary
func GetStockSummary(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	tab := c.DefaultQuery("tab", "summary")
	groupBy := c.DefaultQuery("group_by", "model")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	vendorIDs := c.QueryArray("vendor_id")
	customerIDs := c.QueryArray("customer_id") // 店櫃
	modelCodeFrom := c.Query("model_code_from")
	modelCodeTo := c.Query("model_code_to")
	brandIDStrs := c.QueryArray("brand_id")
	stockModeStr := c.Query("stock_mode") // "" | "1" | "2"
	remark := strings.TrimSpace(c.Query("remark"))

	var brandIDs []int64
	for _, s := range brandIDStrs {
		if bid, err := strconv.ParseInt(s, 10, 64); err == nil {
			brandIDs = append(brandIDs, bid)
		}
	}

	query := db.GetRead().Model(&models.Stock{}).Select("stocks.*")

	if dateFrom != "" {
		query = query.Where("stocks.stock_date >= ?", dateFrom)
	}
	if dateTo != "" {
		query = query.Where("stocks.stock_date <= ?", dateTo)
	}
	if len(vendorIDs) > 0 {
		var vids []int64
		for _, s := range vendorIDs {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil {
				vids = append(vids, v)
			}
		}
		if len(vids) > 0 {
			query = query.Where("stocks.vendor_id IN (?)", vids)
		}
	}
	if len(customerIDs) > 0 {
		var cids []int64
		for _, s := range customerIDs {
			if cid, err := strconv.ParseInt(s, 10, 64); err == nil {
				cids = append(cids, cid)
			}
		}
		if len(cids) > 0 {
			query = query.Where("stocks.customer_id IN (?)", cids)
		}
	}
	if stockModeStr == "1" || stockModeStr == "2" {
		query = query.Where("stocks.stock_mode = ?", stockModeStr)
	} else {
		query = query.Where("stocks.stock_mode IN (1, 2)")
	}
	if remark != "" {
		query = query.Where("stocks.remark ILIKE ?", "%"+remark+"%")
	}

	// 明細層過濾：型號 / 品牌 —— 改從 SQL WHERE 過濾，避免先 Preload 全部再於應用層 filter
	modelFrag, modelArgs := BuildModelCodeRangeWhere("products.model_code", modelCodeFrom, modelCodeTo)
	applyItemFilter := func(q *gorm.DB) *gorm.DB {
		if modelFrag != "" || len(brandIDs) > 0 {
			q = q.Joins("JOIN products ON products.id = stock_items.product_id")
			if modelFrag != "" {
				q = q.Where(modelFrag, modelArgs...)
			}
			if len(brandIDs) > 0 {
				q = q.Where("products.brand_id IN ?", brandIDs)
			}
		}
		return q
	}
	hasItemFilter := modelFrag != "" || len(brandIDs) > 0
	if hasItemFilter {
		sub := applyItemFilter(db.GetRead().Model(&models.StockItem{}).Select("stock_items.stock_id"))
		query = query.Where("stocks.id IN (?)", sub)
	}

	var stocks []models.Stock
	query.Preload("Vendor").
		Preload("Items", func(q *gorm.DB) *gorm.DB {
			if hasItemFilter {
				return applyItemFilter(q).Select("stock_items.*")
			}
			return q
		}).
		Preload("Items.Product.Brand").
		Find(&stocks)

	if len(stocks) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"rows":   []stockSummaryRow{},
			"footer": emptyStockSummaryFooter(),
		}).Send()
		return
	}

	type lineEntry struct {
		stockID    int64
		stockNo    string
		stockDate  string
		stockMode  int
		vendorID   int64
		vendorName string
		modelCode  string
		qty        int
		unitPrice  float64
		amount     float64 // 未稅
		taxAmount  float64
	}

	var lines []lineEntry
	for _, st := range stocks {
		vendorName := ""
		var vendorID int64
		if st.Vendor != nil {
			vendorID = st.Vendor.ID
			vendorName = st.Vendor.ShortName
			if vendorName == "" {
				vendorName = st.Vendor.Name
			}
		}
		for _, item := range st.Items {
			if item.Product == nil {
				continue
			}
			amount := math.Round(item.TotalAmount)
			tax := 0.0
			if st.TaxMode == 2 {
				tax = math.Round(amount * st.TaxRate / 100)
			}
			lines = append(lines, lineEntry{
				stockID:    st.ID,
				stockNo:    st.StockNo,
				stockDate:  st.StockDate,
				stockMode:  st.StockMode,
				vendorID:   vendorID,
				vendorName: vendorName,
				modelCode:  item.Product.ModelCode,
				qty:        item.TotalQty,
				unitPrice:  item.PurchasePrice,
				amount:     amount,
				taxAmount:  tax,
			})
		}
	}

	var rows []stockSummaryRow
	footer := stockSummaryRow{GroupLabel: "合計"}

	if tab == "detail" {
		sort.Slice(lines, func(i, j int) bool {
			if lines[i].stockDate != lines[j].stockDate {
				return lines[i].stockDate < lines[j].stockDate
			}
			return lines[i].stockNo < lines[j].stockNo
		})
		for _, l := range lines {
			row := stockSummaryRow{
				VendorID:   l.vendorID,
				VendorName: l.vendorName,
				ModelCode:  l.modelCode,
				StockID:    l.stockID,
				StockNo:    l.stockNo,
				StockDate:  l.stockDate,
				StockMode:  l.stockMode,
				UnitPrice:  l.unitPrice,
				TaxAmount:  l.taxAmount,
			}
			if l.stockMode == 2 {
				row.ReturnQty = l.qty
				row.ReturnAmount = l.amount
				row.NetQty = -l.qty
				row.NetAmount = -l.amount
			} else {
				row.StockQty = l.qty
				row.StockAmount = l.amount
				row.NetQty = l.qty
				row.NetAmount = l.amount
			}
			row.TotalAmount = row.NetAmount + row.TaxAmount
			if groupBy == "vendor" {
				row.GroupLabel = l.modelCode
			} else {
				row.GroupLabel = l.vendorName
			}
			rows = append(rows, row)
			accumulateStockRow(&footer, &row)
		}
	} else {
		type aggEntry struct {
			groupLabel string
			vendorID   int64
			vendorName string
			modelCode  string
			row        stockSummaryRow
		}
		aggMap := map[string]*aggEntry{}
		var order []string
		for _, l := range lines {
			var key, label string
			switch groupBy {
			case "vendor":
				key = strconv.FormatInt(l.vendorID, 10) + "|" + l.vendorName
				label = l.vendorName
			default:
				key = l.modelCode
				label = l.modelCode
			}
			e, ok := aggMap[key]
			if !ok {
				e = &aggEntry{
					groupLabel: label,
					vendorID:   l.vendorID,
					vendorName: l.vendorName,
					modelCode:  l.modelCode,
				}
				e.row.GroupLabel = label
				aggMap[key] = e
				order = append(order, key)
			}
			per := stockSummaryRow{TaxAmount: l.taxAmount}
			if l.stockMode == 2 {
				per.ReturnQty = l.qty
				per.ReturnAmount = l.amount
				per.NetQty = -l.qty
				per.NetAmount = -l.amount
			} else {
				per.StockQty = l.qty
				per.StockAmount = l.amount
				per.NetQty = l.qty
				per.NetAmount = l.amount
			}
			per.TotalAmount = per.NetAmount + per.TaxAmount
			accumulateStockRow(&e.row, &per)
		}
		if groupBy == "vendor" {
			sort.Strings(order)
		} else {
			sort.Slice(order, func(i, j int) bool {
				return ModelCodeNaturalLess(order[i], order[j])
			})
		}
		for _, k := range order {
			e := aggMap[k]
			if groupBy == "vendor" {
				e.row.VendorID = e.vendorID
				e.row.VendorName = e.vendorName
			} else {
				e.row.ModelCode = e.modelCode
			}
			rows = append(rows, e.row)
			accumulateStockRow(&footer, &e.row)
		}
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"rows":   rows,
		"footer": footer,
	}).Send()
}

func accumulateStockRow(dst, src *stockSummaryRow) {
	dst.StockQty += src.StockQty
	dst.StockAmount += src.StockAmount
	dst.ReturnQty += src.ReturnQty
	dst.ReturnAmount += src.ReturnAmount
	dst.NetQty += src.NetQty
	dst.NetAmount += src.NetAmount
	dst.TaxAmount += src.TaxAmount
	dst.TotalAmount += src.TotalAmount
}

func emptyStockSummaryFooter() stockSummaryRow {
	return stockSummaryRow{GroupLabel: "合計"}
}
