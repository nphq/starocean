package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nphq/starocean/internal/db"
	"github.com/nphq/starocean/internal/middleware"
	"github.com/nphq/starocean/internal/server"
	"github.com/nphq/starocean/internal/shared"
)

//go:embed internal/db/migrations
var migrationsFS embed.FS

// 静态资源：Tailwind 构建产物 + vendored htmx（无前端构建链）。
//
//go:embed public
var publicFS embed.FS

func main() {
	// 兼容文档中的子命令形式: starocean <serve|migrate|seed> [flags]
	// （实际参数仍统一使用 flag；子命令只影响执行流程）
	cliCmd := ""
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "serve", "migrate", "seed":
			cliCmd = args[0]
			os.Args = append([]string{os.Args[0]}, args[1:]...)
		}
	}

	port := flag.String("port", envOr("PORT", "8080"), "listen port")
	// 默认单机 SQLite（零依赖），PostgreSQL 通过 postgres:// DSN 可选
	dbURL := flag.String("db", envOr("DATABASE_URL", "sqlite:starocean.db"), "database URL (sqlite:<path> 或 postgres://...)")
	secretKey := flag.String("secret", envOr("SECRET_KEY", ""), "session secret key")
	seedFlag := flag.Bool("seed", false, "seed demo data on startup")
	productCount := flag.Int("products", 0, "number of random products to generate (use with -seed)")
	clearFlag := flag.Bool("clear", false, "clear existing products before seeding (use with -seed -products)")
	demoPassword := flag.Bool("demo-password", false, "allow demo admin password (local evaluation ONLY, never in production)")
	flag.Parse()

	if cliCmd == "seed" {
		*seedFlag = true
	}

	// migrate/seed 不需要 SECRET_KEY（不启动 Web 服务）
	if *secretKey == "" && cliCmd != "migrate" && cliCmd != "seed" {
		log.Fatal("SECRET_KEY is required. Set via -secret flag or SECRET_KEY env var.")
	}
	// P0: 生产密钥强度校验（测试密钥 dev-secret 显式放行，避免误杀本地开发）。
	if cliCmd == "" || cliCmd == "serve" {
		if len(*secretKey) < 32 && *secretKey != "dev-secret-key-32-chars!!" {
			log.Fatal("SECRET_KEY 太弱：长度至少32字符，请用 `openssl rand -base64 32` 生成")
		}
	}

	database, err := db.Connect(*dbURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer func() { _ = database.Close() }()

	if err := db.Migrate(database, *dbURL, migrationsFS); err != nil {
		// P0: 迁移失败必须阻断启动（此前仅 log 继续 serve，会导致半迁移库对外服务）。
		// golang-migrate 的 dirty 状态也归为此类错误，直接退出由运维手动修复。
		log.Fatalf("migrate: %v", err)
	}

	if cliCmd == "migrate" {
		log.Println("database migrated")
		return
	}

	if *seedFlag {
		// 公开仓库安全基线：seed 默认拒绝演示口令，逼调用方显式决策。
		// 生产：设置 ADMIN_PASSWORD；本地评估：加 -demo-password 显式放行。
		if os.Getenv("ADMIN_PASSWORD") == "" && !*demoPassword {
			log.Fatal("seed refused: ADMIN_PASSWORD 未设置（本地演示请加 -demo-password 显式放行，生产请设置随机强口令）")
		}
		db.Seed(database, *productCount, *clearFlag)
	}

	if cliCmd == "seed" {
		log.Println("seed done")
		return
	}

	middleware.SetSecretKey(*secretKey)

	gin.SetMode(gin.ReleaseMode)
	if envOr("DEBUG", "") == "true" {
		gin.SetMode(gin.DebugMode)
	}

	r := gin.New()
	// 不信任任何代理头：直连部署下 ClientIP() 取 RemoteAddr，
	// 防止伪造 X-Forwarded-For 绕过登录限流（反代场景需自行调整）。
	_ = r.SetTrustedProxies(nil)
	r.Use(server.LoggerMiddleware())
	r.Use(server.RecoveryMiddleware())
	r.Use(server.SecurityHeadersMiddleware())
	r.Use(middleware.SessionMiddleware())
	// P0: no-store 仅作用于 API/打印页；静态资源允许缓存，否则 SPA 每次全量下载。
	r.Use(func(c *gin.Context) {
		p := c.Request.URL.Path
		if p == "/api" || strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/sales/") || strings.HasPrefix(p, "/finance/") {
			c.Header("Cache-Control", "no-store")
		}
		c.Next()
	})

	companyName := envOr("COMPANY_NAME", "StarOcean")

	// P0: 存活/就绪探针（k8s 健康检查用）。readyz 会 ping DB。
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.GET("/readyz", func(c *gin.Context) {
		if err := database.PingContext(c.Request.Context()); err != nil {
			c.JSON(503, gin.H{"ok": false})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})

	server.RegisterRoutes(r, database, publicFS, companyName)

	addr := fmt.Sprintf(":%s", *port)
	// P0: 显式超时（Slowloris 慢连接可耗尽无超时的服务）+ 优雅停机。
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	go func() {
		log.Printf("starocean starting on http://localhost%s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen %s: %v", addr, err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down (waiting up to 15s for in-flight requests)...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("forced shutdown: %v", err)
	}
	// 等待 FireAfter 异步监听器收尾（有上限，避免监听器卡住拖死停机）。
	if !shared.WaitHooks(5 * time.Second) {
		log.Printf("hooks wait timed out after 5s; continuing shutdown")
	}
	log.Println("starocean stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
