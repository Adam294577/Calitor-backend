package cron

import (
	"fmt"
	"project/models"
	"project/services/log"
	"project/services/storage"
	"time"
)

// CleanupOrphanImages 清理 MinIO 中無對應商品的孤兒圖片
func CleanupOrphanImages() {
	log.Info("[Cron] 開始清理孤兒圖片...")

	minioClient := storage.NewClient()
	if !minioClient.IsAvailable() {
		log.Warn("[Cron] MinIO 未啟用，跳過孤兒圖片清理")
		return
	}

	// 列出 MinIO products/ 下所有檔案（含上傳時間）
	allObjects, err := minioClient.ListObjectsWithInfo("products/")
	if err != nil {
		log.Error("[Cron] 列出 MinIO 物件失敗: %s", err.Error())
		return
	}

	if len(allObjects) == 0 {
		log.Info("[Cron] MinIO 無任何商品圖片，跳過")
		return
	}

	// 查出 DB 中所有有圖片的 image_url
	db := models.PostgresNew()
	defer db.Close()
	var usedURLs []string
	db.GetRead().Model(&models.Product{}).
		Where("image_url IS NOT NULL AND image_url != ''").
		Pluck("image_url", &usedURLs)

	usedSet := make(map[string]bool, len(usedURLs))
	for _, u := range usedURLs {
		usedSet[u] = true
	}

	// 比對並刪除孤兒（僅刪除上傳超過 24 小時的，避免競態條件）
	gracePeriod := 24 * time.Hour
	cutoff := time.Now().Add(-gracePeriod)
	deleted := 0
	failed := 0
	skipped := 0
	for _, obj := range allObjects {
		if !usedSet[obj.Key] {
			if obj.LastModified.After(cutoff) {
				skipped++
				continue
			}
			if err := minioClient.Delete(obj.Key); err != nil {
				log.Warn("[Cron] 刪除孤兒圖片失敗 %s: %s", obj.Key, err.Error())
				failed++
			} else {
				deleted++
			}
		}
	}

	log.Info(fmt.Sprintf("[Cron] 孤兒圖片清理完成：MinIO 共 %d 張，DB 使用 %d 張，刪除 %d 張，跳過(未滿24h) %d 張，失敗 %d 張",
		len(allObjects), len(usedURLs), deleted, skipped, failed))
}
