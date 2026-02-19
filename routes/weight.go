package routes

import (
	"context"
	"net/http"
	"shapee-go/config"
	"shapee-go/db"
	"shapee-go/services"
	"strconv"
	"time"

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
			if err == db.ErrWeightUpdateLimitReached {
				c.JSON(http.StatusBadRequest, gin.H{"error": "一天最多只能更新一次体重"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// AddWeightRecord 已经更新了 ticket 的 current_weight，这里只需要获取最新的 ticket 信息
		ticket, err := db.GetTicket(c.Request.Context(), ticketID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// 使用独立的 context，避免请求完成后 context 被取消
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			services.GenerateDailyPlan(ctx, cfg, ticketID, req.Weight)
		}()

		c.JSON(http.StatusOK, gin.H{
			"weight_record": record,
			"ticket":        ticket,
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
