package handlers

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ShinoharaHaruna/GoFi/internal/config"
	"github.com/ShinoharaHaruna/GoFi/internal/database"
	"github.com/ShinoharaHaruna/GoFi/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CreateShortLinkRequest 定义了创建短链接的请求体结构
// CreateShortLinkRequest defines the request body structure for creating a short link
type CreateShortLinkRequest struct {
	Filename string `json:"filename" binding:"required"`
}

// DisableShortLink godoc
//
//	@Summary		Disable short link
//	@Description	Soft-disable a short link by setting is_enabled to false
//	@Tags			Short Links
//	@Param			shortcode	path	string	true	"Short code of the file"
//	@Security		ApiKeyAuth
//	@Success		200	{object}	object{message=string}
//	@Failure		401	{object}	object{error=string}
//	@Failure		404	{object}	object{error=string}
//	@Failure		500	{object}	object{error=string}
//	@Router			/shorten/{shortcode} [delete]
func DisableShortLink(c *gin.Context) {
	if !isTokenValid(c, models.ApiKeyTypeShorten) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	shortcode := c.Param("shortcode")

	var shortLink models.ShortLink
	result := database.DB.Where("short_code = ?", shortcode).First(&shortLink)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Short link not found"})
		return
	}
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query short link"})
		return
	}

	if !shortLink.IsEnabled {
		c.JSON(http.StatusOK, gin.H{"message": "Short link already disabled"})
		return
	}

	if err := database.DB.Model(&shortLink).Update("is_enabled", false).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable short link"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Short link disabled"})
}

// EnableShortLink godoc
//
//	@Summary		Enable short link
//	@Description	Reactivate a short link by setting is_enabled to true
//	@Tags			Short Links
//	@Param			shortcode	path	string	true	"Short code of the file"
//	@Security		ApiKeyAuth
//	@Success		200	{object}	object{message=string}
//	@Failure		401	{object}	object{error=string}
//	@Failure		404	{object}	object{error=string}
//	@Failure		500	{object}	object{error=string}
//	@Router			/shorten/{shortcode}/enable [post]
func EnableShortLink(c *gin.Context) {
	if !isTokenValid(c, models.ApiKeyTypeShorten) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	shortcode := c.Param("shortcode")

	var shortLink models.ShortLink
	result := database.DB.Where("short_code = ?", shortcode).First(&shortLink)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Short link not found"})
		return
	}
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query short link"})
		return
	}

	if shortLink.IsEnabled {
		c.JSON(http.StatusOK, gin.H{"message": "Short link already enabled"})
		return
	}

	if err := database.DB.Model(&shortLink).Update("is_enabled", true).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable short link"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Short link enabled"})
}

// CreateShortLink godoc
//
//	@Summary		Create a short link
//	@Description	Creates a short link for an existing file. Requires a 'shorten' type token.
//	@Tags			Short Links
//	@Accept			json
//	@Produce		json
//	@Param			request	body	CreateShortLinkRequest	true	"Request body containing the filename"
//	@Security		ApiKeyAuth
//	@Success		200	{object}	object{short_url_path=string}
//	@Failure		400	{object}	object{error=string}
//	@Failure		401	{object}	object{error=string}
//	@Failure		404	{object}	object{error=string}
//	@Failure		500	{object}	object{error=string}
//	@Router			/shorten [post]
//
// CreateShortLink 创建一个新的短链接
// CreateShortLink creates a new short link
func CreateShortLink(c *gin.Context) {
	cfg, _ := c.Get("config")
	config := cfg.(*config.Config)

	// 1. 验证 Token
	// 1. Validate Token
	if !isTokenValid(c, models.ApiKeyTypeShorten) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// 2. 解析请求
	// 2. Parse Request
	var req CreateShortLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	// 3. 检查文件是否存在并确定其隐私状态
	// 3. Check if file exists and determine its privacy status
	cleanFilename := filepath.Clean(req.Filename)
	publicPath := filepath.Join(config.GoFiBaseDir, "public", cleanFilename)
	privatePath := filepath.Join(config.GoFiBaseDir, "private", cleanFilename)

	// 检查文件是否存在于任何一个目录中
	// Check if the file exists in either directory
	_, errPublic := os.Stat(publicPath)
	_, errPrivate := os.Stat(privatePath)

	if os.IsNotExist(errPublic) && os.IsNotExist(errPrivate) {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	// 确定文件是否为私有
	// Determine if the file is private
	isPrivate := !os.IsNotExist(errPrivate)

	// 4. 生成唯一的短代码
	// 4. Generate a unique short code
	shortCode, err := generateUniqueShortCode(5)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate short code"})
		return
	}

	// 5. 创建数据库记录
	// 5. Create database record
	shortLink := models.ShortLink{
		ShortCode:        shortCode,
		OriginalFilename: cleanFilename,
		IsPrivate:        isPrivate,
		IsEnabled:        true, // 默认启用 / Enabled by default
	}

	if result := database.DB.Create(&shortLink); result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save short link"})
		return
	}

	// 6. 返回短链接 URL
	// 6. Return the short link URL
	// 注意：这里的 URL 应该由客户端根据自己的域名构建，服务器只提供路径
	// Note: The URL here should be constructed by the client based on its own domain, the server only provides the path
	c.JSON(http.StatusOK, gin.H{"short_url_path": "/s/" + shortCode})
}

// DownloadFileFromShortLink godoc
//
//	@Summary		Download a file from a short link
//	@Description	Downloads a file using a short code. If the original file is private, a 'download' type token is required.
//	@Tags			Short Links
//	@Produce		application/octet-stream
//	@Param			shortcode	path		string	true	"Short code of the file"
//	@Param			token		query		string	false	"Authentication token for private files"
//	@Success		200			{file}		file	"The requested file"
//	@Failure		401			{object}	object{error=string}
//	@Failure		404			{object}	object{error=string}
//	@Failure		500			{object}	object{error=string}
//	@Router			/s/{shortcode} [get]
//
// DownloadFileFromShortLink 处理通过短链接下载文件的请求
// DownloadFileFromShortLink handles file download requests via short link
func DownloadFileFromShortLink(c *gin.Context) {
	cfg, _ := c.Get("config")
	config := cfg.(*config.Config)
	shortCode := c.Param("shortcode")

	// 1. 在数据库中查找短链接
	// 1. Find the short link in the database
	var shortLink models.ShortLink
	if result := database.DB.Where("short_code = ?", shortCode).First(&shortLink); result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Short link not found"})
		return
	}

	// 2. 检查短链接是否启用
	// 2. Check if the short link is enabled
	if !shortLink.IsEnabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "Short link is disabled"})
		return
	}

	// 3. 如果是私有文件，验证 Token
	// 3. If it's a private file, validate the Token
	if shortLink.IsPrivate {
		if !isTokenValid(c, models.ApiKeyTypeDownload) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
	}

	// 4. 构建文件路径并提供下载
	// 4. Build the file path and serve the download
	var filePath string
	if shortLink.IsPrivate {
		filePath = filepath.Join(config.GoFiBaseDir, "private", shortLink.OriginalFilename)
	} else {
		filePath = filepath.Join(config.GoFiBaseDir, "public", shortLink.OriginalFilename)
	}

	// 安全检查：确保最终路径仍在 base dir 内
	// Security check: ensure the final path is still within the base dir
	if !isPathSafe(filePath, config.GoFiBaseDir) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// 检查文件是否存在
	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Original file not found"})
		return
	}

	c.File(filePath)
}
