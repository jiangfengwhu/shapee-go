package routes

import (
	"keepy-go/config"
	"keepy-go/db"
	"keepy-go/services"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func WeightRoutes(r gin.IRouter, cfg *config.Config) {
	r.POST("/weight/update", func(c *gin.Context) {
		var req struct {
			Weight float64 `json:"weight" binding:"required,gt=20,lt=500"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请输入有效体重（20-500kg）"})
			return
		}

		ticketID := c.GetString("ticket_id")

		record, err := db.AddWeightRecord(c.Request.Context(), ticketID, req.Weight)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		ticket, err := db.UpdateTicketWeight(c.Request.Context(), ticketID, req.Weight)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		go services.GenerateDailyPlan(c.Request.Context(), cfg, ticketID, req.Weight)

		c.JSON(http.StatusOK, gin.H{
			"weight_record": record,
			"user":          ticket,
			"message":       "体重已更新，正在为你生成今日饮食和锻炼计划...",
		})
	})

	r.GET("/weight/history", func(c *gin.Context) {
		ticketID := c.GetString("ticket_id")

		limitStr := c.DefaultQuery("limit", "30")
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			limit = 30
		}
		if limit > 365 {
			limit = 365
		}

		records, err := db.GetWeightHistory(c.Request.Context(), ticketID, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"records": records,
			"count":   len(records),
		})
	})
}
