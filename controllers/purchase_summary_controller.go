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

// purchaseSummaryRow 廠商採購統計列
type purchaseSummaryRow struct {
	GroupLabel  string `json:"group_label"`
	VendorID    int64  `json:"vendor_id,omitempty"`
	VendorCode  string `json:"vendor_code,omitempty"`
	VendorName  string `json:"vendor_name,omitempty"`
	ModelCode   string `json:"model_code,omitempty"`
	ProductName string `json:"product_name,omitempty"`

	PurchaseQty int     `json:"purchase_qty"`
	NetAmount   float64 `json:"net_amount"`
	TaxAmount   float64 `json:"tax_amount"`
	TotalAmount float64 `json:"total_amount"`

	// detail 專用
	PurchaseID   int64   `json:"purchase_id,omitempty"`
	PurchaseNo   string  `json:"purchase_no,omitempty"`
	PurchaseDate string  `json:"purchase_date,omitempty"`
	UnitPrice    float64 `json:"unit_price,omitempty"`
}

// GetPurchaseSummary 廠商採購統計
// GET /api/admin/purchases/summary
func GetPurchaseSummary(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	tab := c.DefaultQuery("tab", "summary")
	groupBy := c.DefaultQuery("group_by", "model")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	vendorIDs := c.QueryArray("vendor_id")
	modelCodeFrom := c.Query("model_code_from")
	modelCodeTo := c.Query("model_code_to")
	brandCodeFrom := strings.TrimSpace(c.Query("brand_code_from"))
	brandCodeTo := strings.TrimSpace(c.Query("brand_code_to"))
	brandIDStrs := c.QueryArray("product_brand_id")
	dealModeStr := c.Query("deal_mode") // "" | "1" | "2"

	var brandIDs []int64
	for _, s := range brandIDStrs {
		if bid, err := strconv.ParseInt(s, 10, 64); err == nil {
			brandIDs = append(brandIDs, bid)
		}
	}

	query := db.GetRead().Model(&models.Purchase{}).Select("purchases.*")

	if dateFrom != "" {
		query = query.Where("purchases.purchase_date >= ?", dateFrom)
	}
	if dateTo != "" {
		query = query.Where("purchases.purchase_date <= ?", dateTo)
	}
	if len(vendorIDs) > 0 {
		var vids []int64
		for _, s := range vendorIDs {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil {
				vids = append(vids, v)
			}
		}
		if len(vids) > 0 {
			query = query.Where("purchases.vendor_id IN (?)", vids)
		}
	}
	if dealModeStr == "1" || dealModeStr == "2" {
		query = query.Where("purchases.deal_mode = ?", dealModeStr)
	}

	// 明細層過濾：型號 / 品牌(product_brand_id 多選) / 商品品牌區間(product_brands.code) — cancel_flag=1 排除停交 line
	modelFrag, modelArgs := BuildModelCodeRangeWhere("products.model_code", modelCodeFrom, modelCodeTo)
	brandFrag, brandArgs := BuildModelCodeRangeWhere("product_brands.code", brandCodeFrom, brandCodeTo)
	applyItemFilter := func(q *gorm.DB) *gorm.DB {
		q = q.Where("purchase_items.cancel_flag = ?", 1)
		if modelFrag != "" || len(brandIDs) > 0 || brandFrag != "" {
			q = q.Joins("JOIN products ON products.id = purchase_items.product_id")
			if modelFrag != "" {
				q = q.Where(modelFrag, modelArgs...)
			}
			if len(brandIDs) > 0 {
				q = q.Where("products.product_brand_id IN ?", brandIDs)
			}
			if brandFrag != "" {
				q = q.Joins("LEFT JOIN product_brands ON product_brands.id = products.product_brand_id")
				q = q.Where(brandFrag, brandArgs...)
			}
		}
		return q
	}
	// 採購單層只挑選有正常明細的
	sub := applyItemFilter(db.GetRead().Model(&models.PurchaseItem{}).Select("purchase_items.purchase_id"))
	query = query.Where("purchases.id IN (?)", sub)

	var purchases []models.Purchase
	query.Preload("Vendor").
		Preload("Items", func(q *gorm.DB) *gorm.DB {
			return applyItemFilter(q).Select("purchase_items.*")
		}).
		Preload("Items.Product").
		Find(&purchases)

	if len(purchases) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"rows":   []purchaseSummaryRow{},
			"footer": emptyPurchaseSummaryFooter(),
		}).Send()
		return
	}

	type lineEntry struct {
		purchaseID   int64
		purchaseNo   string
		purchaseDate string
		vendorID     int64
		vendorCode   string
		vendorName   string
		modelCode    string
		productName  string
		qty          int
		unitPrice    float64
		amount       float64
		taxAmount    float64
	}

	var lines []lineEntry
	for _, p := range purchases {
		var vendorID int64
		vendorCode := ""
		vendorName := ""
		if p.Vendor != nil {
			vendorID = p.Vendor.ID
			vendorCode = p.Vendor.Code
			vendorName = p.Vendor.ShortName
			if vendorName == "" {
				vendorName = p.Vendor.Name
			}
		}
		for _, item := range p.Items {
			if item.Product == nil {
				continue
			}
			amount := math.Round(item.TotalAmount)
			tax := 0.0
			if p.TaxMode == 2 {
				tax = math.Round(amount * p.TaxRate / 100)
			}
			lines = append(lines, lineEntry{
				purchaseID:   p.ID,
				purchaseNo:   p.PurchaseNo,
				purchaseDate: p.PurchaseDate,
				vendorID:     vendorID,
				vendorCode:   vendorCode,
				vendorName:   vendorName,
				modelCode:    item.Product.ModelCode,
				productName:  item.Product.NameSpec,
				qty:          item.TotalQty,
				unitPrice:    item.PurchasePrice,
				amount:       amount,
				taxAmount:    tax,
			})
		}
	}

	var rows []purchaseSummaryRow
	footer := purchaseSummaryRow{GroupLabel: "合計"}

	if tab == "detail" {
		// 明細：依採購日期 asc，再依採購單號
		sort.Slice(lines, func(i, j int) bool {
			if lines[i].purchaseDate != lines[j].purchaseDate {
				return lines[i].purchaseDate < lines[j].purchaseDate
			}
			return lines[i].purchaseNo < lines[j].purchaseNo
		})
		for _, l := range lines {
			row := purchaseSummaryRow{
				VendorID:     l.vendorID,
				VendorCode:   l.vendorCode,
				VendorName:   l.vendorName,
				ModelCode:    l.modelCode,
				ProductName:  l.productName,
				PurchaseID:   l.purchaseID,
				PurchaseNo:   l.purchaseNo,
				PurchaseDate: l.purchaseDate,
				UnitPrice:    l.unitPrice,
				PurchaseQty:  l.qty,
				NetAmount:    l.amount,
				TaxAmount:    l.taxAmount,
			}
			row.TotalAmount = row.NetAmount + row.TaxAmount
			if groupBy == "vendor" {
				row.GroupLabel = l.modelCode
			} else {
				row.GroupLabel = l.vendorName
			}
			rows = append(rows, row)
			accumulatePurchaseRow(&footer, &row)
		}
	} else {
		type aggEntry struct {
			groupLabel  string
			vendorID    int64
			vendorCode  string
			vendorName  string
			modelCode   string
			productName string
			row         purchaseSummaryRow
		}
		aggMap := map[string]*aggEntry{}
		var order []string
		for _, l := range lines {
			var key, label string
			switch groupBy {
			case "vendor":
				key = strconv.FormatInt(l.vendorID, 10)
				label = l.vendorName
			default:
				key = l.modelCode
				label = l.modelCode
			}
			e, ok := aggMap[key]
			if !ok {
				e = &aggEntry{
					groupLabel:  label,
					vendorID:    l.vendorID,
					vendorCode:  l.vendorCode,
					vendorName:  l.vendorName,
					modelCode:   l.modelCode,
					productName: l.productName,
				}
				e.row.GroupLabel = label
				aggMap[key] = e
				order = append(order, key)
			}
			per := purchaseSummaryRow{
				PurchaseQty: l.qty,
				NetAmount:   l.amount,
				TaxAmount:   l.taxAmount,
			}
			per.TotalAmount = per.NetAmount + per.TaxAmount
			accumulatePurchaseRow(&e.row, &per)
		}
		if groupBy == "vendor" {
			sort.Slice(order, func(i, j int) bool {
				return aggMap[order[i]].vendorCode < aggMap[order[j]].vendorCode
			})
		} else {
			sort.Slice(order, func(i, j int) bool {
				return ModelCodeNaturalLess(order[i], order[j])
			})
		}
		for _, k := range order {
			e := aggMap[k]
			if groupBy == "vendor" {
				e.row.VendorID = e.vendorID
				e.row.VendorCode = e.vendorCode
				e.row.VendorName = e.vendorName
			} else {
				e.row.ModelCode = e.modelCode
				e.row.ProductName = e.productName
			}
			rows = append(rows, e.row)
			accumulatePurchaseRow(&footer, &e.row)
		}
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"rows":   rows,
		"footer": footer,
	}).Send()
}

func accumulatePurchaseRow(dst, src *purchaseSummaryRow) {
	dst.PurchaseQty += src.PurchaseQty
	dst.NetAmount += src.NetAmount
	dst.TaxAmount += src.TaxAmount
	dst.TotalAmount += src.TotalAmount
}

func emptyPurchaseSummaryFooter() purchaseSummaryRow {
	return purchaseSummaryRow{GroupLabel: "合計"}
}
