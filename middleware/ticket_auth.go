package middleware

import (
	"net/http"
	"shapee-go/util"

	"github.com/gin-gonic/gin"
)

func TicketAuth(c *gin.Context) {
	encryptedTicketID := c.GetHeader("X-Ticket-ID")
	if encryptedTicketID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing X-Ticket-ID header"})
		c.Abort()
		return
	}

	ticketID, err := util.DecryptTicketID(encryptedTicketID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid Ticket ID"})
		c.Abort()
		return
	}

	// Store the decrypted ticket ID in the context
	c.Set("ticket_id", ticketID)
	c.Next()
}
