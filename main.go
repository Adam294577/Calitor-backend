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
	"project/services/log"
	"project/services/redis"
	response "project/services/responses"
	"project/services/storage"
	"runtime"
	"strings"
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

	// 初始化並檢查 PostgreSQL 連接
	dbTest := models.PostgresNew()
	fmt.Println("✓ PostgreSQL 資料庫連線成功")

	// // 自動遷移資料表
	if err := models.MigrateAll(dbTest); err != nil {
		fmt.Printf("⚠ 資料表遷移失敗: %s\n", err.Error())
	} else {
		fmt.Println("✓ 資料表遷移完成")
	}

	// 初始化預設資料
	models.SeedPermissionsAndRoles(dbTest)
	models.SeedDefaultAdmin(dbTest)
	// fmt.Println("✓ 預設資料初始化完成")

	dbTest.Close()

	// 初始化並檢查 Redis 連接
	redisClient := redis.NewRedisClient()
	if redisClient.IsAvailable() {
		fmt.Println("✓ Redis 緩存功能已啟用")
	} else {
		fmt.Println("⚠ Redis 緩存功能未啟用，將使用優雅降級模式（直接查詢資料庫）")
	}
	redisClient.Close() // 關閉測試連接，後續使用時會重新創建

	// 初始化並檢查 MinIO 連接
	minioClient := storage.NewClient()
	if minioClient.IsAvailable() {
		fmt.Println("✓ MinIO 檔案儲存功能已啟用")
	} else {
		fmt.Println("⚠ MinIO 檔案儲存功能未啟用")
	}

	// 啟動Gin服務
	HttpServer = gin.Default()
	// 設定信任的 Proxy（請修改為你的反向代理 IP）
	if err := HttpServer.SetTrustedProxies(nil); err != nil {
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

	// 添加 swagger 路由（需要在中间件之前注册，确保不被拦截）
	// 处理 /swagger/ 根路径，重定向到 index.html
	// HttpServer.GET("/swagger/", func(ctx *gin.Context) {
	// ctx.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	// })
	// 使用 ginSwagger.WrapHandler 处理所有 Swagger 路径，包括 doc.json
	HttpServer.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 使用 middleware（CORS 需要最先執行，排除 Swagger 路径）
	HttpServer.Use(
		func(ctx *gin.Context) {
			// 排除 Swagger 路径
			if strings.HasPrefix(ctx.Request.URL.Path, "/swagger") {
				ctx.Next()
				return
			}
			middlewares.CORS()(ctx)
		},
		func(ctx *gin.Context) {
			// 排除 Swagger 路径
			if strings.HasPrefix(ctx.Request.URL.Path, "/swagger") {
				ctx.Next()
				return
			}
			// 設定變數
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
			// 排除 Swagger 路径
			if strings.HasPrefix(ctx.Request.URL.Path, "/swagger") {
				ctx.Next()
				return
			}
			middlewares.Logger()(ctx)
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
