package handlers

import (
	"net/http"

	"github.com/ShinoharaHaruna/GoFi/internal/utility"
	"github.com/gin-gonic/gin"
)

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
	uuid, err := utility.GenerateUUIDv4()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate UUID"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"uuid": uuid})
}
