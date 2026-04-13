package controllers

import (
	"fmt"
	"project/models"
	response "project/services/responses"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// inventoryRow 庫存明細列
type inventoryRow struct {
	ProductID         int64              `json:"product_id"`
	ModelCode         string             `json:"model_code"`
	CustomerID        int64              `json:"customer_id"`
	CustomerShortName string             `json:"customer_short_name"`
	SizeGroupID       int64              `json:"size_group_id"`
	SizeGroupCode     string             `json:"size_group_code"`
	CostStart         float64            `json:"cost_start"`
	TotalQty          int                `json:"total_qty"`
	Amount            float64            `json:"amount"`
	MSRP              float64            `json:"msrp"`
	CreatedOn         string             `json:"created_on"`
	Sizes             map[string]int     `json:"sizes"`
	SizeOptions       []inventorySizeCol `json:"size_options"`
}

type inventorySizeCol struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// inventoryRawRow raw query 結果
type inventoryRawRow struct {
	ProductID         int64   `gorm:"column:product_id"`
	ModelCode         string  `gorm:"column:model_code"`
	CustomerID        int64   `gorm:"column:customer_id"`
	CustomerShortName string  `gorm:"column:customer_short_name"`
	SizeGroupID       int64   `gorm:"column:size_group_id"`
	SizeGroupCode     string  `gorm:"column:size_group_code"`
	CostStart         float64 `gorm:"column:cost_start"`
	MSRP              float64 `gorm:"column:msrp"`
	CreatedOn         string  `gorm:"column:created_on"`
	SizeOptionID      int64   `gorm:"column:size_option_id"`
	SizeLabel         string  `gorm:"column:size_label"`
	SortOrder         int     `gorm:"column:sort_order"`
	Qty               int     `gorm:"column:qty"`
}

// GetInventory 庫存明細查詢
func GetInventory(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	// 組建 WHERE 條件
	where := "WHERE s.deleted_at IS NULL AND p.deleted_at IS NULL"
	args := []interface{}{}

	if v := c.Query("customer_id"); v != "" {
		where += " AND s.customer_id = ?"
		args = append(args, v)
	}
	if v := c.Query("model_codes"); v != "" {
		codes := strings.Split(v, ",")
		where += " AND p.model_code IN (" + placeholders(len(codes)) + ")"
		for _, code := range codes {
			args = append(args, strings.TrimSpace(code))
		}
	}
	if v := c.Query("brand_ids"); v != "" {
		ids := strings.Split(v, ",")
		where += " AND p.product_brand_id IN (" + placeholders(len(ids)) + ")"
		for _, id := range ids {
			args = append(args, strings.TrimSpace(id))
		}
	}
	if v := c.Query("vendor_ids"); v != "" {
		ids := strings.Split(v, ",")
		where += " AND p.id IN (SELECT pv2.product_id FROM product_vendors pv2 WHERE pv2.vendor_id IN (" + placeholders(len(ids)) + "))"
		for _, id := range ids {
			args = append(args, strings.TrimSpace(id))
		}
	}
	if v := c.Query("created_from"); v != "" {
		where += " AND TO_CHAR(p.created_on, 'YYYYMMDD') >= ?"
		args = append(args, v)
	}
	if v := c.Query("created_to"); v != "" {
		where += " AND TO_CHAR(p.created_on, 'YYYYMMDD') <= ?"
		args = append(args, v)
	}
	for i := 1; i <= 5; i++ {
		key := fmt.Sprintf("category%d_id", i)
		if v := c.Query(key); v != "" {
			col := fmt.Sprintf("category%d_id", i)
			where += fmt.Sprintf(" AND p.id IN (SELECT pcm.product_id FROM product_category_map pcm WHERE pcm.category_type = %d AND pcm.%s = ?)", i, col)
			args = append(args, v)
		}
	}

	// 改用 product_size_stocks 表（進貨加、出貨扣 都已即時更新）
	where = strings.Replace(where, "s.deleted_at IS NULL AND ", "", 1)
	where = strings.Replace(where, "s.customer_id", "pss.customer_id", -1)

	sql := fmt.Sprintf(`
SELECT
  pss.product_id,
  p.model_code,
  pss.customer_id,
  COALESCE(NULLIF(rc.short_name, ''), rc.name, '') as customer_short_name,
  COALESCE(sg.id, 0) as size_group_id,
  COALESCE(sg.code, '') as size_group_code,
  COALESCE(pv.cost_start, 0) as cost_start,
  COALESCE(p.msrp, 0) as msrp,
  COALESCE(TO_CHAR(p.created_on, 'YYYYMMDD'), '') as created_on,
  pss.size_option_id,
  so.label as size_label,
  so.sort_order,
  pss.qty
FROM product_size_stocks pss
JOIN products p ON p.id = pss.product_id
JOIN size_options so ON so.id = pss.size_option_id
LEFT JOIN size_groups sg ON sg.id = p.size1_group_id
LEFT JOIN product_vendors pv ON pv.product_id = p.id AND pv.is_primary = true
LEFT JOIN retail_customers rc ON rc.id = pss.customer_id
LEFT JOIN product_brands pb ON pb.id = p.product_brand_id
%s AND pss.qty != 0
ORDER BY p.model_code, pss.customer_id, so.sort_order
`, where)

	var rawRows []inventoryRawRow
	if err := db.GetRead().Raw(sql, args...).Scan(&rawRows).Error; err != nil {
		resp.Panic(err).Send()
		return
	}

	if len(rawRows) == 0 {
		resp.Success("成功").SetData(map[string]interface{}{
			"rows": []inventoryRow{},
		}).Send()
		return
	}

	// 聚合：key = product_id + customer_id
	type aggKey struct {
		ProductID  int64
		CustomerID int64
	}
	aggMap := map[aggKey]*inventoryRow{}
	var aggOrder []aggKey

	// 收集用到的 size_group_id
	sizeGroupIDs := map[int64]bool{}

	for _, raw := range rawRows {
		key := aggKey{raw.ProductID, raw.CustomerID}
		row, exists := aggMap[key]
		if !exists {
			row = &inventoryRow{
				ProductID:         raw.ProductID,
				ModelCode:         raw.ModelCode,
				CustomerID:        raw.CustomerID,
				CustomerShortName: raw.CustomerShortName,
				SizeGroupID:       raw.SizeGroupID,
				SizeGroupCode:     raw.SizeGroupCode,
				CostStart:         raw.CostStart,
				MSRP:              raw.MSRP,
				CreatedOn:         raw.CreatedOn,
				Sizes:             map[string]int{},
			}
			aggMap[key] = row
			aggOrder = append(aggOrder, key)
		}

		sizeKey := strconv.FormatInt(raw.SizeOptionID, 10)
		row.Sizes[sizeKey] += raw.Qty
		row.TotalQty += raw.Qty

		if raw.SizeGroupID > 0 {
			sizeGroupIDs[raw.SizeGroupID] = true
		}
	}

	// 查出所有用到的 size group 的完整 options
	sizeGroupOptionsMap := map[int64][]inventorySizeCol{}
	if len(sizeGroupIDs) > 0 {
		ids := make([]int64, 0, len(sizeGroupIDs))
		for id := range sizeGroupIDs {
			ids = append(ids, id)
		}

		var options []models.SizeOption
		db.GetRead().Where("size_group_id IN ?", ids).Order("sort_order ASC, id ASC").Find(&options)

		for _, o := range options {
			sizeGroupOptionsMap[o.SizeGroupID] = append(sizeGroupOptionsMap[o.SizeGroupID], inventorySizeCol{
				ID:        o.ID,
				Label:     o.Label,
				SortOrder: o.SortOrder,
			})
		}
	}

	// 組裝 rows，填入完整 size_options
	rows := make([]inventoryRow, 0, len(aggOrder))
	for _, key := range aggOrder {
		row := aggMap[key]
		row.Amount = row.CostStart * float64(row.TotalQty)
		row.SizeOptions = sizeGroupOptionsMap[row.SizeGroupID]
		if row.SizeOptions == nil {
			row.SizeOptions = []inventorySizeCol{}
		}
		rows = append(rows, *row)
	}

	resp.Success("成功").SetData(map[string]interface{}{
		"rows": rows,
	}).SetTotal(int64(len(rows))).Send()
}

// placeholders 產生 n 個 "?" 逗號分隔，供 IN 子句使用
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
