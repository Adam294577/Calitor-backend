package controllers

import (
	"fmt"
	"net/http"
	pathpkg "path"
	"path/filepath"
	"project/services/storage"
	response "project/services/responses"
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

	ext := filepath.Ext(file.Filename)
	now := time.Now()
	objectName := fmt.Sprintf("products/%s/%s-%d%s", now.Format("2006/01"), now.Format("02"), now.UnixNano(), ext)
	contentType := file.Header.Get("Content-Type")

	_, err = minioClient.UploadFromReader(objectName, src, file.Size, contentType)
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
