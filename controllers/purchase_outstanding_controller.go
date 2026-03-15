package controllers

import (
	"fmt"
	"project/models"
	"project/services/delivery"
	response "project/services/responses"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
)

// outstandingRow 未交統計列
type outstandingRow struct {
	GroupLabel    string         `json:"group_label"`
	SubLabel      string         `json:"sub_label,omitempty"`
	SizeGroupCode string         `json:"size_group_code"`
	Sizes         map[string]int `json:"sizes"`
	TotalQty      int            `json:"total_qty"`
	TotalAmount   float64        `json:"total_amount"`
}

type outstandingSizeCol struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// GetPurchaseOutstanding 採購未交統計
func GetPurchaseOutstanding(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	tab := c.DefaultQuery("tab", "summary")
	groupBy := c.DefaultQuery("group_by", "model")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	vendorIDStr := c.Query("vendor_id")

	// 1. 查 purchases WHERE delivery_status < 2
	query := db.GetRead().Model(&models.Purchase{}).Where("delivery_status < 2")

	if dateFrom != "" {
		query = query.Where("purchase_date >= ?", dateFrom)
	}
	if dateTo != "" {
		query = query.Where("purchase_date <= ?", dateTo)
	}
	if vendorIDStr != "" {
		if vid, err := strconv.ParseInt(vendorIDStr, 10, 64); err == nil {
			query = query.Where("vendor_id = ?", vid)
		}
	}

	var purchases []models.Purchase
	query.Preload("Vendor").
		Preload("Items.Sizes.SizeOption").
		Preload("Items.Product").
		Preload("Items.SizeGroup.Options").
		Find(&purchases)

	if len(purchases) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"size_columns": []outstandingSizeCol{},
			"rows":         []outstandingRow{},
			"footer":       map[string]interface{}{"sizes": map[string]int{}, "total_qty": 0, "total_amount": 0},
		}).Send()
		return
	}

	// 2. 收集所有 PurchaseItem ID
	var allItemIDs []int64
	for _, p := range purchases {
		for _, item := range p.Items {
			allItemIDs = append(allItemIDs, item.ID)
		}
	}

	// 3. 查已進貨量
	delivered := delivery.DeliveredQtyMap(db.GetRead(), allItemIDs)

	// 4. 計算未交量，收集 SizeOption
	type sizeOptionInfo struct {
		ID        int64
		Label     string
		SortOrder int
	}
	sizeOptionMap := map[int64]sizeOptionInfo{}

	// detail rows: 每個 PurchaseItem 一列
	type detailEntry struct {
		purchaseNo    string
		vendorName    string
		modelCode     string
		sizeGroupCode string
		purchasePrice float64
		sizes         map[string]int
		totalQty      int
	}
	var details []detailEntry

	for _, p := range purchases {
		vendorName := ""
		if p.Vendor != nil {
			vendorName = p.Vendor.ShortName
			if vendorName == "" {
				vendorName = p.Vendor.Name
			}
		}

		for _, item := range p.Items {
			modelCode := ""
			if item.Product != nil {
				modelCode = item.Product.ModelCode
			}
			sizeGroupCode := ""
			if item.SizeGroup != nil {
				sizeGroupCode = item.SizeGroup.Code
			}

			// 收集該 item 的 SizeOption 資訊
			if item.SizeGroup != nil {
				for _, opt := range item.SizeGroup.Options {
					if _, exists := sizeOptionMap[opt.ID]; !exists {
						sizeOptionMap[opt.ID] = sizeOptionInfo{
							ID:        opt.ID,
							Label:     opt.Label,
							SortOrder: opt.SortOrder,
						}
					}
				}
			}

			sizes := map[string]int{}
			itemTotalQty := 0

			for _, sz := range item.Sizes {
				key := fmt.Sprintf("%d-%d", item.ID, sz.SizeOptionID)
				deliveredQty := delivered[key]
				outstanding := sz.Qty - deliveredQty
				if outstanding <= 0 {
					continue
				}
				sizeKey := strconv.FormatInt(sz.SizeOptionID, 10)
				sizes[sizeKey] = outstanding
				itemTotalQty += outstanding

				// 確保 sizeOption 資訊被收集
				if sz.SizeOption != nil {
					if _, exists := sizeOptionMap[sz.SizeOptionID]; !exists {
						sizeOptionMap[sz.SizeOptionID] = sizeOptionInfo{
							ID:        sz.SizeOption.ID,
							Label:     sz.SizeOption.Label,
							SortOrder: sz.SizeOption.SortOrder,
						}
					}
				}
			}

			if itemTotalQty == 0 {
				continue
			}

			details = append(details, detailEntry{
				purchaseNo:    p.PurchaseNo,
				vendorName:    vendorName,
				modelCode:     modelCode,
				sizeGroupCode: sizeGroupCode,
				purchasePrice: item.PurchasePrice,
				sizes:         sizes,
				totalQty:      itemTotalQty,
			})
		}
	}

	// 5. 建立 size_columns
	var sizeColumns []outstandingSizeCol
	for _, info := range sizeOptionMap {
		sizeColumns = append(sizeColumns, outstandingSizeCol{
			ID:        info.ID,
			Label:     info.Label,
			SortOrder: info.SortOrder,
		})
	}
	sort.Slice(sizeColumns, func(i, j int) bool {
		if sizeColumns[i].SortOrder != sizeColumns[j].SortOrder {
			return sizeColumns[i].SortOrder < sizeColumns[j].SortOrder
		}
		return sizeColumns[i].ID < sizeColumns[j].ID
	})

	// 6. 組裝 rows
	var rows []outstandingRow

	if tab == "detail" {
		// detail 模式：每個 PurchaseItem 獨立一列
		for _, d := range details {
			subLabel := fmt.Sprintf("#%s(%s)", d.vendorName, d.purchaseNo)
			rows = append(rows, outstandingRow{
				GroupLabel:    d.modelCode,
				SubLabel:      subLabel,
				SizeGroupCode: d.sizeGroupCode,
				Sizes:         d.sizes,
				TotalQty:      d.totalQty,
				TotalAmount:   float64(d.totalQty) * d.purchasePrice,
			})
		}
	} else {
		// summary 模式：依 group_by 聚合
		type aggEntry struct {
			groupLabel    string
			sizeGroupCode string
			sizes         map[string]int
			totalQty      int
			totalAmount   float64
		}
		aggMap := map[string]*aggEntry{}
		var aggOrder []string

		for _, d := range details {
			var groupKey string
			switch groupBy {
			case "vendor":
				groupKey = d.vendorName
			default: // model
				groupKey = d.modelCode
			}

			agg, exists := aggMap[groupKey]
			if !exists {
				agg = &aggEntry{
					groupLabel:    groupKey,
					sizeGroupCode: d.sizeGroupCode,
					sizes:         map[string]int{},
				}
				aggMap[groupKey] = agg
				aggOrder = append(aggOrder, groupKey)
			}

			for sizeKey, qty := range d.sizes {
				agg.sizes[sizeKey] += qty
			}
			agg.totalQty += d.totalQty
			agg.totalAmount += float64(d.totalQty) * d.purchasePrice
		}

		for _, key := range aggOrder {
			agg := aggMap[key]
			rows = append(rows, outstandingRow{
				GroupLabel:    agg.groupLabel,
				SizeGroupCode: agg.sizeGroupCode,
				Sizes:         agg.sizes,
				TotalQty:      agg.totalQty,
				TotalAmount:   agg.totalAmount,
			})
		}
	}

	// 7. footer 加總
	footerSizes := map[string]int{}
	footerTotalQty := 0
	footerTotalAmount := 0.0
	for _, row := range rows {
		for sizeKey, qty := range row.Sizes {
			footerSizes[sizeKey] += qty
		}
		footerTotalQty += row.TotalQty
		footerTotalAmount += row.TotalAmount
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"size_columns": sizeColumns,
		"rows":         rows,
		"footer": map[string]interface{}{
			"sizes":        footerSizes,
			"total_qty":    footerTotalQty,
			"total_amount": footerTotalAmount,
		},
	}).Send()
}
