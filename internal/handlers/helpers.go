package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ShinoharaHaruna/GoFi/internal/database"
	"github.com/ShinoharaHaruna/GoFi/internal/models"
	"github.com/gin-gonic/gin"
)

// isTokenValid 检查提供的 token 是否有效
// isTokenValid checks if the provided token is valid
func isTokenValid(c *gin.Context, keyType models.ApiKeyType) bool {
	// 1. 按优先级顺序从 Header, Path, Query 中获取 Token
	// 1. Get Token from Header, Path, Query in order of priority
	token := ""
	authHeader := c.GetHeader("Authorization")
	if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		token = after
	} else {
		token = c.Query("token")
	}

	if token == "" {
		return false
	}

	// 2. 在数据库中查找 Token
	// 2. Find the Token in the database
	var apiKey models.ApiKey
	result := database.DB.Where("key = ? AND type = ?", token, keyType).First(&apiKey)
	if result.Error != nil {
		return false // Token 不存在或类型不匹配 / Token does not exist or type mismatch
	}

	// 3. 检查 Token 是否启用
	// 3. Check if the Token is enabled
	return apiKey.IsEnabled
}

// generateUniqueShortCode 生成一个在数据库中唯一的短代码
// generateUniqueShortCode generates a short code that is unique in the database
func generateUniqueShortCode(length int) (string, error) {
	for range 10 { // 尝试 10 次以避免无限循环 / Try 10 times to avoid an infinite loop
		code, err := generateRandomString(length)
		if err != nil {
			return "", err
		}

		var count int64
		database.DB.Model(&models.ShortLink{}).Where("short_code = ?", code).Count(&count)
		if count == 0 {
			return code, nil
		}
	}
	return "", fmt.Errorf("failed to generate a unique short code after multiple attempts")
}

// generateRandomString 生成指定长度的随机十六进制字符串
// generateRandomString generates a random hex string of the specified length
func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// isPathSafe 检查目标路径是否在基础目录内
// isPathSafe checks if the target path is within the base directory
func isPathSafe(targetPath, baseDir string) bool {
	cleanBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	cleanTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	return strings.HasPrefix(cleanTargetPath, cleanBaseDir)
}
