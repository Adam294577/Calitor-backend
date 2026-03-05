package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"project/services/log"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

// Client Redis 客戶端封裝
type Client struct {
	client    *redis.Client
	ctx       context.Context
	available bool // 標記 Redis 是否可用
}

// NewRedisClient 創建 Redis 客戶端（優雅降級版本）
func NewRedisClient() *Client {
	redisDB := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", viper.GetString("Redis.Host"), viper.GetInt("Redis.Port")),
		Password: viper.GetString("Redis.Password"),
		DB:       viper.GetInt("Redis.Db"),
		PoolSize: viper.GetInt("Redis.PoolSize"), // 連接池大小
	})
	ctx := context.Background()
	// 測試連接，失敗時不 panic
	if _, err := redisDB.Ping(ctx).Result(); err != nil {
		// 記錄警告日誌，確保部署時能看到 Redis 連接失敗
		log.Warn("Redis 連接失敗: %v，將使用優雅降級模式（直接查詢資料庫）", err)
		return &Client{
			client:    nil, // 標記為 nil 表示不可用
			ctx:       ctx,
			available: false,
		}
	}
	// 連接成功，記錄資訊日誌
	log.Info("Redis 連接成功: %s:%d", viper.GetString("Redis.Host"), viper.GetInt("Redis.Port"))

	return &Client{
		client:    redisDB,
		ctx:       ctx,
		available: true,
	}
}

// IsAvailable 檢查 Redis 是否可用
func (c *Client) IsAvailable() bool {
	return c.available && c.client != nil
}

// Close 關閉連接
func (c *Client) Close() error {
	if !c.IsAvailable() {
		return nil
	}
	return c.client.Close()
}

// 基本字串操作

// Set 設置鍵值對
func (c *Client) Set(key string, value interface{}, expiration time.Duration) error {
	if !c.IsAvailable() {
		return errors.New("Redis 不可用")
	}
	return c.client.Set(c.ctx, key, value, expiration).Err()
}

// Get 獲取值
func (c *Client) Get(key string) (string, error) {
	if !c.IsAvailable() {
		return "", errors.New("Redis 不可用")
	}
	return c.client.Get(c.ctx, key).Result()
}

// Delete 刪除鍵
func (c *Client) Delete(keys ...string) error {
	if !c.IsAvailable() {
		return errors.New("Redis 不可用")
	}
	return c.client.Del(c.ctx, keys...).Err()
}

// Exists 檢查鍵是否存在
func (c *Client) Exists(key string) (bool, error) {
	if !c.IsAvailable() {
		return false, errors.New("Redis 不可用")
	}
	result, err := c.client.Exists(c.ctx, key).Result()
	return result > 0, err
}

// Expire 設置過期時間
func (c *Client) Expire(key string, expiration time.Duration) error {
	if !c.IsAvailable() {
		return errors.New("Redis 不可用")
	}
	return c.client.Expire(c.ctx, key, expiration).Err()
}

// JSON 操作

// SetJSON 設置 JSON 數據
func (c *Client) SetJSON(key string, value interface{}, expiration time.Duration) error {
	jsonData, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.Set(key, string(jsonData), expiration)
}

// GetJSON 獲取 JSON 數據
func (c *Client) GetJSON(key string, result interface{}) error {
	if !c.IsAvailable() {
		return errors.New("Redis 不可用")
	}
	data, err := c.Get(key)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(data), result)
}

// 雜湊表操作

// HSet 設置雜湊字段
func (c *Client) HSet(key string, values ...interface{}) error {
	if !c.IsAvailable() {
		return errors.New("Redis 不可用")
	}
	return c.client.HSet(c.ctx, key, values...).Err()
}

// HGet 獲取雜湊字段
func (c *Client) HGet(key, field string) (string, error) {
	if !c.IsAvailable() {
		return "", errors.New("Redis 不可用")
	}
	return c.client.HGet(c.ctx, key, field).Result()
}

// HGetAll 獲取所有雜湊字段
func (c *Client) HGetAll(key string) (map[string]string, error) {
	if !c.IsAvailable() {
		return nil, errors.New("Redis 不可用")
	}
	return c.client.HGetAll(c.ctx, key).Result()
}

// 列表操作

// LPush 左邊推入元素
func (c *Client) LPush(key string, values ...interface{}) error {
	if !c.IsAvailable() {
		return errors.New("Redis 不可用")
	}
	return c.client.LPush(c.ctx, key, values...).Err()
}

// RPop 右邊彈出元素
func (c *Client) RPop(key string) (string, error) {
	if !c.IsAvailable() {
		return "", errors.New("Redis 不可用")
	}
	return c.client.RPop(c.ctx, key).Result()
}

// LRange 獲取列表範圍
func (c *Client) LRange(key string, start, stop int64) ([]string, error) {
	if !c.IsAvailable() {
		return nil, errors.New("Redis 不可用")
	}
	return c.client.LRange(c.ctx, key, start, stop).Result()
}

// 集合操作

// SAdd 添加集合元素
func (c *Client) SAdd(key string, members ...interface{}) error {
	if !c.IsAvailable() {
		return errors.New("Redis 不可用")
	}
	return c.client.SAdd(c.ctx, key, members...).Err()
}

// SIsMember 檢查是否集合成員
func (c *Client) SIsMember(key string, member interface{}) (bool, error) {
	if !c.IsAvailable() {
		return false, errors.New("Redis 不可用")
	}
	return c.client.SIsMember(c.ctx, key, member).Result()
}

// SMembers 獲取所有集合成員
func (c *Client) SMembers(key string) ([]string, error) {
	if !c.IsAvailable() {
		return nil, errors.New("Redis 不可用")
	}
	return c.client.SMembers(c.ctx, key).Result()
}

// 進階功能

// Increment 自增
func (c *Client) Increment(key string) (int64, error) {
	if !c.IsAvailable() {
		return 0, errors.New("Redis 不可用")
	}
	return c.client.Incr(c.ctx, key).Result()
}

// IncrementBy 按指定值自增
func (c *Client) IncrementBy(key string, value int64) (int64, error) {
	if !c.IsAvailable() {
		return 0, errors.New("Redis 不可用")
	}
	return c.client.IncrBy(c.ctx, key, value).Result()
}

// SetNX 設置不存在的鍵（分散式鎖基礎）
func (c *Client) SetNX(key string, value interface{}, expiration time.Duration) (bool, error) {
	if !c.IsAvailable() {
		return false, errors.New("Redis 不可用")
	}
	return c.client.SetNX(c.ctx, key, value, expiration).Result()
}

// TTL 獲取過期時間
func (c *Client) TTL(key string) (time.Duration, error) {
	if !c.IsAvailable() {
		return 0, errors.New("Redis 不可用")
	}
	return c.client.TTL(c.ctx, key).Result()
}

// Keys 根據模式查找鍵
func (c *Client) Keys(pattern string) ([]string, error) {
	if !c.IsAvailable() {
		return nil, errors.New("Redis 不可用")
	}
	return c.client.Keys(c.ctx, pattern).Result()
}

// Pipeline 管道操作（批量執行）
func (c *Client) Pipeline(commands ...func(pipe redis.Pipeliner) error) ([]redis.Cmder, error) {
	if !c.IsAvailable() {
		return nil, errors.New("Redis 不可用")
	}
	pipe := c.client.Pipeline()

	for _, cmd := range commands {
		if err := cmd(pipe); err != nil {
			return nil, err
		}
	}

	return pipe.Exec(c.ctx)
}
