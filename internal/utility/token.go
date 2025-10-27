package utility

import (
	"strings"

	"github.com/ShinoharaHaruna/GoFi/internal/database"
	"github.com/ShinoharaHaruna/GoFi/internal/models"
	"github.com/gin-gonic/gin"
)

// IsTokenValid 检查提供的 token 是否有效
// IsTokenValid checks if the provided token is valid
func IsTokenValid(c *gin.Context, keyType models.ApiKeyType) bool {
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
