package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"shapee-go/config"
	"shapee-go/db"
	"shapee-go/llm"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const plannerPromptHead = `角色设定：
请你扮演一位拥有十年以上经验的资深注册营养师（RD）和国际认证私人教练（ACE/ACSM）。你需要以科学、健康、不反弹为核心原则，结合运动医学和营养学知识，为我定制一份专属的减脂计划。

我的核心目标：
我目前的体重是：%.1f kg，%s，请帮我在安全健康的前提下，规划一个合理的减重周期和阶段性目标。

我的基础信息及偏好：
%s

制定规则：
1. 估算我的 BMI、基础代谢率（BMR）和每日总能量消耗（TDEE），评估我的目标体重是否合理，并科学预测达成该目标大概需要几个月
2. 饮食方案（七分吃）： 设定每日热量摄入目标和缺口，给出宏量营养素（碳水、蛋白质、脂肪）的科学配比。请结合我的饮食偏好。
3. 运动方案（三分练）： 结合我的运动偏好、已有健身器械、时间和健康状态（避开可能导致损伤的动作），运动总时长不能超过我规定的锻炼时长（如果有）。
4. 日常干预与避坑指南： 针对我的作息或健康状态，给出睡眠、饮水量等生活方式建议，并列出 1-3 条减脂期最容易踩的坑。
5. 语气： 专业、严谨但充满鼓励，像一位现实中真正关心我健康的教练。
`

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
					"type":           map[string]interface{}{"type": "string", "enum": []string{"breakfast", "lunch", "dinner"}},
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
				"target_calories_intake":  map[string]interface{}{"type": "integer", "description": "目标摄入热量(卡)"},
				"target_calories_burn":    map[string]interface{}{"type": "integer", "description": "目标消耗热量(卡)"},
				"water_intake_ml":         map[string]interface{}{"type": "integer", "description": "建议饮水量(ml)"},
				"target_steps":            map[string]interface{}{"type": "integer", "description": "建议今日步数"},
				"estimated_weeks_to_goal": map[string]interface{}{"type": "integer", "description": "预计多少周可达目标体重，未设目标时为0"},
				"tips":                    map[string]interface{}{"type": "string", "description": "今日总结建议，换行隔开"},
			},
			"required":             []string{"target_calories_intake", "target_calories_burn", "water_intake_ml", "target_steps", "estimated_weeks_to_goal", "tips"},
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

// buildPlanContext 组装用户基础信息、近三日计划与体重趋势，用于生成更个性化的计划
func buildPlanContext(ctx context.Context, ticketIDHex string) string {
	var b strings.Builder

	ticket, err := db.GetTicket(ctx, ticketIDHex)
	if err != nil || ticket == nil {
		return ""
	}

	// 用户基础信息与偏好
	if ticket.BasicInfo != "" {
		b.WriteString("【我的基础信息】\n")
		b.WriteString(ticket.BasicInfo)
		b.WriteString("\n\n")
	}
	if ticket.DietaryAndExercisePreferences != "" {
		b.WriteString("【饮食与运动偏好】\n")
		b.WriteString(ticket.DietaryAndExercisePreferences)
		b.WriteString("\n\n")
	}
	if ticket.HealthIssues != "" {
		b.WriteString("【健康问题/注意事项】\n")
		b.WriteString(ticket.HealthIssues)
		b.WriteString("\n\n")
	}
	if ticket.WorkType != "" {
		b.WriteString("【工作类型】\n")
		b.WriteString(ticket.WorkType)
		b.WriteString("\n\n")
	}
	if ticket.ExecutionConstraints != "" {
		b.WriteString("【备餐与锻炼时长限制】\n")
		b.WriteString(ticket.ExecutionConstraints)
		b.WriteString("\n\n")
	}
	if ticket.PastFailureExperience != "" {
		b.WriteString("【过往减肥失败经历】\n")
		b.WriteString(ticket.PastFailureExperience)
		b.WriteString("\n\n")
	}
	if ticket.FitnessEquipment != "" {
		b.WriteString("【已有健身器械】\n")
		b.WriteString(ticket.FitnessEquipment)
		b.WriteString("\n\n")
	}

	// 近七日计划（仅已就绪的，供参考以保持连贯或避免重复）
	recentPlans, _ := db.GetRecentPlans(ctx, ticketIDHex, 7)
	if len(recentPlans) > 0 {
		b.WriteString("【近七日计划摘要（供参考，请在此基础上优化今日计划）】\n")
		for _, p := range recentPlans {
			b.WriteString(fmt.Sprintf("- %s（体重 %.1f kg）", p.Date, p.Weight))
			if p.DailySummary != nil {
				b.WriteString(fmt.Sprintf("：摄入目标 %d 卡，步数 %d", p.DailySummary.TargetCaloriesIntake, p.DailySummary.TargetSteps))
			}
			b.WriteString("\n")
			for _, m := range p.Meals {
				b.WriteString(fmt.Sprintf("  %s %s %s %d卡\n", m.Type, m.Time, m.Title, m.TotalCalories))
			}
			for _, e := range p.Exercises {
				b.WriteString(fmt.Sprintf("  运动 %s %s %d分钟 %d卡\n", e.Title, e.Time, e.DurationMinutes, e.CaloriesBurn))
			}
		}
		b.WriteString("\n")
	}

	// 近期体重变化趋势（最多取最近 7 条，weight_history 已按时间倒序）
	const weightTrendLimit = 7
	if len(ticket.WeightHistory) > 0 {
		b.WriteString("【近期体重变化趋势】\n")
		n := weightTrendLimit
		if n > len(ticket.WeightHistory) {
			n = len(ticket.WeightHistory)
		}
		for i := 0; i < n; i++ {
			r := ticket.WeightHistory[i]
			b.WriteString(fmt.Sprintf("%s %.1f kg", r.Date, r.Weight))
			if i < n-1 {
				b.WriteString(" → ")
			}
		}
		b.WriteString("\n\n")
	}

	return b.String()
}

// GenerateDailyPlan 为用户生成每日计划（后台异步调用）
// targetWeight 为用户目标体重(kg)，未设置时传 0
func GenerateDailyPlan(ctx context.Context, cfg *config.Config, ticketIDHex string, weight, targetWeight float64) {
	plan, err := db.CreateDailyPlan(ctx, ticketIDHex, weight)
	if err != nil {
		log.Printf("[Planner] 创建计划失败 ticket=%s: %v", ticketIDHex, err)
		return
	}

	// 如果计划已经存在且不是 pending 状态，默认跳过；开发测试开启「一天多次更新体重」时则直接覆盖
	overwriteWeight := 0.0
	if plan.Status != db.PlanStatusPending {
		if cfg.AllowMultipleWeightUpdatesPerDay {
			log.Printf("[Planner] 今日计划已存在，直接覆盖 ticket=%s status=%s", ticketIDHex, plan.Status)
			overwriteWeight = weight
		} else {
			log.Printf("[Planner] 今日计划已存在 ticket=%s status=%s", ticketIDHex, plan.Status)
			return
		}
	}

	if err := db.UpdatePlanStatus(ctx, plan.ID, db.PlanStatusGenerating, ""); err != nil {
		log.Printf("[Planner] 更新状态失败: %v", err)
		return
	}

	targetLine := ""
	if targetWeight > 0 {
		targetLine = fmt.Sprintf("我的目标体重是：%.1f kg\n", targetWeight)
	}

	contextBlock := buildPlanContext(ctx, ticketIDHex)
	prompt := fmt.Sprintf(plannerPromptHead, weight, targetLine, contextBlock)

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

	if err := db.UpdatePlanContent(ctx, plan.ID, planData.Meals, planData.Exercises, planData.DailySummary, overwriteWeight); err != nil {
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
