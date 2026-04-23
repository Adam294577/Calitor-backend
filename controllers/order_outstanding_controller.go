package controllers

import (
	"fmt"
	"math"
	"project/models"
	response "project/services/responses"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// orderOutstandingRow 訂貨未交明細列（detail 級，統計由前端 groupBy 聚合）
type orderOutstandingRow struct {
	OrderID       int64          `json:"order_id"`
	OrderNo       string         `json:"order_no"`
	OrderDate     string         `json:"order_date"`
	CustomerCode  string         `json:"customer_code"`
	CustomerName  string         `json:"customer_name"`
	ModelCode     string         `json:"model_code"`
	BrandName     string         `json:"brand_name"`
	ExpectedDate  string         `json:"expected_date"`
	SizeGroupCode string         `json:"size_group_code"`
	Sizes         map[string]int `json:"sizes"`
	TotalQty      int            `json:"total_qty"`
	TotalAmount   float64        `json:"total_amount"`
}

// outstandingSizeGroup 尺碼組 metadata（含完整 options）
type outstandingSizeGroup struct {
	Code    string                    `json:"code"`
	Name    string                    `json:"name"`
	Options []outstandingSizeGroupOpt `json:"options"`
}
type outstandingSizeGroupOpt struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// GetOrderOutstanding 訂貨未交明細（統計由前端自行聚合）
func GetOrderOutstanding(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	customerIDs := c.QueryArray("customer_id")
	modelCodes := c.QueryArray("model_code")
	brandIDStrs := c.QueryArray("brand_id")
	expectedFrom := c.Query("expected_from")
	expectedTo := c.Query("expected_to")

	var cids []int64
	for _, s := range customerIDs {
		if cid, err := strconv.ParseInt(s, 10, 64); err == nil {
			cids = append(cids, cid)
		}
	}
	var brandIDs []int64
	for _, s := range brandIDStrs {
		if bid, err := strconv.ParseInt(s, 10, 64); err == nil {
			brandIDs = append(brandIDs, bid)
		}
	}

	// 1. 所有 size_groups（供前端 header/position 對照）
	type sgOptRow struct {
		SgCode    string `gorm:"column:sg_code"`
		SgName    string `gorm:"column:sg_name"`
		OptID     int64  `gorm:"column:opt_id"`
		OptLabel  string `gorm:"column:opt_label"`
		SortOrder int    `gorm:"column:sort_order"`
	}
	var sgOptRows []sgOptRow
	db.GetRead().Raw(`
		SELECT sg.code as sg_code, sg.name as sg_name,
		       so.id as opt_id, so.label as opt_label, so.sort_order
		FROM size_groups sg
		JOIN size_options so ON so.size_group_id = sg.id
		WHERE sg.deleted_at IS NULL
		ORDER BY sg.code, so.sort_order
	`).Scan(&sgOptRows)

	type sizeGroupInfo struct {
		Code    string
		Name    string
		Options []outstandingSizeGroupOpt
	}
	allSizeGroupMap := map[string]*sizeGroupInfo{}
	sizeOptionToPos := map[int64]int{}
	for _, r := range sgOptRows {
		sg, exists := allSizeGroupMap[r.SgCode]
		if !exists {
			sg = &sizeGroupInfo{Code: r.SgCode, Name: r.SgName}
			allSizeGroupMap[r.SgCode] = sg
		}
		pos := len(sg.Options) + 1
		sg.Options = append(sg.Options, outstandingSizeGroupOpt{
			ID: r.OptID, Label: r.OptLabel, SortOrder: r.SortOrder,
		})
		sizeOptionToPos[r.OptID] = pos
	}

	sgCodes := make([]string, 0, len(allSizeGroupMap))
	for code := range allSizeGroupMap {
		sgCodes = append(sgCodes, code)
	}
	sort.Strings(sgCodes)
	sizeGroups := make([]outstandingSizeGroup, 0, len(allSizeGroupMap))
	maxColumns := 0
	for _, code := range sgCodes {
		sg := allSizeGroupMap[code]
		sizeGroups = append(sizeGroups, outstandingSizeGroup{
			Code: sg.Code, Name: sg.Name, Options: sg.Options,
		})
		if len(sg.Options) > maxColumns {
			maxColumns = len(sg.Options)
		}
	}

	// 2. SQL 下推所有 filter 條件，取得符合的 order_item IDs
	filterQuery := db.GetRead().Table("order_items").
		Select("order_items.id").
		Joins("JOIN orders ON orders.id = order_items.order_id AND orders.deleted_at IS NULL").
		Joins("JOIN retail_customers ON retail_customers.id = orders.customer_id AND retail_customers.is_visible = true").
		Joins("LEFT JOIN products ON products.id = order_items.product_id").
		Where("orders.delivery_status < 2").
		Where("order_items.cancel_flag NOT IN (2, 3)")

	if dateFrom != "" {
		filterQuery = filterQuery.Where("orders.order_date >= ?", dateFrom)
	}
	if dateTo != "" {
		filterQuery = filterQuery.Where("orders.order_date <= ?", dateTo)
	}
	if len(cids) > 0 {
		filterQuery = filterQuery.Where("orders.customer_id IN ?", cids)
	}
	if len(brandIDs) > 0 {
		filterQuery = filterQuery.Where("products.brand_id IN ?", brandIDs)
	}
	if len(modelCodes) > 0 {
		filterQuery = filterQuery.Where("products.model_code IN ?", modelCodes)
	}
	if expectedFrom != "" {
		filterQuery = filterQuery.Where("order_items.expected_date >= ?", expectedFrom)
	}
	if expectedTo != "" {
		filterQuery = filterQuery.Where("order_items.expected_date <= ?", expectedTo)
	}

	var matchedItemIDs []int64
	filterQuery.Scan(&matchedItemIDs)

	if len(matchedItemIDs) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"size_groups": sizeGroups,
			"max_columns": maxColumns,
			"rows":        []orderOutstandingRow{},
		}).Send()
		return
	}

	// 3. 載入符合的 order_items + 關聯
	var items []models.OrderItem
	db.GetRead().
		Where("id IN ?", matchedItemIDs).
		Preload("Sizes").
		Preload("Product.Brand").
		Preload("SizeGroup").
		Find(&items)

	// 4. 載入對應的 orders + customers
	orderIDSet := map[int64]bool{}
	for _, it := range items {
		orderIDSet[it.OrderID] = true
	}
	orderIDs := make([]int64, 0, len(orderIDSet))
	for id := range orderIDSet {
		orderIDs = append(orderIDs, id)
	}
	var orders []models.Order
	db.GetRead().Where("id IN ?", orderIDs).Preload("Customer").Find(&orders)
	orderByID := map[int64]*models.Order{}
	for i := range orders {
		orderByID[orders[i].ID] = &orders[i]
	}

	// 5. 已出貨量
	shipped := ShippedQtyMap(db.GetRead(), matchedItemIDs)

	// 6. 組 detail rows（同時記錄排序用的 product.created_on）
	type rowWithMeta struct {
		row       orderOutstandingRow
		createdOn *time.Time
	}
	sortable := make([]rowWithMeta, 0, len(items))
	for _, item := range items {
		order, ok := orderByID[item.OrderID]
		if !ok {
			continue
		}

		modelCode := ""
		brandName := ""
		var productCreatedOn *time.Time
		if item.Product != nil {
			modelCode = item.Product.ModelCode
			productCreatedOn = item.Product.CreatedOn
			if item.Product.Brand != nil {
				brandName = item.Product.Brand.Name
			}
		}
		sizeGroupCode := ""
		if item.SizeGroup != nil {
			sizeGroupCode = item.SizeGroup.Code
		}
		customerCode := ""
		customerName := ""
		if order.Customer != nil {
			customerCode = order.Customer.Code
			customerName = order.Customer.ShortName
			if customerName == "" {
				customerName = order.Customer.Name
			}
		}

		sizes := map[string]int{}
		totalQty := 0
		for _, sz := range item.Sizes {
			key := fmt.Sprintf("%d-%d", item.ID, sz.SizeOptionID)
			outstanding := sz.Qty - shipped[key]
			if outstanding <= 0 {
				continue
			}
			pos := sizeOptionToPos[sz.SizeOptionID]
			if pos == 0 {
				continue
			}
			sizes[strconv.Itoa(pos)] = outstanding
			totalQty += outstanding
		}
		if totalQty == 0 {
			continue
		}

		sortable = append(sortable, rowWithMeta{
			row: orderOutstandingRow{
				OrderID:       item.OrderID,
				OrderNo:       order.OrderNo,
				OrderDate:     order.OrderDate,
				CustomerCode:  customerCode,
				CustomerName:  customerName,
				ModelCode:     modelCode,
				BrandName:     brandName,
				ExpectedDate:  item.ExpectedDate,
				SizeGroupCode: sizeGroupCode,
				Sizes:         sizes,
				TotalQty:      totalQty,
				TotalAmount:   math.Round(float64(totalQty) * item.OrderPrice),
			},
			createdOn: productCreatedOn,
		})
	}

	// 7. 排序：對帳品牌 asc → 商品型號建檔日 asc（nil 排最後），同鍵以型號穩定化
	sort.SliceStable(sortable, func(i, j int) bool {
		bi, bj := sortable[i].row.BrandName, sortable[j].row.BrandName
		if bi != bj {
			return bi < bj
		}
		ci, cj := sortable[i].createdOn, sortable[j].createdOn
		switch {
		case ci != nil && cj == nil:
			return true
		case ci == nil && cj != nil:
			return false
		case ci != nil && cj != nil && !ci.Equal(*cj):
			return ci.Before(*cj)
		}
		return sortable[i].row.ModelCode < sortable[j].row.ModelCode
	})

	rows := make([]orderOutstandingRow, 0, len(sortable))
	for _, s := range sortable {
		rows = append(rows, s.row)
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"size_groups": sizeGroups,
		"max_columns": maxColumns,
		"rows":        rows,
	}).Send()
}
