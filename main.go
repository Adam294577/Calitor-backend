package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"project/cron"
	"project/middlewares"
	"project/models"
	"project/routes"
	"project/services/firewall"
	"project/services/log"
	"project/services/redis"
	response "project/services/responses"
	"project/services/storage"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"github.com/swaggo/swag/example/basic/docs"
)

// @title Landtop API
// @version 1.0
// @description Landtop API文檔
// @host localhost:8002
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description 請輸入 Bearer Token，格式為：Bearer <token>
// @schemes http https

var config string

func init() {
	// 優先使用環境變數 ENV，如果沒有則檢查命令行參數
	env := os.Getenv("ENV")
	if env == "" {
		args := os.Args
		// 如果提供了命令行參數，使用參數指定的環境
		if len(args) > 1 {
			env = args[1]
		} else {
			// 如果都沒有設置，默認使用 prod（適合生產環境部署）
			env = "prod"
		}
	}
	configFile := fmt.Sprintf("config/config_%s.yaml", env)
	flag.StringVar(&config, "c", configFile, "Configuration file path.")
	flag.Parse()
	// 初始化配置
	initConfig()
}

// initConfig 初始化配置
func initConfig() {
	// 設置環境變數替換規則：將配置路徑中的點號替換為下劃線
	// 例如 Redis.Host 可以通過 REDIS_HOST 環境變數覆蓋
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetConfigFile(config)
	if err := viper.ReadInConfig(); err != nil {
		// 找不到設定檔時僅靠環境變數運作（適用於容器部署）
		fmt.Printf("⚠ 未載入設定檔（%s），將完全使用環境變數\n", config)
	}

	// 安全關鍵設定明確 BindEnv,避免 viper 對巢狀 key 的 AutomaticEnv 在不同版本行為不一致
	// 確保 SERVER_SECURITY_ALLOWEDOFFICEIP 一定能覆蓋 YAML 的同名欄位
	_ = viper.BindEnv("Server.Security.AllowedOfficeIP", "SERVER_SECURITY_ALLOWEDOFFICEIP")
}

var HttpServer *gin.Engine

func main() {
	// 捕獲panic不崩潰
	defer func() {
		if err := recover(); err != nil {
			fmt.Println("recover error", err)
		}
	}()
	App(HttpServer)
}

func App(HttpServer *gin.Engine) {
	numCPUs := runtime.NumCPU()
	log.Info("CPU cores: %d", numCPUs)

	// 初始化全域 PostgreSQL 連線池（啟動一次，所有 controller 共用）
	db := models.PostgresInit()
	fmt.Println("✓ PostgreSQL 資料庫連線成功")

	// 自動遷移 & Seed（僅在 RUN_MIGRATE=true 時執行，預設跳過）
	if os.Getenv("RUN_MIGRATE") == "true" {
		if err := models.MigrateAll(db); err != nil {
			fmt.Printf("⚠ 資料表遷移失敗: %s\n", err.Error())
		} else {
			fmt.Println("✓ 資料表遷移完成")
		}
		models.SeedPermissionsAndRoles(db)
		models.SeedDefaultAdmin(db)
		models.SeedBanks(db)
	} else {
		fmt.Println("⏭ 略過資料表遷移與 Seed（設定 RUN_MIGRATE=true 以啟用）")
	}

	// 每次啟動都同步環境變數 IP 到 firewall_ips 表（env 仍為 source of truth）
	models.SyncEnvFirewallIPs(db)

	// 一次性遷移：將 role_permissions 中的父節點展開為葉子節點
	// models.MigrateRolePermissionsToLeaf(db)

	// 並行初始化 Redis 與 MinIO（減少啟動等待時間）
	var redisClient *redis.Client
	var minioClient *storage.Client
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		redisClient = redis.InitGlobal()
	}()

	go func() {
		defer wg.Done()
		minioClient = storage.NewClient()
	}()

	wg.Wait()

	if redisClient.IsAvailable() {
		fmt.Println("✓ Redis 緩存功能已啟用")
	} else {
		fmt.Println("⚠ Redis 緩存功能未啟用，將使用優雅降級模式（直接查詢資料庫）")
	}
	if minioClient.IsAvailable() {
		fmt.Println("✓ MinIO 檔案儲存功能已啟用")
	} else {
		fmt.Println("⚠ MinIO 檔案儲存功能未啟用")
	}

	// 清除 firewall 白名單的 Redis 殘留快取:redeploy 後若 Redis 仍持有上次部屬的 snapshot,
	// 新加入 env 的合法 IP 會被舊 cache 擋下最多 5 分鐘 (cacheTTL)。SyncEnvFirewallIPs 已寫入 DB,
	// 這裡確保下次 Load() 必定 cache miss → 重新從 DB 取得最新白名單。
	firewall.Invalidate()

	// 啟動Gin服務
	HttpServer = gin.Default()
	// 設定信任的 Proxy（請修改為你的反向代理 IP）
	if err := HttpServer.SetTrustedProxies([]string{"127.0.0.1", "::1", "172.16.0.0/12", "10.0.0.0/8"}); err != nil {
		fmt.Println("設定信任Proxy錯誤")
		return
	}

	// 啟動伺服器
	// 優先使用環境變數 PORT（雲平台標準做法）
	port := os.Getenv("PORT")
	if port == "" {
		// 如果沒有環境變數，使用配置文件中的端口
		port = viper.GetString("Server.Website.Port")
	}
	if port == "" {
		port = "8002" // 默認端口
	}

	// 确保 docs 包被初始化并设置正确的配置
	docs.SwaggerInfo.BasePath = "/"
	// 優先使用環境變數 HOST（部署環境），如果沒有則使用 localhost
	host := os.Getenv("HOST")
	if host == "" {
		host = fmt.Sprintf("localhost:%s", port)
	}
	docs.SwaggerInfo.Host = host

	// Swagger 僅在 debug 模式下載入
	if viper.GetString("Server.Mode") == "debug" {
		HttpServer.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	// 預建 middleware 實例（只初始化一次，避免每次請求重複建立）
	corsMiddleware := middlewares.CORS()
	ipWhiteList := middlewares.IPWhiteList()
	loggerMiddleware := middlewares.Logger()

	// 使用 middleware（Security Headers → CORS → IP 白名單 → requestID → log → recovery）
	HttpServer.Use(
		middlewares.SecurityHeaders(),
		func(ctx *gin.Context) {
			if middlewares.SkipMiddleware(ctx.Request.URL.Path) {
				ctx.Next()
				return
			}
			corsMiddleware(ctx)
		},
		func(ctx *gin.Context) {
			if middlewares.SkipMiddleware(ctx.Request.URL.Path) {
				ctx.Next()
				return
			}
			ipWhiteList(ctx)
		},
		func(ctx *gin.Context) {
			if middlewares.SkipMiddleware(ctx.Request.URL.Path) {
				ctx.Next()
				return
			}
			ctx.Set("requestID", ctx.Request.Header.Get("X-Request-ID"))
			ctx.Next()
		},
		// 開發環境終端即時 log
		func(ctx *gin.Context) {
			start := time.Now()
			ctx.Next()
			fmt.Printf("[API] %3d | %13v | %-7s | %s\n",
				ctx.Writer.Status(),
				time.Since(start),
				ctx.Request.Method,
				ctx.Request.RequestURI,
			)
		},
		func(ctx *gin.Context) {
			if middlewares.SkipMiddleware(ctx.Request.URL.Path) {
				ctx.Next()
				return
			}
			loggerMiddleware(ctx)
		},
		gin.Recovery(),
	)

	// 執行排程
	go cron.Run()
	// 注冊路由
	routes.RouterRegister(HttpServer)

	// 當Route不存在時的處理
	HttpServer.NoRoute(func(ctx *gin.Context) {
		resp := response.New(ctx)
		resp.Fail(http.StatusNotFound, "路由不存在").Send()
	})

	startServer(HttpServer, port)
}

func startServer(router *gin.Engine, port string) {
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: router,
	}

	go func() {
		fmt.Printf("伺服器運行於 %s port \n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("listen: %s\n", err.Error())
		}
	}()

	// 优雅关闭逻辑
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM) // Windows支持这两个信号
	<-quit
	fmt.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Printf("Server forced to shutdown: %s\n", err.Error())
	}
	fmt.Println("Server exiting")
}
