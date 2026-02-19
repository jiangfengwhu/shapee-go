package routes

import (
	"net/http"
	"shapee-go/db"

	"github.com/gin-gonic/gin"
)

func PlanRoutes(r gin.IRouter) {
	// 获取今日计划（含生成状态）
	r.GET("/plan/today", func(c *gin.Context) {
		ticketID := c.GetString("ticket_id")

		plan, err := db.GetTodayPlan(c.Request.Context(), ticketID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if plan == nil {
			hasWeight, _ := db.HasWeightRecordToday(c.Request.Context(), ticketID)
			c.JSON(http.StatusOK, gin.H{
				"plan":           nil,
				"status":         "no_plan",
				"weight_updated": hasWeight,
				"message":        "今日尚未生成计划，请先更新体重",
			})
			return
		}

		pushTasks, _ := db.GetTodayPushTasks(c.Request.Context(), ticketID)

		c.JSON(http.StatusOK, gin.H{
			"plan":       plan,
			"status":     plan.Status,
			"push_tasks": pushTasks,
		})
	})

	// 查询计划生成状态
	r.GET("/plan/status", func(c *gin.Context) {
		ticketID := c.GetString("ticket_id")

		plan, err := db.GetTodayPlan(c.Request.Context(), ticketID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if plan == nil {
			c.JSON(http.StatusOK, gin.H{
				"status":  "no_plan",
				"message": "今日尚未生成计划",
			})
			return
		}

		resp := gin.H{
			"status": plan.Status,
			"date":   plan.Date,
			"weight": plan.Weight,
		}

		switch plan.Status {
		case db.PlanStatusPending:
			resp["message"] = "计划等待生成中..."
		case db.PlanStatusGenerating:
			resp["message"] = "正在为你生成个性化计划..."
		case db.PlanStatusReady:
			resp["message"] = "计划已就绪"
		case db.PlanStatusFailed:
			resp["message"] = "计划生成失败，请重新更新体重"
			resp["error"] = plan.ErrorMessage
		}

		c.JSON(http.StatusOK, resp)
	})
}
