package middlewares

import (
	"fmt"
	"net/http"
	"project/services/log"
	"project/services/redis"
	response "project/services/responses"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	loginRateLimitMax = 5               // 最大失敗次數
	loginRateLimitTTL = 5 * time.Minute // 鎖定時間窗口
)

// LoginRateLimit 登入頻率限制（Redis-based）
// 每個 IP 在 5 分鐘內最多允許 5 次失敗登入，超過後回 429
func LoginRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		rds := redis.Global()
		if !rds.IsAvailable() {
			// Redis 不可用時放行，不阻擋登入
			c.Next()
			return
		}

		ip := c.ClientIP()
		key := fmt.Sprintf("login_limit:%s", ip)

		// 檢查目前失敗次數
		countStr, err := rds.Get(key)
		if err == nil {
			var count int
			fmt.Sscanf(countStr, "%d", &count)
			if count >= loginRateLimitMax {
				ttl, _ := rds.TTL(key)
				remaining := int(ttl.Seconds())
				if remaining < 0 {
					remaining = 0
				}
				log.Warn("LoginRateLimit: IP %s 已被鎖定，剩餘 %d 秒", ip, remaining)
				resp := response.New(c)
				resp.Fail(http.StatusTooManyRequests,
					fmt.Sprintf("登入嘗試過多，請 %d 秒後再試", remaining)).Send()
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// LoginRateLimitIncr 登入失敗時呼叫，遞增計數
func LoginRateLimitIncr(ip string) {
	rds := redis.Global()
	if !rds.IsAvailable() {
		return
	}
	key := fmt.Sprintf("login_limit:%s", ip)
	count, err := rds.Increment(key)
	if err != nil {
		log.Error("LoginRateLimit INCR 失敗: %s", err.Error())
		return
	}
	// 第一次失敗時設定 TTL
	if count == 1 {
		rds.Expire(key, loginRateLimitTTL)
	}
}

// LoginRateLimitReset 登入成功時呼叫，重置計數
func LoginRateLimitReset(ip string) {
	rds := redis.Global()
	if !rds.IsAvailable() {
		return
	}
	key := fmt.Sprintf("login_limit:%s", ip)
	rds.Delete(key)
}
