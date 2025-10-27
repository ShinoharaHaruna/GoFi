package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gin-gonic/gin"
)

// generateUUIDv4 creates an RFC 4122 UUIDv4 using crypto-grade randomness
func generateUUIDv4() (string, error) {
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

// GenerateUUID godoc
//
//	@Summary		Generate a random UUIDv4
//	@Description	Return a random UUIDv4
//	@Tags			Utility
//	@Produce		json
//	@Success		200	{object}	object{uuid=string}
//	@Failure		500	{object}	object{error=string}
//	@Router			/uuid [get]
//
// GenerateUUID returns a random UUIDv4
func GenerateUUID(c *gin.Context) {
	uuid, err := generateUUIDv4()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate UUID"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"uuid": uuid})
}
