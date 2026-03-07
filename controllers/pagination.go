package controllers

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const defaultPageSize = 100
const maxPageSize = 1000

// Paginate 從 query params 解析分頁參數，回傳套用分頁的 query 與總筆數
func Paginate(c *gin.Context, query *gorm.DB, model interface{}) (*gorm.DB, int64) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(defaultPageSize)))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}

	var total int64
	query.Session(&gorm.Session{}).Model(model).Count(&total)

	offset := (page - 1) * pageSize
	return query.Offset(offset).Limit(pageSize), total
}

// ApplySearch 將 ILIKE 搜尋條件套用到 query（PostgreSQL）
func ApplySearch(query *gorm.DB, search string, fields ...string) *gorm.DB {
	if search == "" {
		return query
	}
	like := "%" + search + "%"
	conditions := make([]string, len(fields))
	args := make([]interface{}, len(fields))
	for i, f := range fields {
		conditions[i] = f + " ILIKE ?"
		args[i] = like
	}
	return query.Where(strings.Join(conditions, " OR "), args...)
}
