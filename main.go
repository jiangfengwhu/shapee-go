package main

import (
	"context"
	"log"
	"net/http"
	"shapee-go/config"
	"shapee-go/db"
	"shapee-go/middleware"
	"shapee-go/routes"
	"shapee-go/services"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("failed to validate config: %v", err)
	}

	db.Connect(cfg.Database.URL)
	defer db.Close()

	// 初始化数据库索引
	ctx := context.Background()
	if err := db.EnsurePlanIndexes(ctx); err != nil {
		log.Printf("warning: failed to create plan indexes: %v", err)
	}
	if err := db.EnsurePushTaskIndexes(ctx); err != nil {
		log.Printf("warning: failed to create push task indexes: %v", err)
	}

	// 初始化 APNs 服务
	apns, err := services.NewAPNsService(cfg.APNs)
	if err != nil {
		log.Printf("warning: APNs service init failed: %v (push notifications disabled)", err)
		apns, _ = services.NewAPNsService(config.APNsConfig{})
	}

	// 启动后台调度器
	scheduler := services.NewScheduler(cfg, apns)
	scheduler.Start()
	defer scheduler.Stop()

	r := gin.Default()

	// 公开路由
	routes.TicketRoutes(r, cfg)

	// 需要认证的路由
	protected := r.Group("/")
	protected.Use(middleware.TicketAuth)
	{
		routes.TicketProfileRoutes(protected)
		routes.WeightRoutes(protected, cfg)
		routes.PlanRoutes(protected)
	}

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
