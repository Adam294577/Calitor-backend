package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"project/services/log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/spf13/viper"
)

// Client MinIO 儲存客戶端
type Client struct {
	client    *minio.Client
	bucket    string
	endpoint  string
	useSSL    bool
	available bool
}

// NewClient 建立 MinIO 客戶端（優雅降級，連線失敗不會 panic）
func NewClient() *Client {
	endpoint := viper.GetString("Minio.Endpoint")
	accessKey := viper.GetString("Minio.AccessKey")
	secretKey := viper.GetString("Minio.SecretKey")
	bucket := viper.GetString("Minio.Bucket")
	useSSL := viper.GetBool("Minio.UseSSL")

	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		log.Warn("MinIO 客戶端初始化失敗：%v", err)
		return &Client{available: false}
	}

	// 確認 bucket 存在，不存在則建立
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exists, err := minioClient.BucketExists(ctx, bucket)
	if err != nil {
		log.Warn("MinIO 連線失敗：%v", err)
		return &Client{available: false}
	}
	if !exists {
		if err := minioClient.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			log.Warn("MinIO 建立 bucket 失敗：%v", err)
			return &Client{available: false}
		}
		log.Info("MinIO bucket「%s」已建立", bucket)
	}

	return &Client{
		client:    minioClient,
		bucket:    bucket,
		endpoint:  endpoint,
		useSSL:    useSSL,
		available: true,
	}
}

// IsAvailable 檢查 MinIO 是否可用
func (c *Client) IsAvailable() bool {
	return c.available
}

// Upload 上傳檔案，回傳公開存取 URL
// objectName 為儲存路徑（如 "images/avatar/123.png"）
// data 為檔案內容，contentType 為 MIME 類型（如 "image/png"）
func (c *Client) Upload(objectName string, data []byte, contentType string) (string, error) {
	if !c.available {
		return "", fmt.Errorf("MinIO 服務未啟用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reader := bytes.NewReader(data)
	_, err := c.client.PutObject(ctx, c.bucket, objectName, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("上傳失敗：%w", err)
	}

	return c.GetURL(objectName), nil
}

// UploadFromReader 從 io.Reader 上傳檔案（適用於大檔案串流）
func (c *Client) UploadFromReader(objectName string, reader io.Reader, size int64, contentType string) (string, error) {
	if !c.available {
		return "", fmt.Errorf("MinIO 服務未啟用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, err := c.client.PutObject(ctx, c.bucket, objectName, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("上傳失敗：%w", err)
	}

	return c.GetURL(objectName), nil
}

// Download 下載檔案，回傳檔案內容
func (c *Client) Download(objectName string) ([]byte, error) {
	if !c.available {
		return nil, fmt.Errorf("MinIO 服務未啟用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	obj, err := c.client.GetObject(ctx, c.bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("下載失敗：%w", err)
	}
	defer obj.Close()

	return io.ReadAll(obj)
}

// Delete 刪除檔案
func (c *Client) Delete(objectName string) error {
	if !c.available {
		return fmt.Errorf("MinIO 服務未啟用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return c.client.RemoveObject(ctx, c.bucket, objectName, minio.RemoveObjectOptions{})
}

// GetPresignedURL 取得帶簽章的臨時存取 URL（適用於私有檔案）
func (c *Client) GetPresignedURL(objectName string, expiry time.Duration) (string, error) {
	if !c.available {
		return "", fmt.Errorf("MinIO 服務未啟用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	presignedURL, err := c.client.PresignedGetObject(ctx, c.bucket, objectName, expiry, url.Values{})
	if err != nil {
		return "", fmt.Errorf("產生簽章 URL 失敗：%w", err)
	}

	return presignedURL.String(), nil
}

// GetURL 取得檔案的公開存取 URL
func (c *Client) GetURL(objectName string) string {
	scheme := "http"
	if c.useSSL {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s/%s", scheme, c.endpoint, c.bucket, objectName)
}

// UploadBase64Image 上傳 Base64 編碼的圖片
// base64Data 可包含 data URL 前綴（如 "data:image/png;base64,..."）
// dir 為儲存目錄（如 "images/avatar"）
// filename 為檔名（不含副檔名）
func (c *Client) UploadBase64Image(base64Data string, dir string, filename string) (string, error) {
	if !c.available {
		return "", fmt.Errorf("MinIO 服務未啟用")
	}

	// 偵測圖片格式
	format := detectImageFormat(base64Data)
	contentType := "image/" + format

	// 清除 data URL 前綴並解碼
	cleaned := cleanBase64Data(base64Data)
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return "", fmt.Errorf("Base64 解碼失敗：%w", err)
	}

	objectName := path.Join(dir, filename+"."+format)
	return c.Upload(objectName, decoded, contentType)
}

// cleanBase64Data 清除 Base64 data URL 前綴
func cleanBase64Data(data string) string {
	if idx := strings.Index(data, ","); idx != -1 {
		return data[idx+1:]
	}
	return data
}

// detectImageFormat 從 Base64 資料偵測圖片格式
func detectImageFormat(base64Data string) string {
	if strings.Contains(base64Data, "image/png") {
		return "png"
	}
	if strings.Contains(base64Data, "image/gif") {
		return "gif"
	}
	if strings.Contains(base64Data, "image/webp") {
		return "webp"
	}
	if strings.Contains(base64Data, "image/svg") {
		return "svg"
	}
	return "jpeg"
}
