package services

import (
	"context"
	"encoding/json"
	"fmt"
	"keepy-go/config"
	"keepy-go/db"
	"keepy-go/llm"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const plannerPrompt = `你是一位专业的减肥营养师和健身教练。请根据用户的当前体重，为其制定今天的个性化饮食和锻炼计划。

用户当前体重：%.1f kg

制定规则：
1. 三餐饮食计划必须科学合理，总热量控制在适合减肥的范围内
2. 体重越大，基础代谢越高，热量缺口要合理（每日亏空300-500卡为宜）
3. 体重>90kg：运动以低冲击为主（快走、游泳、椭圆机），避免跑步等高冲击运动
4. 体重70-90kg：可适当加入慢跑、骑行等中等冲击运动
5. 体重<70kg：可进行各类运动，包括HIIT
6. 所有推荐的食物和运动都要具体、可执行，使用中国常见食材
7. 锻炼安排1-2次，时间合理`

var planResponseSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"meals": map[string]interface{}{
			"type":     "array",
			"minItems": int64(3),
			"maxItems": int64(3),
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{"type": "string", "enum": []string{"breakfast", "lunch", "dinner"}},
					"time":           map[string]interface{}{"type": "string", "description": "用餐时间，格式 HH:MM"},
					"title":          map[string]interface{}{"type": "string", "description": "餐名"},
					"total_calories": map[string]interface{}{"type": "integer", "description": "总热量(卡)"},
					"tips":           map[string]interface{}{"type": "string", "description": "饮食建议"},
					"foods": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":     map[string]interface{}{"type": "string"},
								"amount":   map[string]interface{}{"type": "string"},
								"calories": map[string]interface{}{"type": "integer"},
							},
							"required":             []string{"name", "amount", "calories"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"type", "time", "title", "foods", "total_calories", "tips"},
				"additionalProperties": false,
			},
		},
		"exercises": map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title":            map[string]interface{}{"type": "string", "description": "运动名称"},
					"time":             map[string]interface{}{"type": "string", "description": "运动时间，格式 HH:MM"},
					"duration_minutes": map[string]interface{}{"type": "integer", "description": "时长(分钟)"},
					"calories_burn":    map[string]interface{}{"type": "integer", "description": "消耗热量(卡)"},
					"description":      map[string]interface{}{"type": "string", "description": "具体动作描述"},
					"tips":             map[string]interface{}{"type": "string", "description": "运动建议"},
				},
				"required":             []string{"title", "time", "duration_minutes", "calories_burn", "description", "tips"},
				"additionalProperties": false,
			},
		},
		"daily_summary": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_calories_intake": map[string]interface{}{"type": "integer", "description": "目标摄入热量(卡)"},
				"target_calories_burn":   map[string]interface{}{"type": "integer", "description": "目标消耗热量(卡)"},
				"water_intake_ml":        map[string]interface{}{"type": "integer", "description": "建议饮水量(ml)"},
				"tips":                   map[string]interface{}{"type": "string", "description": "今日总结建议"},
			},
			"required":             []string{"target_calories_intake", "target_calories_burn", "water_intake_ml", "tips"},
			"additionalProperties": false,
		},
	},
	"required":             []string{"meals", "exercises", "daily_summary"},
	"additionalProperties": false,
}

type LLMPlanResponse struct {
	Meals        []db.Meal        `json:"meals"`
	Exercises    []db.Exercise    `json:"exercises"`
	DailySummary *db.DailySummary `json:"daily_summary"`
}

// GenerateDailyPlan 为用户生成每日计划（后台异步调用）
func GenerateDailyPlan(ctx context.Context, cfg *config.Config, ticketIDHex string, weight float64) {
	plan, err := db.CreateDailyPlan(ctx, ticketIDHex, weight)
	if err != nil {
		log.Printf("[Planner] 创建计划失败 ticket=%s: %v", ticketIDHex, err)
		return
	}

	// 如果计划已经存在且不是pending状态，跳过
	if plan.Status != db.PlanStatusPending {
		log.Printf("[Planner] 今日计划已存在 ticket=%s status=%s", ticketIDHex, plan.Status)
		return
	}

	if err := db.UpdatePlanStatus(ctx, plan.ID, db.PlanStatusGenerating, ""); err != nil {
		log.Printf("[Planner] 更新状态失败: %v", err)
		return
	}

	prompt := fmt.Sprintf(plannerPrompt, weight)

	response, err := llm.GenerateJSON(ctx, llm.ChatConfig{
		Provider:       cfg.Provider,
		OpenAIConfig:   cfg.OpenAI,
		VertexAIConfig: cfg.VertexAI,
		History: []llm.ChatMessage{
			{Role: "user", Content: prompt},
		},
		ResponseSchema: planResponseSchema,
	})
	if err != nil {
		log.Printf("[Planner] LLM调用失败 ticket=%s: %v", ticketIDHex, err)
		db.UpdatePlanStatus(ctx, plan.ID, db.PlanStatusFailed, err.Error())
		return
	}

	planData, err := parsePlanResponse(response)
	if err != nil {
		log.Printf("[Planner] 解析LLM响应失败 ticket=%s: %v", ticketIDHex, err)
		db.UpdatePlanStatus(ctx, plan.ID, db.PlanStatusFailed, "解析计划失败: "+err.Error())
		return
	}

	if err := db.UpdatePlanContent(ctx, plan.ID, planData.Meals, planData.Exercises, planData.DailySummary); err != nil {
		log.Printf("[Planner] 保存计划失败 ticket=%s: %v", ticketIDHex, err)
		db.UpdatePlanStatus(ctx, plan.ID, db.PlanStatusFailed, err.Error())
		return
	}

	if err := createPushTasksForPlan(ctx, plan.TicketID, plan.ID, planData); err != nil {
		log.Printf("[Planner] 创建推送任务失败 ticket=%s: %v", ticketIDHex, err)
	}

	log.Printf("[Planner] 计划生成完成 ticket=%s weight=%.1f", ticketIDHex, weight)
}

func parsePlanResponse(response string) (*LLMPlanResponse, error) {
	var plan LLMPlanResponse
	if err := json.Unmarshal([]byte(response), &plan); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w, raw: %s", err, truncate(response, 200))
	}

	if len(plan.Meals) == 0 {
		return nil, fmt.Errorf("empty meals in response")
	}
	return &plan, nil
}

func createPushTasksForPlan(ctx context.Context, ticketID, planID bson.ObjectID, plan *LLMPlanResponse) error {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")

	var tasks []*db.PushTask

	for _, meal := range plan.Meals {
		scheduledAt, err := parseTimeToday(today, meal.Time, loc)
		if err != nil {
			continue
		}
		// 如果时间已经过了，提前10分钟标记或跳过
		if scheduledAt.Before(time.Now()) {
			continue
		}

		mealName := mealTypeName(meal.Type)
		var foodList []string
		for _, f := range meal.Foods {
			foodList = append(foodList, f.Name)
		}

		tasks = append(tasks, &db.PushTask{
			TicketID:    ticketID,
			PlanID:      planID,
			Type:        db.PushTaskMeal,
			Title:       fmt.Sprintf("%s时间到！", mealName),
			Body:        fmt.Sprintf("今日推荐：%s（约%d卡）- %s", meal.Title, meal.TotalCalories, strings.Join(foodList, "、")),
			ScheduledAt: scheduledAt.UTC(),
		})
	}

	for _, exercise := range plan.Exercises {
		scheduledAt, err := parseTimeToday(today, exercise.Time, loc)
		if err != nil {
			continue
		}
		if scheduledAt.Before(time.Now()) {
			continue
		}

		tasks = append(tasks, &db.PushTask{
			TicketID:    ticketID,
			PlanID:      planID,
			Type:        db.PushTaskExercise,
			Title:       "运动时间到！💪",
			Body:        fmt.Sprintf("今日锻炼：%s %d分钟，预计消耗%d卡 - %s", exercise.Title, exercise.DurationMinutes, exercise.CaloriesBurn, exercise.Tips),
			ScheduledAt: scheduledAt.UTC(),
		})
	}

	if len(tasks) > 0 {
		return db.CreatePushTasks(ctx, tasks)
	}
	return nil
}

func parseTimeToday(dateStr, timeStr string, loc *time.Location) (time.Time, error) {
	return time.ParseInLocation("2006-01-02 15:04", dateStr+" "+timeStr, loc)
}

func mealTypeName(mealType string) string {
	switch mealType {
	case "breakfast":
		return "早餐"
	case "lunch":
		return "午餐"
	case "dinner":
		return "晚餐"
	default:
		return "用餐"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
