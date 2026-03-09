package middlewares

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// SkipMiddleware 判斷該路徑是否應跳過業務 middleware（swagger、health）
func SkipMiddleware(path string) bool {
	return strings.HasPrefix(path, "/swagger") || path == "/health"
}

// IPWhiteList 限制僅允許白名單內的公網 IP 存取
// 支援單一 IP 與 CIDR 表示法（例如 "192.168.1.0/24"），逗點分隔多筆
func IPWhiteList() gin.HandlerFunc {
	allowedIPStr := viper.GetString("Server.Security.AllowedOfficeIP")

	// 若未設定白名單，不限制（方便本地開發）
	if strings.TrimSpace(allowedIPStr) == "" {
		log.Warn("IPWhiteList: AllowedOfficeIP 未設定，IP 防火牆已停用")
		return func(ctx *gin.Context) {
			ctx.Next()
		}
	}

	// 預先解析白名單：區分精確 IP 與 CIDR 網段
	parts := strings.Split(allowedIPStr, ",")
	allowedIPs := make(map[string]bool, len(parts))
	var allowedNets []*net.IPNet

	for _, entry := range parts {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, ipNet, err := net.ParseCIDR(entry)
			if err != nil {
				log.Error("IPWhiteList: 無法解析 CIDR %s: %s", entry, err.Error())
				continue
			}
			allowedNets = append(allowedNets, ipNet)
		} else {
			// 用 net.ParseIP 標準化後存入（處理 IPv4-mapped IPv6 等情況）
			if parsed := net.ParseIP(entry); parsed != nil {
				allowedIPs[parsed.String()] = true
			} else {
				log.Error("IPWhiteList: 無法解析 IP %s", entry)
			}
		}
	}

	log.Info("IPWhiteList: 已載入 %d 個 IP + %d 個 CIDR 網段", len(allowedIPs), len(allowedNets))

	return func(ctx *gin.Context) {
		resp := response.New(ctx)

		// 取得客戶端 IP：
		// 1. 優先使用 Cloudflare 的 CF-Connecting-IP（最可靠）
		// 2. 其次從 X-Forwarded-For 取第一個（原始客戶端 IP）
		// 3. 最後 fallback 到 RemoteIP（無代理場景）
		rawIP := ""
		if cfIP := ctx.GetHeader("CF-Connecting-IP"); cfIP != "" {
			rawIP = strings.TrimSpace(cfIP)
		} else if xff := ctx.GetHeader("X-Forwarded-For"); xff != "" {
			xffParts := strings.Split(xff, ",")
			rawIP = strings.TrimSpace(xffParts[0])
		} else {
			rawIP = ctx.RemoteIP()
		}

		// 用 net.ParseIP 標準化，確保 IPv6/IPv4 格式一致
		parsed := net.ParseIP(rawIP)
		if parsed == nil {
			log.Warn("IPWhiteList: 無法解析來源 IP [%s]", rawIP)
			resp.Fail(http.StatusForbidden, "Forbidden").Send()
			ctx.Abort()
			return
		}
		clientIP := parsed.String()

		// 精確 IP 比對
		if allowedIPs[clientIP] {
			ctx.Next()
			return
		}

		// CIDR 網段比對
		for _, ipNet := range allowedNets {
			if ipNet.Contains(parsed) {
				ctx.Next()
				return
			}
		}

		log.Warn("IPWhiteList: 拒絕來自 %s 的請求", clientIP)
		resp.Fail(http.StatusForbidden, "Forbidden").Send()
		ctx.Abort()
	}
}

func Middleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// 設定變數
		ctx.Set("requestID", ctx.Request.Header.Get("X-Request-ID"))
		ctx.Next()
	}
}

// RequirePermission 檢查使用者是否擁有指定的任一權限
func RequirePermission(keys ...string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		resp := response.New(ctx)
		perms, exists := ctx.Get("Permissions")
		if !exists {
			resp.Fail(http.StatusForbidden, "無權限").Send()
			ctx.Abort()
			return
		}

		permSlice, ok := perms.([]interface{})
		if !ok {
			resp.Fail(http.StatusForbidden, "無權限").Send()
			ctx.Abort()
			return
		}

		for _, key := range keys {
			for _, p := range permSlice {
				if str, ok := p.(string); ok && str == key {
					ctx.Next()
					return
				}
			}
		}

		resp.Fail(http.StatusForbidden, "無權限執行此操作").Send()
		ctx.Abort()
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

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			resp.Fail(http.StatusUnauthorized, "無效的 Token").Send()
			ctx.Abort()
			return
		}

		// 明確檢查 Token 是否過期
		if exp, ok := claims["exp"].(float64); ok {
			if time.Now().Unix() > int64(exp) {
				resp.Fail(http.StatusUnauthorized, "Token 已過期，請重新登入").Send()
				ctx.Abort()
				return
			}
		} else {
			resp.Fail(http.StatusUnauthorized, "無效的 Token（缺少過期時間）").Send()
			ctx.Abort()
			return
		}

		ctx.Set("AdminId", claims["AdminId"])
		ctx.Set("Account", claims["Account"])
		ctx.Set("RoleId", claims["RoleId"])
		if perms, exists := claims["Permissions"]; exists {
			ctx.Set("Permissions", perms)
		}
		ctx.Next()
	}
}

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
			bodyBytes, _ = io.ReadAll(ctx.Request.Body)
		}
		// 重新放回 Body，讓後續的 ShouldBindJSON 等仍可讀取
		ctx.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		clientIP := ctx.ClientIP()
		startTime := time.Now()               // 開始時間
		ctx.Next()                            // 處理請求
		endTime := time.Now()                 // 結束時間
		latencyTime := endTime.Sub(startTime) // 執行時間
		reqMethod := ctx.Request.Method       // 請求方式
		reqUri := ctx.Request.RequestURI      // 請求路由
		reqPost := common.JsonEncode(ctx.Request.PostForm)
		reqBody := string(bodyBytes)
		statusCode := ctx.Writer.Status() // 狀態碼
		var heading bytes.Buffer
		for k, v := range ctx.Request.Header {
			head := make(map[string]interface{})
			head[k] = v
			jsonString, _ := json.Marshal(head)
			heading.WriteString(string(jsonString))
		}
		inf, _ := net.Interfaces()

		// 終端即時 log
		fmt.Printf("[API] %3d | %13v | %s | %s\n", statusCode, latencyTime, reqMethod, reqUri)

		// 若為修改性請求（POST / PUT / PATCH），額外寫一份應用程式層 INFO log，重點記錄「請求」內容
		if reqMethod == http.MethodPost || reqMethod == http.MethodPut || reqMethod == http.MethodPatch {
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

// CORS 處理跨域請求的 middleware
// 從設定檔讀取 Server.AllowedOrigins（字串陣列），若未設定則預設僅允許 localhost
func CORS() gin.HandlerFunc {
	allowed := viper.GetStringSlice("Server.AllowedOrigins")
	// viper 從環境變數讀取 StringSlice 時，不會自動分割逗號
	// 若只有一個元素且含逗號，手動分割
	if len(allowed) == 1 && strings.Contains(allowed[0], ",") {
		allowed = strings.Split(allowed[0], ",")
		for i := range allowed {
			allowed[i] = strings.TrimSpace(allowed[i])
		}
	}
	if len(allowed) == 0 {
		allowed = []string{
			"http://localhost:5173",
			"http://localhost:4173",
			"http://127.0.0.1:5173",
			"http://127.0.0.1:4173",
		}
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		allowedSet[o] = true
	}

	return func(ctx *gin.Context) {
		origin := ctx.GetHeader("Origin")

		if origin != "" {
			// 檢查 origin 是否在允許清單中
			if allowedSet[origin] {
				ctx.Header("Access-Control-Allow-Origin", origin)
				ctx.Header("Access-Control-Allow-Credentials", "true")
			} else {
				// 不在允許清單中：不設定 CORS header，瀏覽器會阻擋請求
				ctx.AbortWithStatus(http.StatusForbidden)
				return
			}
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
