package controllers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	pathpkg "path"
	"path/filepath"
	response "project/services/responses"
	"project/services/storage"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// UploadProductImage 上傳商品圖片
func UploadProductImage(c *gin.Context) {
	resp := response.New(c)

	file, err := c.FormFile("file")
	if err != nil {
		resp.Fail(http.StatusBadRequest, "請選擇圖片檔案").Send()
		return
	}

	// 限制檔案大小 10MB（前端會自動壓縮，此為安全上限）
	if file.Size > 10*1024*1024 {
		resp.Fail(http.StatusBadRequest, "檔案大小不可超過 10MB").Send()
		return
	}

	// 副檔名白名單驗證
	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowedExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true,
	}
	if !allowedExts[ext] {
		resp.Fail(http.StatusBadRequest, "僅允許上傳 jpg、jpeg、png、webp、gif 格式圖片").Send()
		return
	}

	minioClient := storage.NewClient()
	if !minioClient.IsAvailable() {
		resp.Fail(http.StatusInternalServerError, "檔案儲存服務未啟用").Send()
		return
	}

	src, err := file.Open()
	if err != nil {
		resp.Fail(http.StatusInternalServerError, "開啟檔案失敗").Send()
		return
	}
	defer src.Close()

	// 讀取前 12 bytes 做 magic number 驗證
	header := make([]byte, 12)
	n, err := io.ReadFull(src, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		resp.Fail(http.StatusBadRequest, "無法讀取檔案內容").Send()
		return
	}
	header = header[:n]

	if !isValidImageMagic(header) {
		resp.Fail(http.StatusBadRequest, "檔案內容不是有效的圖片格式").Send()
		return
	}

	// 將 reader 重置回開頭（重組已讀取的 header + 剩餘內容）
	remaining, _ := io.ReadAll(src)
	fullContent := append(header, remaining...)
	reader := bytes.NewReader(fullContent)

	// 使用 http.DetectContentType 偵測真實 Content-Type（不信任 client header）
	contentType := http.DetectContentType(fullContent)

	now := time.Now()
	objectName := fmt.Sprintf("products/%s/%s-%d%s", now.Format("2006/01"), now.Format("02"), now.UnixNano(), ext)

	_, err = minioClient.UploadFromReader(objectName, reader, int64(len(fullContent)), contentType)
	if err != nil {
		resp.Fail(http.StatusInternalServerError, "上傳失敗: "+err.Error()).Send()
		return
	}

	resp.Success("上傳成功").SetData(gin.H{
		"object_name": objectName,
	}).Send()
}

// DeleteProductImage 刪除商品圖片
func DeleteProductImage(c *gin.Context) {
	resp := response.New(c)

	var req struct {
		ObjectName string `json:"object_name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(http.StatusBadRequest, "請提供 object_name").Send()
		return
	}

	minioClient := storage.NewClient()
	if !minioClient.IsAvailable() {
		resp.Fail(http.StatusInternalServerError, "檔案儲存服務未啟用").Send()
		return
	}

	// 驗證 object_name 必須以 products/ 開頭且不含 ..
	if !strings.HasPrefix(req.ObjectName, "products/") || strings.Contains(req.ObjectName, "..") {
		resp.Fail(http.StatusBadRequest, "無效的檔案路徑").Send()
		return
	}

	if err := minioClient.Delete(req.ObjectName); err != nil {
		resp.Fail(http.StatusInternalServerError, "刪除失敗: "+err.Error()).Send()
		return
	}

	resp.Success("刪除成功").Send()
}

// isValidImageMagic 檢查檔案 magic number 是否為允許的圖片格式
func isValidImageMagic(header []byte) bool {
	if len(header) < 3 {
		return false
	}
	// JPEG: FF D8 FF
	if header[0] == 0xFF && header[1] == 0xD8 && header[2] == 0xFF {
		return true
	}
	// PNG: 89 50 4E 47
	if len(header) >= 4 && header[0] == 0x89 && header[1] == 0x50 && header[2] == 0x4E && header[3] == 0x47 {
		return true
	}
	// GIF: 47 49 46 38
	if len(header) >= 4 && header[0] == 0x47 && header[1] == 0x49 && header[2] == 0x46 && header[3] == 0x38 {
		return true
	}
	// WebP: RIFF....WEBP (bytes 0-3: "RIFF", bytes 8-11: "WEBP")
	if len(header) >= 12 &&
		header[0] == 0x52 && header[1] == 0x49 && header[2] == 0x46 && header[3] == 0x46 &&
		header[8] == 0x57 && header[9] == 0x45 && header[10] == 0x42 && header[11] == 0x50 {
		return true
	}
	return false
}

// ServeFile 代理讀取 MinIO 檔案（圖片等）
func ServeFile(c *gin.Context) {
	objectName := c.Param("path")
	// Gin wildcard 會帶前綴 "/"，去掉
	if len(objectName) > 0 && objectName[0] == '/' {
		objectName = objectName[1:]
	}
	// 路徑清理：防止 path traversal 攻擊
	objectName = pathpkg.Clean(objectName)
	if objectName == "" || objectName == "." || strings.HasPrefix(objectName, "..") || strings.Contains(objectName, "..") {
		c.Status(http.StatusBadRequest)
		return
	}

	minioClient := storage.NewClient()
	if !minioClient.IsAvailable() {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	data, contentType, err := minioClient.DownloadWithInfo(objectName)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	c.Data(http.StatusOK, contentType, data)
}
