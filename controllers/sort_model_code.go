package controllers

import (
	"fmt"
	"regexp"
	"strconv"
)

// ModelCodeOrderBy returns the comma-separated ORDER BY keys that sort the
// given column using natural-number ordering. Caller is expected to prepend
// "ORDER BY " (or pass it inside a Gorm .Order(...) call).
//
// 拆解 model_code 為 4 個排序鍵：
//  1. 開頭非數字（例 "GB"、"N"）
//  2. 第一段數字當 int（例 8210、1843）
//  3. 中段非「-」（例 ""、"W"）
//  4. 「-」後尾段數字當 int（例 1、96）
//
// 並用完整字串 ASC 當最終 fallback。
//
// 範例：
//
//	"GB8019-15" < "GB8030-01" < "GB8210-01" < "N1843W-15"
//	"GB8030-01" < "GB8030-15" < "GB8030-49"
func ModelCodeOrderBy(col string) string {
	return fmt.Sprintf(
		`COALESCE(SUBSTRING(%[1]s FROM '^([^0-9]*)'), '') ASC, `+
			`COALESCE(NULLIF(SUBSTRING(%[1]s FROM '^[^0-9]*([0-9]+)'), '')::bigint, 0) ASC, `+
			`COALESCE(SUBSTRING(%[1]s FROM '^[^0-9]*[0-9]+([^-]*)'), '') ASC, `+
			`COALESCE(NULLIF(SUBSTRING(%[1]s FROM '-([0-9]+)$'), '')::bigint, 0) ASC, `+
			`%[1]s ASC`,
		col,
	)
}

var (
	reModelLeading  = regexp.MustCompile(`^([^0-9]*)`)
	reModelFirstNum = regexp.MustCompile(`^[^0-9]*([0-9]+)`)
	reModelMiddle   = regexp.MustCompile(`^[^0-9]*[0-9]+([^-]*)`)
	reModelTailNum  = regexp.MustCompile(`-([0-9]+)$`)
)

// ModelCodeNaturalLess 用於 sort.Slice 的應用層比較器。規則同 ModelCodeOrderBy。
func ModelCodeNaturalLess(a, b string) bool {
	a1, a2, a3, a4 := modelCodeNaturalKey(a)
	b1, b2, b3, b4 := modelCodeNaturalKey(b)
	if a1 != b1 {
		return a1 < b1
	}
	if a2 != b2 {
		return a2 < b2
	}
	if a3 != b3 {
		return a3 < b3
	}
	if a4 != b4 {
		return a4 < b4
	}
	return a < b
}

func modelCodeNaturalKey(s string) (string, int64, string, int64) {
	var k1, k3 string
	var k2, k4 int64
	if m := reModelLeading.FindStringSubmatch(s); m != nil {
		k1 = m[1]
	}
	if m := reModelFirstNum.FindStringSubmatch(s); m != nil {
		if v, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			k2 = v
		}
	}
	if m := reModelMiddle.FindStringSubmatch(s); m != nil {
		k3 = m[1]
	}
	if m := reModelTailNum.FindStringSubmatch(s); m != nil {
		if v, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			k4 = v
		}
	}
	return k1, k2, k3, k4
}
