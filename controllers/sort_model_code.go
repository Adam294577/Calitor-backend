package controllers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
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

// BuildModelCodeRangeWhere 產生 model_code 區間查詢的 WHERE 片段 (lex 字典序、case-insensitive)。
//   from + to 都有 → [from, to]
//   只有 from      → 開放上界 (UPPER(col) >= UPPER(from))
//   只有 to        → 開放下界 (UPPER(col) <= UPPER(to))
//   兩個都空       → 回傳 ("", nil),caller 自行略過
//
// 注意:過濾用 lex,排序仍用 ModelCodeOrderBy 自然序。兩者刻意分離。
//
// 用法 (raw SQL):
//   if frag, fargs := BuildModelCodeRangeWhere("p.model_code", from, to); frag != "" {
//       where += " AND " + frag
//       args = append(args, fargs...)
//   }
//
// 用法 (GORM):
//   if frag, fargs := BuildModelCodeRangeWhere("products.model_code", from, to); frag != "" {
//       q = q.Where(frag, fargs...)
//   }
func BuildModelCodeRangeWhere(col, from, to string) (string, []interface{}) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" && to == "" {
		return "", nil
	}
	conds := make([]string, 0, 2)
	args := make([]interface{}, 0, 2)
	if from != "" {
		conds = append(conds, fmt.Sprintf("UPPER(%s) >= UPPER(?)", col))
		args = append(args, from)
	}
	if to != "" {
		conds = append(conds, fmt.Sprintf("UPPER(%s) <= UPPER(?)", col))
		args = append(args, to)
	}
	return strings.Join(conds, " AND "), args
}

// MatchModelCodeRange 應用層判斷 code 是否落在 [from, to] 區間內,規則同 BuildModelCodeRangeWhere。
// 兩個都空 → 視為「不過濾」回傳 true。
func MatchModelCodeRange(code, from, to string) bool {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == "" && to == "" {
		return true
	}
	u := strings.ToUpper(code)
	if from != "" && u < strings.ToUpper(from) {
		return false
	}
	if to != "" && u > strings.ToUpper(to) {
		return false
	}
	return true
}
