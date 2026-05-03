// Package permission 提供 Controller / Service 層共用的細項權限檢查 helper。
package permission

import (
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"project/middlewares"
)

// MasterCodePermission 是決定是否允許編輯主檔 code / account_no / model_code 等欄位的權限 key。
const MasterCodePermission = "edit-master-code"

// StripMasterCodeFields 在當前使用者沒有 MasterCodePermission 時，
// 將 req 中指定 jsonFields 重置為 zero value（map 則 delete key）。
//
// 下游既有的 zero-value filter（`if req.F != "" { updates[...] = req.F }`、
// gorm 對 pointer nil 自動略過、map missing key 自動略過）自然達成「保留原值」效果。
//
// 參數：
//   - c          : gin.Context，用來讀權限
//   - req        : 支援 *struct（一般 JSON bind 結果）或 map[string]interface{}（rawReq）
//   - jsonFields : 受保護欄位的 JSON tag 名稱（snake_case），例如 "code"、"account_no"、"model_code"
//
// 回傳：是否實際發生剝離。
//   - 有權限時回 false
//   - 沒權限但所有欄位本來就是 zero / 不在 map 中 → 回 false
//   - 否則回 true（供 caller 做 audit log）
func StripMasterCodeFields(c *gin.Context, req any, jsonFields ...string) bool {
	if middlewares.HasPermission(c, MasterCodePermission) {
		return false
	}
	if req == nil || len(jsonFields) == 0 {
		return false
	}

	if m, ok := req.(map[string]interface{}); ok {
		stripped := false
		for _, f := range jsonFields {
			if _, exists := m[f]; exists {
				delete(m, f)
				stripped = true
			}
		}
		return stripped
	}

	v := reflect.ValueOf(req)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return false
	}
	elem := v.Elem()
	if elem.Kind() != reflect.Struct {
		return false
	}

	idx := jsonFieldIndex(elem.Type())
	stripped := false
	for _, f := range jsonFields {
		i, ok := idx[f]
		if !ok {
			continue
		}
		fv := elem.Field(i)
		if !fv.CanSet() {
			continue
		}
		zero := reflect.Zero(fv.Type())
		if fv.Kind() == reflect.Ptr {
			if fv.IsNil() {
				continue
			}
		} else if fv.Comparable() && fv.Interface() == zero.Interface() {
			continue
		}
		fv.Set(zero)
		stripped = true
	}
	return stripped
}

var jsonTagCache sync.Map // map[reflect.Type]map[string]int

// jsonFieldIndex 建立 struct type 的 jsonTag → fieldIndex 對照表，並以 Type 為 key 快取。
// 使用 JSON tag 第一段作為 key；沒有 tag 或 tag == "-" 的欄位忽略。
func jsonFieldIndex(t reflect.Type) map[string]int {
	if cached, ok := jsonTagCache.Load(t); ok {
		return cached.(map[string]int)
	}
	out := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" || name == "-" {
			continue
		}
		out[name] = i
	}
	jsonTagCache.Store(t, out)
	return out
}
