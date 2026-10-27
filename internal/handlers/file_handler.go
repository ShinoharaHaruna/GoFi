package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ShinoharaHaruna/GoFi/internal/config"
	"github.com/ShinoharaHaruna/GoFi/internal/database"
	"github.com/ShinoharaHaruna/GoFi/internal/models"
	"github.com/ShinoharaHaruna/GoFi/internal/utility"
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
	if !utility.IsTokenValid(c, models.ApiKeyTypeUpload) {
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
	if !utility.IsPathSafe(destPath, config.GoFiBaseDir) {
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
		if !utility.IsPathSafe(publicPath, config.GoFiBaseDir) {
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
		if !utility.IsTokenValid(c, models.ApiKeyTypeDownload) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}

		// 安全检查：确保路径不会逃逸
		// Security check: ensure the path does not escape
		if !utility.IsPathSafe(privatePath, config.GoFiBaseDir) {
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

// FileEntry 描述目录中的一个文件 / FileEntry describes one file in a bucket
type FileEntry struct {
	Name    string    `json:"name"`
	Dir     string    `json:"dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// ListFiles godoc
//
//	@Summary		List files
//	@Description	Lists files in the public and/or private directory as JSON. Requires a 'list' type token. Use ?dir=public or ?dir=private to restrict the listing to one bucket.
//	@Tags			Files
//	@Produce		json
//	@Param		dir		query	string	false	"Bucket to list: 'public' or 'private' (default: both)"	Enums(public, private)
//	@Security		ApiKeyAuth
//	@Success		200	{object}	object{files=[]FileEntry}
//	@Failure		400	{object}	object{error=string}
//	@Failure		401	{object}	object{error=string}
//	@Failure		500	{object}	object{error=string}
//	@Router			/files [get]
//
// ListFiles 返回文件列表 / ListFiles returns a JSON listing of stored files
func ListFiles(c *gin.Context) {
	cfg, _ := c.Get("config")
	config := cfg.(*config.Config)

	if !utility.IsTokenValid(c, models.ApiKeyTypeList) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	dirs, ok := parseDirParam(c)
	if !ok {
		return
	}

	files := []FileEntry{}
	for _, dir := range dirs {
		entries, err := listBucket(filepath.Join(config.GoFiBaseDir, dir), dir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list files: " + err.Error()})
			return
		}
		files = append(files, entries...)
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].Name != files[j].Name {
			return files[i].Name < files[j].Name
		}
		return files[i].Dir < files[j].Dir
	})

	c.JSON(http.StatusOK, gin.H{"files": files})
}

// DeleteFile godoc
//
//	@Summary		Delete a file
//	@Description	Deletes a file. Requires a 'delete' type token. When the same name exists in both buckets, ?dir=public or ?dir=private is required, otherwise the request is rejected as ambiguous. Enabled short links pointing at the deleted file are disabled.
//	@Tags			Files
//	@Produce		json
//	@Param		filename	path	string	true	"Filename"
//	@Param		dir			query	string	false	"Bucket to delete from: 'public' or 'private'"	Enums(public, private)
//	@Security		ApiKeyAuth
//	@Success		200	{object}	object{message=string,filename=string,dir=string,disabled_short_links=int}
//	@Failure		400	{object}	object{error=string}
//	@Failure		401	{object}	object{error=string}
//	@Failure		404	{object}	object{error=string}
//	@Failure		409	{object}	object{error=string}
//	@Failure		500	{object}	object{error=string}
//	@Router			/{filename} [delete]
//
// DeleteFile 删除一个文件 / DeleteFile deletes a stored file
func DeleteFile(c *gin.Context) {
	cfg, _ := c.Get("config")
	config := cfg.(*config.Config)

	if !utility.IsTokenValid(c, models.ApiKeyTypeDelete) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	filename := filepath.Base(c.Param("filename"))

	dirs, ok := parseDirParam(c)
	if !ok {
		return
	}

	// 定位目标：显式 dir 只看一个桶，否则两个桶都检查
	// Locate target: explicit dir checks one bucket, otherwise check both
	candidates := []string{}
	for _, dir := range dirs {
		path := filepath.Join(config.GoFiBaseDir, dir, filename)
		if !utility.IsPathSafe(path, config.GoFiBaseDir) {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			candidates = append(candidates, dir)
		}
	}

	switch len(candidates) {
	case 0:
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	case 1:
		// 唯一命中，继续删除 / Exactly one hit, proceed
	default:
		// 同名文件同时存在于 public/ 和 private/，拒绝二义删除
		// Same name exists in both buckets; refuse the ambiguous delete
		c.JSON(http.StatusConflict, gin.H{"error": "File exists in both public and private; retry with ?dir=public or ?dir=private"})
		return
	}

	dir := candidates[0]
	targetPath := filepath.Join(config.GoFiBaseDir, dir, filename)
	if err := os.Remove(targetPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete file: " + err.Error()})
		return
	}

	// 软禁用指向该文件的短链接，避免同名新文件让旧链接“复活”
	// Soft-disable short links to this file so a same-name upload cannot resurrect them
	isPrivate := dir == "private"
	result := database.DB.Model(&models.ShortLink{}).
		Where("original_filename = ? AND is_private = ? AND is_enabled = ?", filename, isPrivate, true).
		Update("is_enabled", false)
	disabled := result.RowsAffected
	if result.Error != nil {
		// 文件已删；链接清理失败如实上报，不假装全部成功
		// File is gone; report the cleanup failure honestly instead of faking success
		c.JSON(http.StatusOK, gin.H{
			"message":              "File deleted, but failed to disable its short links",
			"filename":             filename,
			"dir":                  dir,
			"disabled_short_links": 0,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":              "File deleted",
		"filename":             filename,
		"dir":                  dir,
		"disabled_short_links": disabled,
	})
}

// parseDirParam 解析 ?dir= 查询参数 / parseDirParam parses the ?dir= query parameter
// 返回要操作的桶列表；参数非法时写出 400 并返回 ok=false
// Returns the buckets to operate on; writes 400 and returns ok=false on a bad value
func parseDirParam(c *gin.Context) (dirs []string, ok bool) {
	switch c.Query("dir") {
	case "":
		return []string{"public", "private"}, true
	case "public":
		return []string{"public"}, true
	case "private":
		return []string{"private"}, true
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid dir parameter: must be 'public' or 'private'"})
		return nil, false
	}
}

// listBucket 读取一个桶内的普通文件 / listBucket reads regular files in one bucket
func listBucket(path, dir string) ([]FileEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	files := []FileEntry{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, FileEntry{
			Name:    info.Name(),
			Dir:     dir,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return files, nil
}
