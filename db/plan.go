package db

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	planCollection     = "daily_plans"
	pushTaskCollection = "push_tasks"
)

// --- Daily Plan ---

type PlanStatus string

const (
	PlanStatusPending    PlanStatus = "pending"
	PlanStatusGenerating PlanStatus = "generating"
	PlanStatusReady      PlanStatus = "ready"
	PlanStatusFailed     PlanStatus = "failed"
)

type MealFood struct {
	Name     string `bson:"name" json:"name"`
	Amount   string `bson:"amount" json:"amount"`
	Calories int    `bson:"calories" json:"calories"`
}

type Meal struct {
	Type          string     `bson:"type" json:"type"`
	Time          string     `bson:"time" json:"time"`
	Title         string     `bson:"title" json:"title"`
	Foods         []MealFood `bson:"foods" json:"foods"`
	TotalCalories int        `bson:"total_calories" json:"total_calories"`
	Tips          string     `bson:"tips" json:"tips"`
}

type Exercise struct {
	Title           string `bson:"title" json:"title"`
	Time            string `bson:"time" json:"time"`
	DurationMinutes int    `bson:"duration_minutes" json:"duration_minutes"`
	CaloriesBurn    int    `bson:"calories_burn" json:"calories_burn"`
	Description     string `bson:"description" json:"description"`
	Tips            string `bson:"tips" json:"tips"`
}

type DailySummary struct {
	TargetCaloriesIntake  int    `bson:"target_calories_intake" json:"target_calories_intake"`
	TargetCaloriesBurn   int    `bson:"target_calories_burn" json:"target_calories_burn"`
	WaterIntakeML        int    `bson:"water_intake_ml" json:"water_intake_ml"`
	TargetSteps          int    `bson:"target_steps" json:"target_steps"`           // 建议每日步数
	EstimatedWeeksToGoal int    `bson:"estimated_weeks_to_goal" json:"estimated_weeks_to_goal"` // 预计多少周可达目标体重，未设目标时为 0
	Tips                 string `bson:"tips" json:"tips"`
}

type DailyPlan struct {
	ID           bson.ObjectID `bson:"_id" json:"id"`
	TicketID     bson.ObjectID `bson:"ticket_id" json:"ticket_id"`
	Date         string        `bson:"date" json:"date"`
	Weight       float64       `bson:"weight" json:"weight"`
	Status       PlanStatus    `bson:"status" json:"status"`
	Meals        []Meal        `bson:"meals,omitempty" json:"meals,omitempty"`
	Exercises    []Exercise    `bson:"exercises,omitempty" json:"exercises,omitempty"`
	DailySummary *DailySummary `bson:"daily_summary,omitempty" json:"daily_summary,omitempty"`
	ErrorMessage string        `bson:"error_message,omitempty" json:"error_message,omitempty"`
	CreatedAt    time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time     `bson:"updated_at" json:"updated_at"`
}

func planColl() (*mongo.Collection, error) {
	if client == nil {
		return nil, ErrClientNotInitialized
	}
	return getDatabase().Collection(planCollection), nil
}

func EnsurePlanIndexes(ctx context.Context) error {
	coll, err := planColl()
	if err != nil {
		return err
	}
	_, err = coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "ticket_id", Value: 1},
			{Key: "date", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	return err
}

func CreateDailyPlan(ctx context.Context, ticketIDHex string, weight float64) (*DailyPlan, error) {
	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return nil, ErrInvalidTicketID
	}

	coll, err := planColl()
	if err != nil {
		return nil, err
	}

	today := getTodayDateString()
	now := time.Now().UTC()

	plan := &DailyPlan{
		ID:        bson.NewObjectID(),
		TicketID:  oid,
		Date:      today,
		Weight:    weight,
		Status:    PlanStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = coll.InsertOne(ctx, plan)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return GetTodayPlan(ctx, ticketIDHex)
		}
		return nil, err
	}
	return plan, nil
}

func GetTodayPlan(ctx context.Context, ticketIDHex string) (*DailyPlan, error) {
	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return nil, ErrInvalidTicketID
	}

	coll, err := planColl()
	if err != nil {
		return nil, err
	}

	today := getTodayDateString()
	var plan DailyPlan
	err = coll.FindOne(ctx, bson.M{"ticket_id": oid, "date": today}).Decode(&plan)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &plan, nil
}

// GetRecentPlans 获取过去 N 天的计划（不含今天），按日期倒序（昨天在前）
func GetRecentPlans(ctx context.Context, ticketIDHex string, days int) ([]*DailyPlan, error) {
	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return nil, ErrInvalidTicketID
	}
	if days <= 0 {
		return nil, nil
	}

	coll, err := planColl()
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)
	var dates []string
	for i := 1; i <= days; i++ {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		dates = append(dates, d)
	}

	cursor, err := coll.Find(ctx, bson.M{
		"ticket_id": oid,
		"date":      bson.M{"$in": dates},
		"status":    PlanStatusReady,
	}, options.Find().SetSort(bson.D{{Key: "date", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var plans []*DailyPlan
	if err := cursor.All(ctx, &plans); err != nil {
		return nil, err
	}
	return plans, nil
}

func UpdatePlanStatus(ctx context.Context, planID bson.ObjectID, status PlanStatus, errorMsg string) error {
	coll, err := planColl()
	if err != nil {
		return err
	}

	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"updated_at": time.Now().UTC(),
		},
	}
	if errorMsg != "" {
		update["$set"].(bson.M)["error_message"] = errorMsg
	}

	_, err = coll.UpdateByID(ctx, planID, update)
	return err
}

// UpdatePlanContent 更新计划内容。overwriteWeight > 0 时同时更新计划的 weight 字段（覆盖今日计划时用）。
func UpdatePlanContent(ctx context.Context, planID bson.ObjectID, meals []Meal, exercises []Exercise, summary *DailySummary, overwriteWeight float64) error {
	coll, err := planColl()
	if err != nil {
		return err
	}

	setFields := bson.M{
		"meals":         meals,
		"exercises":     exercises,
		"daily_summary": summary,
		"status":        PlanStatusReady,
		"updated_at":    time.Now().UTC(),
	}
	if overwriteWeight > 0 {
		setFields["weight"] = overwriteWeight
	}

	_, err = coll.UpdateByID(ctx, planID, bson.M{"$set": setFields})
	return err
}

// --- Push Task ---

type PushTaskStatus string

const (
	PushTaskScheduled PushTaskStatus = "scheduled"
	PushTaskSent      PushTaskStatus = "sent"
	PushTaskFailed    PushTaskStatus = "failed"
)

type PushTaskType string

const (
	PushTaskMeal           PushTaskType = "meal"
	PushTaskExercise       PushTaskType = "exercise"
	PushTaskWeightReminder PushTaskType = "weight_reminder"
)

type PushTask struct {
	ID          bson.ObjectID  `bson:"_id" json:"id"`
	TicketID    bson.ObjectID  `bson:"ticket_id" json:"ticket_id"`
	PlanID      bson.ObjectID  `bson:"plan_id,omitempty" json:"plan_id,omitempty"`
	Type        PushTaskType   `bson:"type" json:"type"`
	Title       string         `bson:"title" json:"title"`
	Body        string         `bson:"body" json:"body"`
	ScheduledAt time.Time      `bson:"scheduled_at" json:"scheduled_at"`
	Status      PushTaskStatus `bson:"status" json:"status"`
	SentAt      *time.Time     `bson:"sent_at,omitempty" json:"sent_at,omitempty"`
	Error       string         `bson:"error,omitempty" json:"error,omitempty"`
	CreatedAt   time.Time      `bson:"created_at" json:"created_at"`
}

func pushTaskColl() (*mongo.Collection, error) {
	if client == nil {
		return nil, ErrClientNotInitialized
	}
	return getDatabase().Collection(pushTaskCollection), nil
}

func EnsurePushTaskIndexes(ctx context.Context) error {
	coll, err := pushTaskColl()
	if err != nil {
		return err
	}
	_, err = coll.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "scheduled_at", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "ticket_id", Value: 1}, {Key: "type", Value: 1}, {Key: "scheduled_at", Value: 1}}},
	})
	return err
}

func CreatePushTask(ctx context.Context, task *PushTask) error {
	coll, err := pushTaskColl()
	if err != nil {
		return err
	}

	if task.ID.IsZero() {
		task.ID = bson.NewObjectID()
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	task.Status = PushTaskScheduled

	_, err = coll.InsertOne(ctx, task)
	return err
}

func CreatePushTasks(ctx context.Context, tasks []*PushTask) error {
	if len(tasks) == 0 {
		return nil
	}

	coll, err := pushTaskColl()
	if err != nil {
		return err
	}

	docs := make([]interface{}, len(tasks))
	now := time.Now().UTC()
	for i, t := range tasks {
		if t.ID.IsZero() {
			t.ID = bson.NewObjectID()
		}
		if t.CreatedAt.IsZero() {
			t.CreatedAt = now
		}
		t.Status = PushTaskScheduled
		docs[i] = t
	}

	_, err = coll.InsertMany(ctx, docs)
	return err
}

// GetDuePushTasks 获取所有到期但未发送的推送任务
func GetDuePushTasks(ctx context.Context, limit int) ([]*PushTask, error) {
	coll, err := pushTaskColl()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	opts := options.Find().
		SetSort(bson.D{{Key: "scheduled_at", Value: 1}}).
		SetLimit(int64(limit))

	cursor, err := coll.Find(ctx, bson.M{
		"status":       PushTaskScheduled,
		"scheduled_at": bson.M{"$lte": now},
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var tasks []*PushTask
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func MarkPushTaskSent(ctx context.Context, taskID bson.ObjectID) error {
	coll, err := pushTaskColl()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	_, err = coll.UpdateByID(ctx, taskID, bson.M{
		"$set": bson.M{"status": PushTaskSent, "sent_at": now},
	})
	return err
}

func MarkPushTaskFailed(ctx context.Context, taskID bson.ObjectID, errMsg string) error {
	coll, err := pushTaskColl()
	if err != nil {
		return err
	}

	_, err = coll.UpdateByID(ctx, taskID, bson.M{
		"$set": bson.M{"status": PushTaskFailed, "error": errMsg},
	})
	return err
}

// CountWeightRemindersToday 统计今天给用户发了几条体重提醒
func CountWeightRemindersToday(ctx context.Context, ticketID bson.ObjectID) (int64, error) {
	coll, err := pushTaskColl()
	if err != nil {
		return 0, err
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).UTC()
	endOfDay := startOfDay.Add(24 * time.Hour)

	return coll.CountDocuments(ctx, bson.M{
		"ticket_id": ticketID,
		"type":      PushTaskWeightReminder,
		"scheduled_at": bson.M{
			"$gte": startOfDay,
			"$lt":  endOfDay,
		},
	})
}

func GetTodayPushTasks(ctx context.Context, ticketIDHex string) ([]*PushTask, error) {
	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return nil, ErrInvalidTicketID
	}

	coll, err := pushTaskColl()
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).UTC()
	endOfDay := startOfDay.Add(24 * time.Hour)

	cursor, err := coll.Find(ctx, bson.M{
		"ticket_id": oid,
		"scheduled_at": bson.M{
			"$gte": startOfDay,
			"$lt":  endOfDay,
		},
	}, options.Find().SetSort(bson.D{{Key: "scheduled_at", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var tasks []*PushTask
	if err := cursor.All(ctx, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}
