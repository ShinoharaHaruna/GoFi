package database

import (
	"log"

	"github.com/ShinoharaHaruna/GoFi/internal/config"
	"github.com/ShinoharaHaruna/GoFi/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

// InitDB 初始化数据库连接并自动迁移模式
// InitDB initializes the database connection and auto-migrates the schema
func InitDB(cfg *config.Config) error {
	var err error
	DB, err = gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	log.Println("Database connection established.")

	// 自动迁移模式
	// Auto-migrate the schema
	err = DB.AutoMigrate(&models.ShortLink{}, &models.ApiKey{})
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	log.Println("Database schema migrated.")
	return nil
}
