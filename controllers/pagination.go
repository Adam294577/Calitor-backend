package controllers

import (
	"project/services/log"
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

	// Count: override Select 為 "*"，避免 query 帶有 Select("table.*") 時 GORM 自動產生
	// SELECT COUNT(table.*) 在 PostgreSQL + JOIN 結合下回 0 的問題。
	var total int64
	if err := query.Session(&gorm.Session{}).Select("*").Model(model).Count(&total).Error; err != nil {
		log.Error("[Paginate] count error: %v", err)
	}

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
