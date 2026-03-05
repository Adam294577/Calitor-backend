package middlewares

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"path"
	"project/services/common"
	"project/services/log"
	"strings"
	"time"

	response "project/services/responses"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func Middleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// 設定變數
		ctx.Set("requestID", ctx.Request.Header.Get("X-Request-ID"))
		ctx.Next()
	}
}

func Auth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		resp := response.New(ctx)
		authHeader := ctx.GetHeader("Authorization")
		if authHeader == "" {
			resp.Fail(http.StatusUnauthorized, "未登入").Send()
			ctx.Abort()
			return
		}
		authorization := strings.TrimPrefix(authHeader, "Bearer ")
		JwtSecret := viper.GetString("Server.JwtKey")
		token, err := jwt.Parse(authorization, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				log.Error("token err :", token.Header["alg"])
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(JwtSecret), nil
		})

		if err != nil || !token.Valid {
			log.Error("token err :", err.Error())
			resp.Fail(http.StatusUnauthorized, "無效的 Token").Send()
			ctx.Abort()
			return
		}
		// 是否到期

		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			ctx.Set("UserId", claims["UserId"])
			ctx.Set("EnterpriseId", claims["EnterpriseId"])
			ctx.Set("ShopId", claims["ShopId"])
			ctx.Set("DeviceToken", claims["DeviceToken"])
		}
		ctx.Next()
	}
}

var hostname string
var ClientIP string

func Logger() gin.HandlerFunc {

	logFilePath := viper.GetString("Server.Logs.FilePath")
	logFileName := viper.GetString("Server.Logs.FileName")
	fullPath := path.Join(logFilePath, logFileName)
	// 每天換檔，保留 7 天
	writer, err := rotatelogs.New(
		fullPath+"_%Y%m%d.log",
		rotatelogs.WithLinkName(fullPath+".log"),  // 建立 symlink 指向最新檔
		rotatelogs.WithMaxAge(7*24*time.Hour),     // 保留 7 天
		rotatelogs.WithRotationTime(24*time.Hour), // 每 24 小時換一次
	)
	if err != nil {
		panic(err)
	}

	logger := logrus.New()                       //例項化
	logger.SetOutput(writer)                     //設定輸出
	logger.SetLevel(logrus.DebugLevel)           //設定日誌級別
	logger.SetFormatter(&logrus.TextFormatter{}) //設定日誌格式

	return func(ctx *gin.Context) {
		var bodyBytes []byte
		if ctx.Request.Body != nil {
			bodyBytes, _ = ioutil.ReadAll(ctx.Request.Body)
		}
		// 重新放回 Body，讓後續的 ShouldBindJSON 等仍可讀取
		ctx.Request.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
		hostname = ctx.Request.Host
		ClientIP = ctx.ClientIP()
		startTime := time.Now()               // 開始時間
		ctx.Next()                            // 處理請求
		endTime := time.Now()                 // 結束時間
		latencyTime := endTime.Sub(startTime) // 執行時間
		reqMethod := ctx.Request.Method       // 請求方式
		reqUri := ctx.Request.RequestURI      // 請求路由
		reqPost := common.JsonEncode(ctx.Request.PostForm)
		reqBody := string(bodyBytes)
		statusCode := ctx.Writer.Status() // 狀態碼
		clientIP := GetClientIP()         // 請求IP
		var heading bytes.Buffer
		for k, v := range ctx.Request.Header {
			head := make(map[string]interface{})
			head[k] = v
			jsonString, _ := json.Marshal(head)
			heading.WriteString(string(jsonString))
		}
		inf, _ := net.Interfaces()

		// 若為修改性請求（POST / PUT），額外寫一份應用程式層 INFO log，重點記錄「請求」內容
		if reqMethod == http.MethodPost || reqMethod == http.MethodPut {
			log.Info(
				"API Write Request | %s %s | ip=%s | statusCode=%d | body=%s",
				reqMethod,
				reqUri,
				clientIP,
				statusCode,
				common.Trim(reqBody),
			)
		}

		// access log：保留原本的 logrus 檔案輪替紀錄
		logger.Infof("| %3d | %13v | %15s | %s | %s | post=[%s] | body=[%s] | heading=[%s] | inf=[%v]", statusCode, latencyTime, clientIP, reqMethod, reqUri, reqPost, common.Trim(reqBody), heading.String(), inf)
	}
}

func GetClientIP() string {
	return ClientIP
}

// CORS 處理跨域請求的 middleware
// 注意：允許所有來源，這在生產環境中不太安全，僅供開發使用
func CORS() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		origin := ctx.GetHeader("Origin")

		// 允許所有來源
		// 如果有 Origin header，直接使用它（支援 Credentials）
		// 如果沒有 Origin header，使用 *（但這種情況下不能設置 Credentials）
		if origin != "" {
			ctx.Header("Access-Control-Allow-Origin", origin)
			ctx.Header("Access-Control-Allow-Credentials", "true")
		} else {
			ctx.Header("Access-Control-Allow-Origin", "*")
		}
		ctx.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		ctx.Header("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
		ctx.Header("Access-Control-Max-Age", "86400")

		// 處理 OPTIONS 預檢請求
		if ctx.Request.Method == "OPTIONS" {
			ctx.AbortWithStatus(http.StatusNoContent)
			return
		}

		ctx.Next()
	}
}
