package library

import (
	"math/rand"
	"sort"
)

func RandString(n int) string {
	var letters = []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// SortByAsc 对泛型Map的Key升序排列，并返回排列后有序Map Key切片
func SortByAsc(params map[string]any) map[string]any {
	resp := make(map[string]any, len(params))

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	// 使用 sort.Strings 替代 sort.Sort(sort.StringSlice(...)) 以满足 lint 要求
	sort.Strings(keys)
	for _, k := range keys {
		resp[k] = params[k]
	}
	return resp
}

// SortByDesc 对泛型Map的Key降序排列，并返回排列后有序Map Key切片
func SortByDesc(params map[string]interface{}) map[string]interface{} {
	resp := make(map[string]interface{}, len(params))

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	for _, k := range keys {
		resp[k] = params[k]
	}
	return resp
}
