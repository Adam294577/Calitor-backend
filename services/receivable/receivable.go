// Package receivable 集中應收帳款相關的 SQL 片段與金額計算。
//
// 口徑：所有涉及應收 / 已收 / 折讓 / 其他扣額 / 未收的聚合都採「per-shipment ROUND 後再加總」，
// 以處理舊系統 DealAmount 有小數但 ChargeAmount 已化整造成的尾數差，
// 讓 receivable-query / aging / 結帳檢查等出口的合計口徑一致。
package receivable

import "math"

// GatherDetailsAggJoin 是以 shipment_id 為 key 的 gather_details 聚合 LEFT JOIN 子句，
// 輸出 alias `gd_agg`，提供欄位 total_allowance / total_other。
// 使用時外層 shipments 需 alias 為 `s`。
const GatherDetailsAggJoin = `LEFT JOIN (
	SELECT gd.shipment_id,
		SUM(gd.discount_amount) AS total_allowance,
		SUM(gd.other_deduct)    AS total_other
	FROM gather_details gd
	JOIN gathers g ON g.id = gd.gather_id AND g.deleted_at IS NULL
	GROUP BY gd.shipment_id
) gd_agg ON gd_agg.shipment_id = s.id`

// OutstandingRoundedExpr 是 per-shipment 未收金額（已 ROUND）SQL 運算式，
// 要求外層 shipments alias 為 `s`，且 JOIN 了 GatherDetailsAggJoin。
const OutstandingRoundedExpr = `ROUND(s.deal_amount) - ROUND(s.charge_amount)
	- ROUND(COALESCE(gd_agg.total_allowance, 0))
	- ROUND(COALESCE(gd_agg.total_other, 0))`

// RoundAmount 金額化整至整數（與 MSSQL 舊系統 ChargeAmount 對齊）。
func RoundAmount(v float64) float64 {
	return math.Round(v)
}

// Outstanding 以 per-shipment 口徑計算單張單的未收金額（含 ROUND）。
func Outstanding(deal, charge, allowance, other float64) float64 {
	return RoundAmount(deal) - RoundAmount(charge) - RoundAmount(allowance) - RoundAmount(other)
}
