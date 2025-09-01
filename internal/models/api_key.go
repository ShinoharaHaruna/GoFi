package models

import "gorm.io/gorm"

// ApiKeyType 定义了 API 密钥的类型
// ApiKeyType defines the type of the API key
type ApiKeyType string

const (
	ApiKeyTypeUpload   ApiKeyType = "upload"
	ApiKeyTypeDownload ApiKeyType = "download"
	ApiKeyTypeShorten  ApiKeyType = "shorten"
)

// ApiKey 代表访问 API 的令牌
// ApiKey represents a token for accessing the API
type ApiKey struct {
	gorm.Model
	Key       string     `gorm:"type:varchar(255);uniqueIndex;not null"` // 密钥 / Key
	Type      ApiKeyType `gorm:"type:varchar(50);not null"`              // 密钥类型 / Key Type
	IsEnabled bool       `gorm:"default:true"`                           // 是否启用 / Is Enabled
}
