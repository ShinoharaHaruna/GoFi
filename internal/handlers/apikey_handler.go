package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ShinoharaHaruna/GoFi/internal/database"
	"github.com/ShinoharaHaruna/GoFi/internal/models"
	"github.com/ShinoharaHaruna/GoFi/internal/utility"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateAPIKeyRequest 定义创建 API Key 的请求体 / CreateAPIKeyRequest defines the request body for creating an API key
type CreateAPIKeyRequest struct {
	Type string `json:"type" binding:"required"`
}

// ApiKeyResponse 表示 API Key 的响应结构 / ApiKeyResponse represents the response structure for an API key
type ApiKeyResponse struct {
	Key       string            `json:"key"`
	Type      models.ApiKeyType `json:"type"`
	IsEnabled bool              `json:"is_enabled"`
}

// CreateAPIKey godoc
//
//	@Summary		Create API key
//	@Description	Create a new API key. Requires an `api` type key.
//	@Tags			API Keys
//	@Accept			json
//	@Produce		json
//	@Param			request	body	CreateAPIKeyRequest	true	"API key information"
//	@Security		ApiKeyAuth
//	@Success		201	{object}	ApiKeyResponse
//	@Failure		400	{object}	object{error=string}
//	@Failure		401	{object}	object{error=string}
//	@Failure		500	{object}	object{error=string}
//	@Router			/api-keys [post]
func CreateAPIKey(c *gin.Context) {
	if !utility.IsTokenValid(c, models.ApiKeyTypeAPI) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	keyType, ok := parseAPIKeyType(req.Type)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid API key type"})
		return
	}

	keyValue, err := utility.GenerateUUIDv4()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate key"})
		return
	}

	apiKey := models.ApiKey{
		Key:       keyValue,
		Type:      keyType,
		IsEnabled: true,
	}

	if err := database.DB.Create(&apiKey).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create API key"})
		return
	}

	c.JSON(http.StatusCreated, ApiKeyResponse{
		Key:       apiKey.Key,
		Type:      apiKey.Type,
		IsEnabled: apiKey.IsEnabled,
	})
}

// DisableAPIKey godoc
//
//	@Summary		Disable API key
//	@Description	Soft-disable an API key by setting its is_enabled flag to false.
//	@Tags			API Keys
//	@Param			key	path	string	true	"API key value"
//	@Security		ApiKeyAuth
//	@Success		200	{object}	object{message=string}
//	@Failure		400	{object}	object{error=string}
//	@Failure		401	{object}	object{error=string}
//	@Failure		404	{object}	object{error=string}
//	@Failure		500	{object}	object{error=string}
//	@Router			/api-keys/{key} [delete]
func DisableAPIKey(c *gin.Context) {
	if !utility.IsTokenValid(c, models.ApiKeyTypeAPI) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	apiKey, err := findAPIKeyByKeyParam(c)
	if err != nil {
		// findAPIKeyByKeyParam 已返回相应的响应 / findAPIKeyByKeyParam already responded
		return
	}

	if !apiKey.IsEnabled {
		c.JSON(http.StatusOK, gin.H{"message": "API key already disabled"})
		return
	}

	if err := database.DB.Model(&apiKey).Update("is_enabled", false).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable API key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API key disabled"})
}

// EnableAPIKey godoc
//
//	@Summary		Enable API key
//	@Description	Enable an API key by setting its is_enabled flag to true.
//	@Tags			API Keys
//	@Param			key	path	string	true	"API key value"
//	@Security		ApiKeyAuth
//	@Success		200	{object}	object{message=string}
//	@Failure		400	{object}	object{error=string}
//	@Failure		401	{object}	object{error=string}
//	@Failure		404	{object}	object{error=string}
//	@Failure		500	{object}	object{error=string}
//	@Router			/api-keys/{key}/enable [post]
func EnableAPIKey(c *gin.Context) {
	if !utility.IsTokenValid(c, models.ApiKeyTypeAPI) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	apiKey, err := findAPIKeyByKeyParam(c)
	if err != nil {
		return
	}

	if apiKey.IsEnabled {
		c.JSON(http.StatusOK, gin.H{"message": "API key already enabled"})
		return
	}

	if err := database.DB.Model(&apiKey).Update("is_enabled", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable API key"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "API key enabled"})
}

// findAPIKeyByKeyParam 读取路径参数并基于 key 查找 API Key / findAPIKeyByKeyParam fetches the API key by its value
func findAPIKeyByKeyParam(c *gin.Context) (models.ApiKey, error) {
	keyParam := c.Param("key")
	trimmedKey := strings.TrimSpace(keyParam)
	if trimmedKey == "" {
		errMsg := "Invalid API key"
		c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
		return models.ApiKey{}, errors.New(errMsg)
	}

	var apiKey models.ApiKey
	result := database.DB.Where("key = ?", trimmedKey).First(&apiKey)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "API key not found"})
		return models.ApiKey{}, result.Error
	}
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query API key"})
		return models.ApiKey{}, result.Error
	}

	return apiKey, nil
}

// parseAPIKeyType 将字符串解析为 ApiKeyType / parseAPIKeyType converts string into ApiKeyType
func parseAPIKeyType(input string) (models.ApiKeyType, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(input))
	switch models.ApiKeyType(trimmed) {
	case models.ApiKeyTypeUpload,
		models.ApiKeyTypeDownload,
		models.ApiKeyTypeShorten,
		models.ApiKeyTypeAPI:
		return models.ApiKeyType(trimmed), true
	default:
		return "", false
	}
}
