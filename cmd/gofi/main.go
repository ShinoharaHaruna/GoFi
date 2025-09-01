package main

import (
	"fmt"
	"log"

	"github.com/ShinoharaHaruna/GoFi/internal/config"
	"github.com/ShinoharaHaruna/GoFi/internal/database"
	"github.com/ShinoharaHaruna/GoFi/internal/router"
	"github.com/gin-gonic/gin"
)

//	@title			GoFi API
//	@version		1.0
//	@description	This is a simple file sharing service.
//	@termsOfService	http://swagger.io/terms/

//	@contact.name	API Support
//	@contact.url	http://www.swagger.io/support
//	@contact.email	support@swagger.io

//	@license.name	Apache 2.0
//	@license.url	http://www.apache.org/licenses/LICENSE-2.0.html

//	@host		localhost:8080
//	@BasePath	/

// @securityDefinitions.apikey	ApiKeyAuth
// @in							header
// @name						Authorization
func main() {
	// 加载配置
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// 初始化数据库
	// Initialize database
	if err := database.InitDB(cfg); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// 设置 Gin 模式
	// Set Gin mode
	gin.SetMode(cfg.GinMode)

	// 创建路由
	// Create router
	r := router.SetupRouter(cfg)

	// 启动服务器
	// Start server
	listenAddr := fmt.Sprintf(":%s", cfg.GoFiPort)
	log.Printf("Server starting on %s", listenAddr)
	if err := r.Run(listenAddr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
