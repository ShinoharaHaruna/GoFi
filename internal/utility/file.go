package utility

import (
	"path/filepath"
	"strings"
)

// IsPathSafe 检查目标路径是否在基础目录内
// IsPathSafe checks if the target path is within the base directory
func IsPathSafe(targetPath, baseDir string) bool {
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
