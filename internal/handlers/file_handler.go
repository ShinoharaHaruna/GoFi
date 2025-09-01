package handlers

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/ShinoharaHaruna/GoFi/internal/config"
	"github.com/ShinoharaHaruna/GoFi/internal/models"
	"github.com/gin-gonic/gin"
)

// UploadFile godoc
//
//	@Summary		Upload a file
//	@Description	Uploads a file to either the public or private directory. Requires an 'upload' type token.
//	@Tags			Files
//	@Accept			multipart/form-data
//	@Produce		json
//	@Param			file				formData	file	true	"File to upload"
//	@Param			X-GoFi-Target-Dir	header		string	false	"Target directory: 'public' or 'private' (default)"	Enums(public, private)
//	@Security		ApiKeyAuth
//	@Success		200	{object}	object{download_path=string}
//	@Failure		400	{object}	object{error=string}
//	@Failure		401	{object}	object{error=string}
//	@Failure		500	{object}	object{error=string}
//	@Router			/upload [post]
//
// UploadFile 处理文件上传请求
// UploadFile handles file upload requests
func UploadFile(c *gin.Context) {
	cfg, _ := c.Get("config")
	config := cfg.(*config.Config)

	// 1. 验证 Token
	// 1. Validate Token
	if !isTokenValid(c, models.ApiKeyTypeUpload) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// 2. 解析 multipart/form-data
	// 2. Parse multipart/form-data
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid file upload request: " + err.Error()})
		return
	}

	// 3. 确定目标目录
	// 3. Determine target directory
	targetDir := c.GetHeader("X-GoFi-Target-Dir")
	if targetDir != "public" {
		targetDir = "private" // 默认为 private / Default to private
	}

	// 4. 构建并清理目标路径
	// 4. Build and clean the destination path
	// 安全措施：只使用文件名，防止路径遍历
	// Security measure: only use the filename, prevent path traversal
	filename := filepath.Base(file.Filename)
	destPath := filepath.Join(config.GoFiBaseDir, targetDir, filename)

	// 再次检查，确保路径不会逃逸出 base dir
	// Double-check to ensure the path does not escape the base dir
	if !isPathSafe(destPath, config.GoFiBaseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename or path"})
		return
	}

	// 5. 保存文件
	// 5. Save the file
	if err := c.SaveUploadedFile(file, destPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file: " + err.Error()})
		return
	}

	// 6. 返回下载路径
	// 6. Return the download path
	c.JSON(http.StatusOK, gin.H{"download_path": "/" + filename})
}

// DownloadFile godoc
//
//	@Summary		Download a file
//	@Description	Downloads a file. Public files are accessible directly. For private files, a 'download' type token is required via query parameter or Authorization header.
//	@Tags			Files
//	@Produce		application/octet-stream
//	@Param			filename	path	string	true	"Filename"
//	@Param			token		query	string	false	"Authentication token for private files"
//	@Security		ApiKeyAuth
//	@Success		200	{file}		file	"The requested file"
//	@Failure		401	{object}	object{error=string}
//	@Failure		403	{object}	object{error=string}
//	@Failure		404	{object}	object{error=string}
//	@Router			/{filename} [get]
//
// DownloadFile 处理文件下载请求
// DownloadFile handles file download requests
func DownloadFile(c *gin.Context) {
	cfg, _ := c.Get("config")
	config := cfg.(*config.Config)
	filename := c.Param("filename")

	// 安全措施：清理路径，防止遍历
	// Security measure: clean the path to prevent traversal
	cleanFilename := filepath.Base(filename)

	// 1. 尝试从 public 目录提供文件
	// 1. Try to serve the file from the public directory
	publicPath := filepath.Join(config.GoFiBaseDir, "public", cleanFilename)
	if _, err := os.Stat(publicPath); err == nil {
		// 安全检查：确保路径不会逃逸
		// Security check: ensure the path does not escape
		if !isPathSafe(publicPath, config.GoFiBaseDir) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}
		c.File(publicPath)
		return
	}

	// 2. 尝试从 private 目录提供文件
	// 2. Try to serve the file from the private directory
	privatePath := filepath.Join(config.GoFiBaseDir, "private", cleanFilename)
	if _, err := os.Stat(privatePath); err == nil {
		// 验证 Token
		// Validate Token
		if !isTokenValid(c, models.ApiKeyTypeDownload) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}

		// 安全检查：确保路径不会逃逸
		// Security check: ensure the path does not escape
		if !isPathSafe(privatePath, config.GoFiBaseDir) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}
		c.File(privatePath)
		return
	}

	// 3. 如果文件在两个目录都不存在
	// 3. If the file does not exist in either directory
	c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
}
