package routes

import (
	"keepy-go/db"
	"net/http"

	"github.com/gin-gonic/gin"
)

func UserRoutes(r gin.IRouter) {
	r.POST("/user/device-token", func(c *gin.Context) {
		var req struct {
			DeviceToken string `json:"device_token" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ticketID := c.GetString("ticket_id")
		if err := db.UpdateTicketDeviceToken(c.Request.Context(), ticketID, req.DeviceToken); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "设备推送token已注册"})
	})

	r.POST("/user/reminder-time", func(c *gin.Context) {
		var req struct {
			Hour   int `json:"hour" binding:"min=0,max=23"`
			Minute int `json:"minute" binding:"min=0,max=59"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请输入有效时间（hour: 0-23, minute: 0-59）"})
			return
		}

		ticketID := c.GetString("ticket_id")
		if err := db.UpdateTicketReminderTime(c.Request.Context(), ticketID, req.Hour, req.Minute); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":         "提醒时间已更新",
			"reminder_hour":   req.Hour,
			"reminder_minute": req.Minute,
		})
	})

	r.GET("/user/profile", func(c *gin.Context) {
		ticketID := c.GetString("ticket_id")

		ticket, err := db.GetTicket(c.Request.Context(), ticketID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"user": ticket})
	})
}
