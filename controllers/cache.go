package controllers

import (
	"fmt"
	"project/services/redis"
	"time"

	"github.com/gin-gonic/gin"
)

const listCacheTTL = 10 * time.Minute

// listCacheKey 根據請求路徑與查詢參數產生快取 key
func listCacheKey(c *gin.Context) string {
	return fmt.Sprintf("list:%s?%s", c.Request.URL.Path, c.Request.URL.RawQuery)
}

// cachedResponse 快取的 API 回應結構
type cachedResponse struct {
	Data  interface{} `json:"Data"`
	Total int64       `json:"Total"`
}

// tryListCache 嘗試從 Redis 讀取快取，命中時直接回應並回傳 true
func tryListCache(c *gin.Context) bool {
	rc := redis.Global()
	if !rc.IsAvailable() {
		return false
	}
	var cached cachedResponse
	if err := rc.GetJSON(listCacheKey(c), &cached); err != nil {
		return false
	}
	c.JSON(200, gin.H{
		"Message": "成功",
		"Status":  200,
		"Data":    cached.Data,
		"Total":   cached.Total,
	})
	return true
}

// setListCache 將列表結果寫入快取
func setListCache(c *gin.Context, data interface{}, total int64) {
	rc := redis.Global()
	if !rc.IsAvailable() {
		return
	}
	rc.SetJSON(listCacheKey(c), cachedResponse{Data: data, Total: total}, listCacheTTL)
}

// getListCache 嘗試從 Redis 讀取快取到 dst，命中回 true（不自動回應，讓呼叫端補即時資料後再回）
func getListCache(c *gin.Context, dst interface{}) bool {
	rc := redis.Global()
	if !rc.IsAvailable() {
		return false
	}
	return rc.GetJSON(listCacheKey(c), dst) == nil
}

// setListCacheRaw 把任意資料直接寫入快取（不包 cachedResponse 外殼），給需自行組回應的場景用
func setListCacheRaw(c *gin.Context, data interface{}) {
	rc := redis.Global()
	if !rc.IsAvailable() {
		return
	}
	rc.SetJSON(listCacheKey(c), data, listCacheTTL)
}

// invalidateListCache 清除指定路徑前綴的所有快取
func invalidateListCache(pathPrefixes ...string) {
	rc := redis.Global()
	if !rc.IsAvailable() {
		return
	}
	for _, prefix := range pathPrefixes {
		keys, err := rc.Keys(fmt.Sprintf("list:/api/admin/%s*", prefix))
		if err == nil && len(keys) > 0 {
			rc.Delete(keys...)
		}
	}
}
