package services

import (
	"context"
	"encoding/json"
	"fmt"
	"shapee-go/config"
	"shapee-go/llm"
	"testing"
	"time"
)

func testPlannerWithProvider(t *testing.T, provider string) {
	t.Helper()

	cfg, err := config.Load("../config.json")
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	weight := 82.5
	prompt := fmt.Sprintf(plannerPrompt, weight)

	response, err := llm.GenerateJSON(ctx, llm.ChatConfig{
		Provider:       provider,
		OpenAIConfig:   cfg.OpenAI,
		VertexAIConfig: cfg.VertexAI,
		History: []llm.ChatMessage{
			{Role: "user", Content: prompt},
		},
		ResponseSchema: planResponseSchema,
	})
	if err != nil {
		t.Fatalf("GenerateJSON 调用失败: %v", err)
	}

	t.Logf("原始响应:\n%s", response)

	plan, err := parsePlanResponse(response)
	if err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if len(plan.Meals) < 3 {
		t.Errorf("期望至少 3 餐，实际 %d 餐", len(plan.Meals))
	}
	mealTypes := map[string]bool{}
	for _, meal := range plan.Meals {
		mealTypes[meal.Type] = true
		if meal.Title == "" {
			t.Error("餐名不应为空")
		}
		if meal.Time == "" {
			t.Error("用餐时间不应为空")
		}
		if len(meal.Foods) == 0 {
			t.Errorf("%s 的食物列表不应为空", meal.Title)
		}
		if meal.TotalCalories <= 0 {
			t.Errorf("%s 的总热量应大于0，实际 %d", meal.Title, meal.TotalCalories)
		}
		for _, food := range meal.Foods {
			if food.Name == "" || food.Amount == "" {
				t.Errorf("食物信息不完整: %+v", food)
			}
		}
	}
	for _, expected := range []string{"breakfast", "lunch", "dinner"} {
		if !mealTypes[expected] {
			t.Errorf("缺少餐类型: %s", expected)
		}
	}

	if len(plan.Exercises) == 0 {
		t.Error("运动计划不应为空")
	}
	for _, ex := range plan.Exercises {
		if ex.Title == "" {
			t.Error("运动名称不应为空")
		}
		if ex.DurationMinutes <= 0 {
			t.Errorf("运动时长应大于0: %s", ex.Title)
		}
		if ex.CaloriesBurn <= 0 {
			t.Errorf("消耗热量应大于0: %s", ex.Title)
		}
	}

	if plan.DailySummary == nil {
		t.Fatal("daily_summary 不应为空")
	}
	if plan.DailySummary.TargetCaloriesIntake <= 0 {
		t.Errorf("目标摄入热量应大于0，实际 %d", plan.DailySummary.TargetCaloriesIntake)
	}
	if plan.DailySummary.WaterIntakeML <= 0 {
		t.Errorf("饮水量应大于0，实际 %d", plan.DailySummary.WaterIntakeML)
	}

	pretty, _ := json.MarshalIndent(plan, "", "  ")
	t.Logf("解析后的计划:\n%s", pretty)
}

func TestPlannerGenerateJSON_OpenAI(t *testing.T) {
	testPlannerWithProvider(t, llm.ProviderOpenAI)
}

func TestPlannerGenerateJSON_VertexAI(t *testing.T) {
	testPlannerWithProvider(t, llm.ProviderVertexAI)
}
