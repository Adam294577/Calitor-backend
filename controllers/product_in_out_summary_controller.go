package controllers

import (
	"fmt"
	"project/models"
	response "project/services/responses"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// productSummaryRow 商品列表（Tab 1：商品型號）
type productSummaryRow struct {
	ID            int64  `json:"id" gorm:"column:id"`
	ModelCode     string `json:"model_code" gorm:"column:model_code"`
	NameSpec      string `json:"name_spec" gorm:"column:name_spec"`
	VendorCode    string `json:"vendor_code" gorm:"column:vendor_code"`
	VendorName    string `json:"vendor_name" gorm:"column:vendor_name"`
	CreatedOn     string `json:"created_on" gorm:"column:created_on"`
	SizeGroupID   int64  `json:"size_group_id" gorm:"column:size_group_id"`
	SizeGroupCode string `json:"size_group_code" gorm:"column:size_group_code"`
}

// detailRow 進出明細列（Tab 2：商品進出明細）
type detailRow struct {
	Kind         string         `json:"kind"`
	KindLabel    string         `json:"kind_label"`
	DocNo        string         `json:"doc_no"`
	BranchCode   string         `json:"branch_code"`
	BranchName   string         `json:"branch_name"`
	Sizes        map[string]int `json:"sizes"`
	TotalQty     int            `json:"total_qty"`
	UnitPrice    float64        `json:"unit_price"`
	VendorName   string         `json:"vendor_name"`
	DocDate      string         `json:"doc_date"`
	ModifiedDate string         `json:"modified_date"`
	ModifiedBy   string         `json:"modified_by"`
}

type sizeColumn struct {
	ID        int64  `json:"id"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

// GetProductInOutSummaryProducts Tab 1：依過濾條件列出商品（預設只列 is_visible=true）
func GetProductInOutSummaryProducts(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	where := "WHERE p.deleted_at IS NULL AND p.is_visible = true"
	args := []interface{}{}

	if frag, fargs := BuildModelCodeRangeWhere("p.model_code", c.Query("model_code_from"), c.Query("model_code_to")); frag != "" {
		where += " AND " + frag
		args = append(args, fargs...)
	}
	if v := c.Query("brand_ids"); v != "" {
		ids := splitNonEmpty(v)
		if len(ids) > 0 {
			where += " AND p.brand_id IN (" + placeholders(len(ids)) + ")"
			for _, id := range ids {
				args = append(args, id)
			}
		}
	}
	if v := c.Query("name_spec"); v != "" {
		where += " AND p.name_spec ILIKE ?"
		args = append(args, "%"+v+"%")
	}
	if v := c.Query("vendor_id"); v != "" {
		where += " AND p.id IN (SELECT pv2.product_id FROM product_vendors pv2 WHERE pv2.vendor_id = ?)"
		args = append(args, v)
	}
	if v := c.Query("size_group_code"); v != "" {
		where += " AND sg.code = ?"
		args = append(args, v)
	}
	if v := c.Query("created_on"); v != "" {
		where += " AND TO_CHAR(p.created_on, 'YYYYMMDD') = ?"
		args = append(args, v)
	}
	if v := c.Query("created_on_from"); v != "" {
		where += " AND TO_CHAR(p.created_on, 'YYYYMMDD') >= ?"
		args = append(args, v)
	}
	if v := c.Query("created_on_to"); v != "" {
		where += " AND TO_CHAR(p.created_on, 'YYYYMMDD') <= ?"
		args = append(args, v)
	}

	// 異動範圍過濾:只列在 [date_from, date_to] 內、且符合所選 kinds 至少一種異動的商品
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")
	kinds := splitNonEmpty(c.Query("kinds"))
	if dateFrom != "" || dateTo != "" {
		if len(kinds) == 0 {
			// 前端必傳 kinds(filterKinds.length 永不為 0),後端 fallback 帶全套保險
			kinds = []string{"stock", "shipment", "retail_sell", "modify", "transfer_in", "transfer_out", "order", "purchase"}
		}
		// 每種 kind 對應的 (header table, item table, header 業務日期欄)
		kindMap := map[string][3]string{
			"stock":        {"stocks", "stock_items", "stock_date"},
			"shipment":     {"shipments", "shipment_items", "shipment_date"},
			"retail_sell":  {"retail_sells", "retail_sell_items", "sell_date"},
			"modify":       {"modifies", "modify_items", "modify_date"},
			"transfer_in":  {"transfers", "transfer_items", "transfer_date"},
			"transfer_out": {"transfers", "transfer_items", "transfer_date"},
			"order":        {"orders", "order_items", "order_date"},
			"purchase":     {"purchases", "purchase_items", "purchase_date"},
		}
		// item table 的 FK 欄位
		fkMap := map[string]string{
			"stocks":       "stock_id",
			"shipments":    "shipment_id",
			"retail_sells": "retail_sell_id",
			"modifies":     "modify_id",
			"transfers":    "transfer_id",
			"orders":       "order_id",
			"purchases":    "purchase_id",
		}
		// transfer_in / transfer_out 在 transfer_items 沒有方向欄,
		// 商品列表階段「有 transfer 即可」,兩個方向都映射到同一張 transfers 查一次。
		// 真正的方向 (調入/調出) 在 Tab 2 (商品進出明細) 才會展開。
		seen := map[string]bool{}
		exists := []string{}
		for _, k := range kinds {
			cfg, ok := kindMap[k]
			if !ok {
				continue
			}
			key := cfg[0] + "|" + cfg[1]
			if seen[key] {
				continue
			}
			seen[key] = true
			header, item, dateCol := cfg[0], cfg[1], cfg[2]
			fk := fkMap[header]
			// 規範 #1 雙路徑:業務日期 OR ChangeDate(updated_at)。
			// Sell/Stock/Purchase/Orders/Ship/Goods/Modify/Transfer 都可能事後改動,
			// 只比對業務日期會漏抓「日期被改成範圍外、但確實在此範圍內被異動過」的單。
			bizParts := []string{}
			chgParts := []string{}
			localArgs := []interface{}{}
			chgArgs := []interface{}{}
			if dateFrom != "" {
				bizParts = append(bizParts, fmt.Sprintf("xh.%s >= ?", dateCol))
				localArgs = append(localArgs, dateFrom)
				chgParts = append(chgParts, "TO_CHAR(xh.updated_at, 'YYYYMMDD') >= ?")
				chgArgs = append(chgArgs, dateFrom)
			}
			if dateTo != "" {
				bizParts = append(bizParts, fmt.Sprintf("xh.%s <= ?", dateCol))
				localArgs = append(localArgs, dateTo)
				chgParts = append(chgParts, "TO_CHAR(xh.updated_at, 'YYYYMMDD') <= ?")
				chgArgs = append(chgArgs, dateTo)
			}
			cond := fmt.Sprintf(
				"EXISTS (SELECT 1 FROM %[2]s xi JOIN %[1]s xh ON xh.id = xi.%[3]s AND xh.deleted_at IS NULL WHERE xi.product_id = p.id AND ((%[4]s) OR (%[5]s)))",
				header, item, fk,
				strings.Join(bizParts, " AND "),
				strings.Join(chgParts, " AND "),
			)
			args = append(args, localArgs...)
			args = append(args, chgArgs...)
			exists = append(exists, cond)
		}
		if len(exists) > 0 {
			where += " AND (" + strings.Join(exists, " OR ") + ")"
		}
	}

	sql := fmt.Sprintf(`
SELECT
  p.id,
  p.model_code,
  COALESCE(p.name_spec, '') AS name_spec,
  COALESCE(v.code, '') AS vendor_code,
  COALESCE(NULLIF(v.short_name, ''), v.name, '') AS vendor_name,
  COALESCE(TO_CHAR(p.created_on, 'YYYYMMDD'), '') AS created_on,
  COALESCE(sg.id, 0) AS size_group_id,
  COALESCE(sg.code, '') AS size_group_code
FROM products p
LEFT JOIN size_groups sg ON sg.id = p.size1_group_id
LEFT JOIN product_vendors pv ON pv.product_id = p.id AND pv.is_primary = true
LEFT JOIN vendors v ON v.id = pv.vendor_id
%s
ORDER BY %s
`, where, ModelCodeOrderBy("p.model_code"))

	var rows []productSummaryRow
	if err := db.GetRead().Raw(sql, args...).Scan(&rows).Error; err != nil {
		resp.Panic(err).Send()
		return
	}

	resp.Success("查詢成功").SetData(rows).Send()
}

// GetProductInOutSummaryDetail Tab 2：取單一商品的進出明細
// 依 kinds 動態組合 UNION，從 stocks/shipments/retail_sells/modifies/transfers/orders/purchases 彙總
func GetProductInOutSummaryDetail(c *gin.Context) {
	resp := response.New(c)
	db := models.PostgresNew()
	defer db.Close()

	productID := c.Query("product_id")
	if productID == "" {
		resp.Fail(400, "缺少 product_id").Send()
		return
	}

	// 取得商品 size group 以決定欄位順序
	var sizeGroupID int64
	if err := db.GetRead().Raw(
		"SELECT COALESCE(size1_group_id, 0) FROM products WHERE id = ? AND deleted_at IS NULL",
		productID,
	).Scan(&sizeGroupID).Error; err != nil {
		resp.Panic(err).Send()
		return
	}

	// 取得 size options（用於決定欄位顯示順序）
	var sizeCols []sizeColumn
	if sizeGroupID > 0 {
		var options []models.SizeOption
		db.GetRead().Where("size_group_id = ?", sizeGroupID).
			Order("sort_order ASC, id ASC").Find(&options)
		for _, o := range options {
			sizeCols = append(sizeCols, sizeColumn{ID: o.ID, Label: o.Label, SortOrder: o.SortOrder})
		}
	}
	if sizeCols == nil {
		sizeCols = []sizeColumn{}
	}

	// 過濾條件
	branchCodes := splitNonEmpty(c.Query("branch_codes"))
	branchIDs := splitNonEmpty(c.Query("branch_ids"))
	txDateFrom := c.Query("tx_date_from")
	txDateTo := c.Query("tx_date_to")
	// 計算當日庫存數量未勾＝排除當日：將 txDateTo 上限改為昨天
	if c.Query("exclude_today") == "1" {
		loc, _ := time.LoadLocation("Asia/Taipei")
		yesterday := time.Now().In(loc).AddDate(0, 0, -1).Format("20060102")
		if txDateTo == "" || txDateTo > yesterday {
			txDateTo = yesterday
		}
	}
	kinds := splitNonEmpty(c.Query("kinds"))
	if len(kinds) == 0 {
		// 預設全部
		kinds = []string{"stock", "shipment", "retail_sell", "modify", "transfer_in", "transfer_out", "order", "purchase"}
	}
	kindSet := map[string]bool{}
	for _, k := range kinds {
		kindSet[k] = true
	}

	// 將 branch_ids 轉成 branch_codes（部分異動單以 branch_code 字串記錄分店）
	if len(branchIDs) > 0 {
		var codes []string
		db.GetRead().Raw(
			"SELECT DISTINCT branch_code FROM retail_customers WHERE id IN ? AND branch_code <> ''",
			branchIDs,
		).Scan(&codes)
		branchCodes = append(branchCodes, codes...)
	}

	allRows := []detailRow{}

	if kindSet["stock"] {
		rows, err := queryStockRows(db, productID, branchIDs, txDateFrom, txDateTo)
		if err != nil {
			resp.Panic(err).Send()
			return
		}
		allRows = append(allRows, rows...)
	}
	if kindSet["shipment"] {
		rows, err := queryShipmentRows(db, productID, branchCodes, branchIDs, txDateFrom, txDateTo)
		if err != nil {
			resp.Panic(err).Send()
			return
		}
		allRows = append(allRows, rows...)
	}
	if kindSet["retail_sell"] {
		rows, err := queryRetailSellRows(db, productID, branchCodes, branchIDs, txDateFrom, txDateTo)
		if err != nil {
			resp.Panic(err).Send()
			return
		}
		allRows = append(allRows, rows...)
	}
	if kindSet["modify"] {
		rows, err := queryModifyRows(db, productID, branchCodes, branchIDs, txDateFrom, txDateTo)
		if err != nil {
			resp.Panic(err).Send()
			return
		}
		allRows = append(allRows, rows...)
	}
	if kindSet["transfer_out"] {
		rows, err := queryTransferOutRows(db, productID, branchCodes, branchIDs, txDateFrom, txDateTo)
		if err != nil {
			resp.Panic(err).Send()
			return
		}
		allRows = append(allRows, rows...)
	}
	if kindSet["transfer_in"] {
		rows, err := queryTransferInRows(db, productID, branchCodes, branchIDs, txDateFrom, txDateTo)
		if err != nil {
			resp.Panic(err).Send()
			return
		}
		allRows = append(allRows, rows...)
	}
	if kindSet["order"] {
		rows, err := queryOrderRows(db, productID, branchCodes, branchIDs, txDateFrom, txDateTo)
		if err != nil {
			resp.Panic(err).Send()
			return
		}
		allRows = append(allRows, rows...)
	}
	if kindSet["purchase"] {
		rows, err := queryPurchaseRows(db, productID, branchIDs, txDateFrom, txDateTo)
		if err != nil {
			resp.Panic(err).Send()
			return
		}
		allRows = append(allRows, rows...)
	}

	// 依 kind 排序順序：進貨 → 出貨 → 銷貨 → 調整 → 調出 → 調入 → 訂貨 → 採購
	kindOrder := map[string]int{
		"stock": 1, "shipment": 2, "retail_sell": 3, "modify": 4,
		"transfer_out": 5, "transfer_in": 6, "order": 7, "purchase": 8,
	}
	sort.SliceStable(allRows, func(i, j int) bool {
		ki := kindOrder[allRows[i].Kind]
		kj := kindOrder[allRows[j].Kind]
		if ki != kj {
			return ki < kj
		}
		if allRows[i].DocDate != allRows[j].DocDate {
			return allRows[i].DocDate < allRows[j].DocDate
		}
		return allRows[i].DocNo < allRows[j].DocNo
	})

	resp.Success("查詢成功").SetData(map[string]interface{}{
		"rows":         allRows,
		"size_columns": sizeCols,
	}).Send()
}

// ========== 各 kind 的查詢函式 ==========

type rawSizeRow struct {
	HeaderID     int64   `gorm:"column:header_id"`
	DocNo        string  `gorm:"column:doc_no"`
	DocDate      string  `gorm:"column:doc_date"`
	BranchCode   string  `gorm:"column:branch_code"`
	BranchName   string  `gorm:"column:branch_name"`
	UnitPrice    float64 `gorm:"column:unit_price"`
	VendorName   string  `gorm:"column:vendor_name"`
	UpdatedAt    string  `gorm:"column:updated_at"`
	ModifiedBy   string  `gorm:"column:modified_by"`
	SizeOptionID int64   `gorm:"column:size_option_id"`
	Qty          int     `gorm:"column:qty"`
}

func aggregateBySizes(raws []rawSizeRow, kind, kindLabel string) []detailRow {
	type aggKey = int64
	bucket := map[aggKey]*detailRow{}
	order := []aggKey{}
	for _, r := range raws {
		dr, exists := bucket[r.HeaderID]
		if !exists {
			dr = &detailRow{
				Kind:         kind,
				KindLabel:    kindLabel,
				DocNo:        r.DocNo,
				BranchCode:   r.BranchCode,
				BranchName:   r.BranchName,
				Sizes:        map[string]int{},
				UnitPrice:    r.UnitPrice,
				VendorName:   r.VendorName,
				DocDate:      r.DocDate,
				ModifiedDate: r.UpdatedAt,
				ModifiedBy:   r.ModifiedBy,
			}
			bucket[r.HeaderID] = dr
			order = append(order, r.HeaderID)
		}
		if r.SizeOptionID > 0 {
			key := strconv.FormatInt(r.SizeOptionID, 10)
			dr.Sizes[key] += r.Qty
			dr.TotalQty += r.Qty
		}
	}
	rows := make([]detailRow, 0, len(order))
	for _, k := range order {
		rows = append(rows, *bucket[k])
	}
	return rows
}

func queryStockRows(db *models.DBManager, productID string, branchIDs []string, dateFrom, dateTo string) ([]detailRow, error) {
	where := "WHERE s.deleted_at IS NULL AND si.product_id = ?"
	args := []interface{}{productID}
	if len(branchIDs) > 0 {
		where += " AND s.customer_id IN (" + placeholders(len(branchIDs)) + ")"
		for _, id := range branchIDs {
			args = append(args, id)
		}
	}
	if dateFrom != "" {
		where += " AND s.stock_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		where += " AND s.stock_date <= ?"
		args = append(args, dateTo)
	}

	sql := fmt.Sprintf(`
SELECT
  s.id AS header_id,
  s.stock_no AS doc_no,
  s.stock_date AS doc_date,
  COALESCE(rc.branch_code, '') AS branch_code,
  COALESCE(NULLIF(rc.short_name, ''), rc.name, '') AS branch_name,
  COALESCE(si.purchase_price, 0) AS unit_price,
  COALESCE(NULLIF(v.short_name, ''), v.name, '') AS vendor_name,
  TO_CHAR(s.updated_at AT TIME ZONE 'Asia/Taipei', 'YYYYMMDD') AS updated_at,
  COALESCE(a.account, '') AS modified_by,
  COALESCE(sis.size_option_id, 0) AS size_option_id,
  COALESCE(sis.qty, 0) AS qty
FROM stocks s
JOIN stock_items si ON si.stock_id = s.id
LEFT JOIN stock_item_sizes sis ON sis.stock_item_id = si.id
LEFT JOIN retail_customers rc ON rc.id = s.customer_id
LEFT JOIN vendors v ON v.id = s.vendor_id
LEFT JOIN admins a ON a.id = s.recorder_id
%s
ORDER BY s.stock_date, s.id
`, where)

	var raws []rawSizeRow
	if err := db.GetRead().Raw(sql, args...).Scan(&raws).Error; err != nil {
		return nil, err
	}
	return aggregateBySizes(raws, "stock", "進貨"), nil
}

func queryShipmentRows(db *models.DBManager, productID string, branchCodes, branchIDs []string, dateFrom, dateTo string) ([]detailRow, error) {
	where := "WHERE s.deleted_at IS NULL AND si.product_id = ?"
	args := []interface{}{productID}
	if len(branchCodes) > 0 || len(branchIDs) > 0 {
		conds := []string{}
		if len(branchCodes) > 0 {
			conds = append(conds, "s.ship_store IN ("+placeholders(len(branchCodes))+")")
			for _, c := range branchCodes {
				args = append(args, c)
			}
		}
		if len(branchIDs) > 0 {
			conds = append(conds, "s.customer_id IN ("+placeholders(len(branchIDs))+")")
			for _, id := range branchIDs {
				args = append(args, id)
			}
		}
		where += " AND (" + strings.Join(conds, " OR ") + ")"
	}
	if dateFrom != "" {
		where += " AND s.shipment_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		where += " AND s.shipment_date <= ?"
		args = append(args, dateTo)
	}

	sql := fmt.Sprintf(`
SELECT
  s.id AS header_id,
  s.shipment_no AS doc_no,
  s.shipment_date AS doc_date,
  COALESCE(s.ship_store, '') AS branch_code,
  COALESCE(NULLIF(branch.short_name, ''), branch.name, '') AS branch_name,
  COALESCE(si.ship_price, 0) AS unit_price,
  COALESCE(NULLIF(rc.short_name, ''), rc.name, '') AS vendor_name,
  TO_CHAR(s.updated_at AT TIME ZONE 'Asia/Taipei', 'YYYYMMDD') AS updated_at,
  COALESCE(a.account, '') AS modified_by,
  COALESCE(sis.size_option_id, 0) AS size_option_id,
  COALESCE(sis.qty, 0) AS qty
FROM shipments s
JOIN shipment_items si ON si.shipment_id = s.id
LEFT JOIN shipment_item_sizes sis ON sis.shipment_item_id = si.id
LEFT JOIN LATERAL (
  SELECT short_name, name FROM retail_customers
  WHERE branch_code = s.ship_store AND deleted_at IS NULL LIMIT 1
) branch ON TRUE
LEFT JOIN retail_customers rc ON rc.id = s.customer_id
LEFT JOIN admins a ON a.id = s.recorder_id
%s
ORDER BY s.shipment_date, s.id
`, where)

	var raws []rawSizeRow
	if err := db.GetRead().Raw(sql, args...).Scan(&raws).Error; err != nil {
		return nil, err
	}
	return aggregateBySizes(raws, "shipment", "出貨"), nil
}

func queryRetailSellRows(db *models.DBManager, productID string, branchCodes, branchIDs []string, dateFrom, dateTo string) ([]detailRow, error) {
	where := "WHERE s.deleted_at IS NULL AND si.product_id = ?"
	args := []interface{}{productID}
	if len(branchCodes) > 0 || len(branchIDs) > 0 {
		conds := []string{}
		if len(branchCodes) > 0 {
			conds = append(conds, "s.sell_store IN ("+placeholders(len(branchCodes))+")")
			for _, c := range branchCodes {
				args = append(args, c)
			}
		}
		if len(branchIDs) > 0 {
			conds = append(conds, "s.customer_id IN ("+placeholders(len(branchIDs))+")")
			for _, id := range branchIDs {
				args = append(args, id)
			}
		}
		where += " AND (" + strings.Join(conds, " OR ") + ")"
	}
	if dateFrom != "" {
		where += " AND s.sell_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		where += " AND s.sell_date <= ?"
		args = append(args, dateTo)
	}

	sql := fmt.Sprintf(`
SELECT
  s.id AS header_id,
  s.sell_no AS doc_no,
  s.sell_date AS doc_date,
  COALESCE(s.sell_store, '') AS branch_code,
  COALESCE(NULLIF(branch.short_name, ''), branch.name, '') AS branch_name,
  COALESCE(si.sell_price, 0) AS unit_price,
  COALESCE(NULLIF(rc.short_name, ''), rc.name, '') AS vendor_name,
  TO_CHAR(s.updated_at AT TIME ZONE 'Asia/Taipei', 'YYYYMMDD') AS updated_at,
  COALESCE(a.account, '') AS modified_by,
  COALESCE(sis.size_option_id, 0) AS size_option_id,
  COALESCE(sis.qty, 0) AS qty
FROM retail_sells s
JOIN retail_sell_items si ON si.retail_sell_id = s.id
LEFT JOIN retail_sell_item_sizes sis ON sis.retail_sell_item_id = si.id
LEFT JOIN LATERAL (
  SELECT short_name, name FROM retail_customers
  WHERE branch_code = s.sell_store AND deleted_at IS NULL LIMIT 1
) branch ON TRUE
LEFT JOIN retail_customers rc ON rc.id = s.customer_id
LEFT JOIN admins a ON a.id = s.recorder_id
%s
ORDER BY s.sell_date, s.id
`, where)

	var raws []rawSizeRow
	if err := db.GetRead().Raw(sql, args...).Scan(&raws).Error; err != nil {
		return nil, err
	}
	return aggregateBySizes(raws, "retail_sell", "銷貨"), nil
}

func queryModifyRows(db *models.DBManager, productID string, branchCodes, branchIDs []string, dateFrom, dateTo string) ([]detailRow, error) {
	where := "WHERE m.deleted_at IS NULL AND mi.product_id = ?"
	args := []interface{}{productID}
	if len(branchCodes) > 0 || len(branchIDs) > 0 {
		conds := []string{}
		if len(branchCodes) > 0 {
			conds = append(conds, "m.modify_store IN ("+placeholders(len(branchCodes))+")")
			for _, c := range branchCodes {
				args = append(args, c)
			}
		}
		if len(branchIDs) > 0 {
			conds = append(conds, "m.customer_id IN ("+placeholders(len(branchIDs))+")")
			for _, id := range branchIDs {
				args = append(args, id)
			}
		}
		where += " AND (" + strings.Join(conds, " OR ") + ")"
	}
	if dateFrom != "" {
		where += " AND m.modify_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		where += " AND m.modify_date <= ?"
		args = append(args, dateTo)
	}

	sql := fmt.Sprintf(`
SELECT
  m.id AS header_id,
  m.modify_no AS doc_no,
  m.modify_date AS doc_date,
  COALESCE(m.modify_store, '') AS branch_code,
  COALESCE(NULLIF(branch.short_name, ''), branch.name, '') AS branch_name,
  0 AS unit_price,
  COALESCE(NULLIF(rc.short_name, ''), rc.name, '') AS vendor_name,
  TO_CHAR(m.updated_at AT TIME ZONE 'Asia/Taipei', 'YYYYMMDD') AS updated_at,
  COALESCE(a.account, '') AS modified_by,
  COALESCE(mis.size_option_id, 0) AS size_option_id,
  COALESCE(mis.qty, 0) AS qty
FROM modifies m
JOIN modify_items mi ON mi.modify_id = m.id
LEFT JOIN modify_item_sizes mis ON mis.modify_item_id = mi.id
LEFT JOIN LATERAL (
  SELECT short_name, name FROM retail_customers
  WHERE branch_code = m.modify_store AND deleted_at IS NULL LIMIT 1
) branch ON TRUE
LEFT JOIN retail_customers rc ON rc.id = m.customer_id
LEFT JOIN admins a ON a.id = m.recorder_id
%s
ORDER BY m.modify_date, m.id
`, where)

	var raws []rawSizeRow
	if err := db.GetRead().Raw(sql, args...).Scan(&raws).Error; err != nil {
		return nil, err
	}
	return aggregateBySizes(raws, "modify", "調整"), nil
}

func queryTransferOutRows(db *models.DBManager, productID string, branchCodes, branchIDs []string, dateFrom, dateTo string) ([]detailRow, error) {
	where := "WHERE t.deleted_at IS NULL AND ti.product_id = ?"
	args := []interface{}{productID}
	if len(branchCodes) > 0 || len(branchIDs) > 0 {
		conds := []string{}
		if len(branchCodes) > 0 {
			conds = append(conds, "t.source_store IN ("+placeholders(len(branchCodes))+")")
			for _, c := range branchCodes {
				args = append(args, c)
			}
		}
		if len(branchIDs) > 0 {
			conds = append(conds, "t.source_customer_id IN ("+placeholders(len(branchIDs))+")")
			for _, id := range branchIDs {
				args = append(args, id)
			}
		}
		where += " AND (" + strings.Join(conds, " OR ") + ")"
	}
	if dateFrom != "" {
		where += " AND t.transfer_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		where += " AND t.transfer_date <= ?"
		args = append(args, dateTo)
	}

	// 調出：以 transfer_item 為單位（單筆 transfer 可能有不同 dest）
	sql := fmt.Sprintf(`
SELECT
  ti.id AS header_id,
  t.transfer_no AS doc_no,
  t.transfer_date AS doc_date,
  COALESCE(t.source_store, '') AS branch_code,
  COALESCE(NULLIF(branch.short_name, ''), branch.name, '') AS branch_name,
  COALESCE(ti.unit_price, 0) AS unit_price,
  COALESCE(NULLIF(dest.short_name, ''), dest.name, '') AS vendor_name,
  TO_CHAR(t.updated_at AT TIME ZONE 'Asia/Taipei', 'YYYYMMDD') AS updated_at,
  COALESCE(a.account, '') AS modified_by,
  COALESCE(tis.size_option_id, 0) AS size_option_id,
  COALESCE(tis.qty, 0) AS qty
FROM transfers t
JOIN transfer_items ti ON ti.transfer_id = t.id
LEFT JOIN transfer_item_sizes tis ON tis.transfer_item_id = ti.id
LEFT JOIN LATERAL (
  SELECT short_name, name FROM retail_customers
  WHERE branch_code = t.source_store AND deleted_at IS NULL LIMIT 1
) branch ON TRUE
LEFT JOIN LATERAL (
  SELECT short_name, name FROM retail_customers
  WHERE branch_code = ti.dest_store AND deleted_at IS NULL LIMIT 1
) dest ON TRUE
LEFT JOIN admins a ON a.id = t.recorder_id
%s
ORDER BY t.transfer_date, ti.id
`, where)

	var raws []rawSizeRow
	if err := db.GetRead().Raw(sql, args...).Scan(&raws).Error; err != nil {
		return nil, err
	}
	return aggregateBySizes(raws, "transfer_out", "調出"), nil
}

func queryTransferInRows(db *models.DBManager, productID string, branchCodes, branchIDs []string, dateFrom, dateTo string) ([]detailRow, error) {
	where := "WHERE t.deleted_at IS NULL AND ti.product_id = ?"
	args := []interface{}{productID}
	if len(branchCodes) > 0 || len(branchIDs) > 0 {
		conds := []string{}
		if len(branchCodes) > 0 {
			conds = append(conds, "ti.dest_store IN ("+placeholders(len(branchCodes))+")")
			for _, c := range branchCodes {
				args = append(args, c)
			}
		}
		if len(branchIDs) > 0 {
			conds = append(conds, "ti.dest_customer_id IN ("+placeholders(len(branchIDs))+")")
			for _, id := range branchIDs {
				args = append(args, id)
			}
		}
		where += " AND (" + strings.Join(conds, " OR ") + ")"
	}
	if dateFrom != "" {
		where += " AND t.transfer_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		where += " AND t.transfer_date <= ?"
		args = append(args, dateTo)
	}

	// 調入：以 transfer_item 為單位（每筆 item 可能有不同 dest）
	sql := fmt.Sprintf(`
SELECT
  ti.id AS header_id,
  t.transfer_no AS doc_no,
  t.transfer_date AS doc_date,
  COALESCE(ti.dest_store, '') AS branch_code,
  COALESCE(NULLIF(branch.short_name, ''), branch.name, '') AS branch_name,
  COALESCE(ti.unit_price, 0) AS unit_price,
  COALESCE(NULLIF(src.short_name, ''), src.name, '') AS vendor_name,
  TO_CHAR(t.updated_at AT TIME ZONE 'Asia/Taipei', 'YYYYMMDD') AS updated_at,
  COALESCE(a.account, '') AS modified_by,
  COALESCE(tis.size_option_id, 0) AS size_option_id,
  COALESCE(tis.qty, 0) AS qty
FROM transfers t
JOIN transfer_items ti ON ti.transfer_id = t.id
LEFT JOIN transfer_item_sizes tis ON tis.transfer_item_id = ti.id
LEFT JOIN LATERAL (
  SELECT short_name, name FROM retail_customers
  WHERE branch_code = ti.dest_store AND deleted_at IS NULL LIMIT 1
) branch ON TRUE
LEFT JOIN LATERAL (
  SELECT short_name, name FROM retail_customers
  WHERE branch_code = t.source_store AND deleted_at IS NULL LIMIT 1
) src ON TRUE
LEFT JOIN admins a ON a.id = t.recorder_id
%s
ORDER BY t.transfer_date, ti.id
`, where)

	var raws []rawSizeRow
	if err := db.GetRead().Raw(sql, args...).Scan(&raws).Error; err != nil {
		return nil, err
	}
	return aggregateBySizes(raws, "transfer_in", "調入"), nil
}

func queryOrderRows(db *models.DBManager, productID string, branchCodes, branchIDs []string, dateFrom, dateTo string) ([]detailRow, error) {
	where := "WHERE o.deleted_at IS NULL AND oi.product_id = ?"
	args := []interface{}{productID}
	if len(branchCodes) > 0 || len(branchIDs) > 0 {
		conds := []string{}
		if len(branchCodes) > 0 {
			conds = append(conds, "o.order_store IN ("+placeholders(len(branchCodes))+")")
			for _, c := range branchCodes {
				args = append(args, c)
			}
		}
		if len(branchIDs) > 0 {
			conds = append(conds, "o.customer_id IN ("+placeholders(len(branchIDs))+")")
			for _, id := range branchIDs {
				args = append(args, id)
			}
		}
		where += " AND (" + strings.Join(conds, " OR ") + ")"
	}
	if dateFrom != "" {
		where += " AND o.order_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		where += " AND o.order_date <= ?"
		args = append(args, dateTo)
	}

	sql := fmt.Sprintf(`
SELECT
  o.id AS header_id,
  o.order_no AS doc_no,
  o.order_date AS doc_date,
  COALESCE(o.order_store, '') AS branch_code,
  COALESCE(NULLIF(branch.short_name, ''), branch.name, '') AS branch_name,
  COALESCE(oi.order_price, 0) AS unit_price,
  COALESCE(NULLIF(rc.short_name, ''), rc.name, '') AS vendor_name,
  TO_CHAR(o.updated_at AT TIME ZONE 'Asia/Taipei', 'YYYYMMDD') AS updated_at,
  COALESCE(a.account, '') AS modified_by,
  COALESCE(ois.size_option_id, 0) AS size_option_id,
  COALESCE(ois.qty, 0) AS qty
FROM orders o
JOIN order_items oi ON oi.order_id = o.id
LEFT JOIN order_item_sizes ois ON ois.order_item_id = oi.id
LEFT JOIN LATERAL (
  SELECT short_name, name FROM retail_customers
  WHERE branch_code = o.order_store AND deleted_at IS NULL LIMIT 1
) branch ON TRUE
LEFT JOIN retail_customers rc ON rc.id = o.customer_id
LEFT JOIN admins a ON a.id = o.recorder_id
%s
ORDER BY o.order_date, o.id
`, where)

	var raws []rawSizeRow
	if err := db.GetRead().Raw(sql, args...).Scan(&raws).Error; err != nil {
		return nil, err
	}
	return aggregateBySizes(raws, "order", "訂貨"), nil
}

func queryPurchaseRows(db *models.DBManager, productID string, branchIDs []string, dateFrom, dateTo string) ([]detailRow, error) {
	where := "WHERE p.deleted_at IS NULL AND pi.product_id = ?"
	args := []interface{}{productID}
	if len(branchIDs) > 0 {
		where += " AND p.customer_id IN (" + placeholders(len(branchIDs)) + ")"
		for _, id := range branchIDs {
			args = append(args, id)
		}
	}
	if dateFrom != "" {
		where += " AND p.purchase_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		where += " AND p.purchase_date <= ?"
		args = append(args, dateTo)
	}

	sql := fmt.Sprintf(`
SELECT
  p.id AS header_id,
  p.purchase_no AS doc_no,
  p.purchase_date AS doc_date,
  COALESCE(rc.branch_code, '') AS branch_code,
  COALESCE(NULLIF(rc.short_name, ''), rc.name, '') AS branch_name,
  COALESCE(pi.purchase_price, 0) AS unit_price,
  COALESCE(NULLIF(v.short_name, ''), v.name, '') AS vendor_name,
  TO_CHAR(p.updated_at AT TIME ZONE 'Asia/Taipei', 'YYYYMMDD') AS updated_at,
  COALESCE(a.account, '') AS modified_by,
  COALESCE(pis.size_option_id, 0) AS size_option_id,
  COALESCE(pis.qty, 0) AS qty
FROM purchases p
JOIN purchase_items pi ON pi.purchase_id = p.id
LEFT JOIN purchase_item_sizes pis ON pis.purchase_item_id = pi.id
LEFT JOIN retail_customers rc ON rc.id = p.customer_id
LEFT JOIN vendors v ON v.id = p.vendor_id
LEFT JOIN admins a ON a.id = p.recorder_id
%s
ORDER BY p.purchase_date, p.id
`, where)

	var raws []rawSizeRow
	if err := db.GetRead().Raw(sql, args...).Scan(&raws).Error; err != nil {
		return nil, err
	}
	return aggregateBySizes(raws, "purchase", "採購"), nil
}

// splitNonEmpty 拆分逗號分隔字串，去掉空字串
func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
