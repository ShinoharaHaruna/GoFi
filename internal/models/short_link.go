package models

import "time"

// ShortLink 对应于数据库中的 short_links 表
// ShortLink corresponds to the short_links table in the database
type ShortLink struct {
	ID               uint   `gorm:"primaryKey"`
	ShortCode        string `gorm:"type:varchar(20);uniqueIndex;not null"`
	OriginalFilename string `gorm:"type:varchar(255);not null"`
	// A pointer preserves an explicit false during INSERT; a zero-value bool is
	// omitted in favor of default:true, misclassifying public links as private.
	IsPrivate *bool     `gorm:"not null;default:true"`
	IsEnabled bool      `gorm:"not null;default:true"` // 控制此短链接是否启用 / Controls if this short link is enabled
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

// RequiresDownloadToken preserves the conservative private default for legacy rows.
func (s ShortLink) RequiresDownloadToken() bool {
	return s.IsPrivate == nil || *s.IsPrivate
}
