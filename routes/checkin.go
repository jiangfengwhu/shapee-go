package routes

import (
	"keepy-go/db"
	"net/http"

	"github.com/gin-gonic/gin"
)

func CheckinRoutes(r gin.IRouter) {
	r.POST("/checkin", func(c *gin.Context) {
		ticketID, _ := c.Get("ticket_id")

		record, err := db.Checkin(c.Request.Context(), ticketID.(string))
		if err != nil {
			if err == db.ErrAlreadyCheckedIn {
				c.JSON(http.StatusConflict, gin.H{"error": "今天已经签到过了"})
				return
			}
			if err == db.ErrTicketNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "Ticket not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"checkin": record,
			"message": "签到成功，获得10次调用额度！",
		})
	})
}
