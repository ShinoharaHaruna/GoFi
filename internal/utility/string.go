package utility

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/ShinoharaHaruna/GoFi/internal/database"
	"github.com/ShinoharaHaruna/GoFi/internal/models"
)

// GenerateRandomString 生成指定长度的随机十六进制字符串
// GenerateRandomString generates a random hex string of the specified length
func GenerateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GenerateUUIDv4 creates an RFC 4122 UUIDv4 using crypto-grade randomness
func GenerateUUIDv4() (string, error) {
	uuidBytes := make([]byte, 16)
	if _, err := rand.Read(uuidBytes); err != nil {
		return "", err
	}

	// Set version and variant bits to ensure RFC compatibility
	uuidBytes[6] = (uuidBytes[6] & 0x0f) | 0x40
	uuidBytes[8] = (uuidBytes[8] & 0x3f) | 0x80

	hexStr := hex.EncodeToString(uuidBytes)
	return hexStr[0:8] + "-" +
		hexStr[8:12] + "-" +
		hexStr[12:16] + "-" +
		hexStr[16:20] + "-" +
		hexStr[20:32], nil
}

// GenerateUniqueShortCode 生成一个在数据库中唯一的短代码
// GenerateUniqueShortCode generates a short code that is unique in the database
func GenerateUniqueShortCode(length int) (string, error) {
	for range 10 { // 尝试 10 次以避免无限循环 / Try 10 times to avoid an infinite loop
		code, err := GenerateRandomString(length)
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
