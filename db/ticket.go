package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	ticketCollection              = "tickets"
	appleIAPTransactionCollection = "apple_iap_transactions"
)

var (
	ErrTicketNotFound           = errors.New("ticket not found")
	ErrInvalidTicketID          = errors.New("invalid ticket id")
	ErrClientNotInitialized     = errors.New("client not initialized")
	ErrSubscriptionExpired      = errors.New("subscription expired")
	ErrWeightUpdateLimitReached = errors.New("一天最多只能更新一次体重")
)

const (
	DefaultReminderHour   = 8
	DefaultReminderMinute = 0
)

type WeightRecord struct {
	Weight    float64   `bson:"weight" json:"weight"`
	Date      string    `bson:"date" json:"date"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

type Ticket struct {
	ID              bson.ObjectID  `bson:"_id"`
	DeviceID        string         `bson:"device_id,omitempty" json:"device_id,omitempty"`
	DeviceToken     string         `bson:"device_token,omitempty" json:"device_token,omitempty"`
	CurrentWeight   float64        `bson:"current_weight,omitempty" json:"current_weight,omitempty"`
	TargetWeight    float64        `bson:"target_weight,omitempty" json:"target_weight,omitempty"` // 目标体重
	WeightUpdatedAt *time.Time     `bson:"weight_updated_at,omitempty" json:"weight_updated_at,omitempty"`
	WeightHistory   []WeightRecord `bson:"weight_history,omitempty" json:"weight_history,omitempty"`
	ReminderHour    int            `bson:"reminder_hour" json:"reminder_hour"`
	ReminderMinute  int            `bson:"reminder_minute" json:"reminder_minute"`

	// 用户profile信息
	BasicInfo                     string `bson:"basic_info,omitempty" json:"basic_info,omitempty"`                                             // 基础信息
	DietaryAndExercisePreferences string `bson:"dietary_and_exercise_preferences,omitempty" json:"dietary_and_exercise_preferences,omitempty"` // 饮食及运动喜好
	HealthIssues                  string `bson:"health_issues,omitempty" json:"health_issues,omitempty"`                                       // 健康问题
	WorkType                      string `bson:"work_type,omitempty" json:"work_type,omitempty"`                                               // 工作类型
	ExecutionConstraints          string `bson:"execution_constraints,omitempty" json:"execution_constraints,omitempty"`                       // 备餐和锻炼时长限制
	PastFailureExperience         string `bson:"past_failure_experience,omitempty" json:"past_failure_experience,omitempty"`                   // 过往减肥失败经历
	FitnessEquipment              string `bson:"fitness_equipment,omitempty" json:"fitness_equipment,omitempty"`                                 // 用户已有的健身器械

	SubscriptionProductID             string     `bson:"subscription_product_id,omitempty" json:"subscription_product_id,omitempty"`
	SubscriptionExpiry                *time.Time `bson:"subscription_expiry,omitempty" json:"subscription_expiry,omitempty"`
	SubscriptionOriginalTransactionID string     `bson:"subscription_original_transaction_id,omitempty" json:"subscription_original_transaction_id,omitempty"`

	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

func (t *Ticket) IsSubscriptionActive() bool {
	if t.SubscriptionExpiry == nil {
		return false
	}
	return t.SubscriptionExpiry.After(time.Now())
}

// GetReminderHour 返回用户设置的提醒小时，未设置则返回默认值
func (t *Ticket) GetReminderHour() int {
	if t.ReminderHour == 0 && t.ReminderMinute == 0 {
		return DefaultReminderHour
	}
	return t.ReminderHour
}

func (t *Ticket) GetReminderMinute() int {
	if t.ReminderHour == 0 && t.ReminderMinute == 0 {
		return DefaultReminderMinute
	}
	return t.ReminderMinute
}

func ticketColl() (*mongo.Collection, error) {
	if client == nil {
		return nil, ErrClientNotInitialized
	}
	db := getDatabase()
	return db.Collection(ticketCollection), nil
}

// FindTicketByDeviceID finds an existing ticket by device ID
func FindTicketByDeviceID(ctx context.Context, deviceID string) (*Ticket, error) {
	if deviceID == "" {
		return nil, nil
	}

	tickets, err := ticketColl()
	if err != nil {
		return nil, err
	}

	var ticket Ticket
	if err := tickets.FindOne(ctx, bson.M{"device_id": deviceID}).Decode(&ticket); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &ticket, nil
}

// GenerateTicket creates a new ticket for a device
func GenerateTicket(ctx context.Context, deviceID string) (string, error) {
	tickets, err := ticketColl()
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	id := bson.NewObjectID()
	ticket := &Ticket{
		ID:        id,
		DeviceID:  deviceID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = tickets.InsertOne(ctx, ticket)
	if err != nil {
		return "", err
	}
	return id.Hex(), nil
}

// GetTicket retrieves ticket details
func GetTicket(ctx context.Context, idHex string) (*Ticket, error) {
	if idHex == "" {
		return nil, errors.New("ticket id is required")
	}
	oid, err := bson.ObjectIDFromHex(idHex)
	if err != nil {
		return nil, errors.New("invalid ticket id")
	}

	tickets, err := ticketColl()
	if err != nil {
		return nil, err
	}

	var ticket Ticket
	if err := tickets.FindOne(ctx, bson.M{"_id": oid}).Decode(&ticket); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrTicketNotFound
		}
		return nil, err
	}
	return &ticket, nil
}

// UpdateSubscription updates the subscription info for a ticket
func UpdateSubscription(ctx context.Context, ticketID bson.ObjectID, productID string, expiry time.Time, originalTransactionID string) error {
	tickets, err := ticketColl()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	filter := bson.M{"_id": ticketID}
	update := bson.M{
		"$set": bson.M{
			"subscription_product_id":              productID,
			"subscription_expiry":                  expiry,
			"subscription_original_transaction_id": originalTransactionID,
			"updated_at":                           now,
		},
	}

	result, err := tickets.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrTicketNotFound
	}
	return nil
}

// ExpireSubscription clears the subscription for a ticket (revoke/refund)
func ExpireSubscription(ctx context.Context, ticketID bson.ObjectID) error {
	tickets, err := ticketColl()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	filter := bson.M{"_id": ticketID}
	update := bson.M{
		"$set": bson.M{
			"subscription_expiry": now,
			"updated_at":          now,
		},
	}

	result, err := tickets.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrTicketNotFound
	}
	return nil
}

// FindTicketByOriginalTransactionID finds a ticket by its Apple subscription original transaction ID
func FindTicketByOriginalTransactionID(ctx context.Context, originalTransactionID string) (*Ticket, error) {
	tickets, err := ticketColl()
	if err != nil {
		return nil, err
	}

	var ticket Ticket
	err = tickets.FindOne(ctx, bson.M{
		"subscription_original_transaction_id": originalTransactionID,
	}).Decode(&ticket)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &ticket, nil
}

// AppleIAPTransactionStatus represents the status of an Apple IAP transaction
type AppleIAPTransactionStatus string

const (
	AppleIAPStatusPending   AppleIAPTransactionStatus = "pending"   // Transaction recorded, recharge not yet attempted
	AppleIAPStatusSuccess   AppleIAPTransactionStatus = "success"   // Recharge completed successfully
	AppleIAPStatusFailed    AppleIAPTransactionStatus = "failed"    // Recharge failed
	AppleIAPStatusDuplicate AppleIAPTransactionStatus = "duplicate" // Duplicate transaction (already processed)
	AppleIAPStatusRefunded  AppleIAPTransactionStatus = "refunded"  // Transaction was refunded by Apple
)

// AppleIAPTransaction records Apple in-app purchase transactions with complete info
type AppleIAPTransaction struct {
	TransactionID         string                    `bson:"_id" json:"transaction_id"`
	OriginalTransactionID string                    `bson:"original_transaction_id" json:"original_transaction_id"`
	TicketID              bson.ObjectID             `bson:"ticket_id" json:"ticket_id"`
	ProductID             string                    `bson:"product_id" json:"product_id"`
	Environment           string                    `bson:"environment" json:"environment"`
	BundleID              string                    `bson:"bundle_id" json:"bundle_id"`
	AppAccountToken       string                    `bson:"app_account_token,omitempty" json:"app_account_token,omitempty"`
	PurchaseDate          time.Time                 `bson:"purchase_date" json:"purchase_date"`
	ExpiresDate           *time.Time                `bson:"expires_date,omitempty" json:"expires_date,omitempty"`
	SignedDate            time.Time                 `bson:"signed_date" json:"signed_date"`
	TransactionType       string                    `bson:"transaction_type" json:"transaction_type"`
	Quantity              int                       `bson:"quantity" json:"quantity"`
	Storefront            string                    `bson:"storefront,omitempty" json:"storefront,omitempty"`
	StorefrontID          string                    `bson:"storefront_id,omitempty" json:"storefront_id,omitempty"`
	Price                 int64                     `bson:"price,omitempty" json:"price,omitempty"`
	Currency              string                    `bson:"currency,omitempty" json:"currency,omitempty"`
	Status                AppleIAPTransactionStatus `bson:"status" json:"status"`
	ErrorMessage          string                    `bson:"error_message,omitempty" json:"error_message,omitempty"`
	CreatedAt             time.Time                 `bson:"created_at" json:"created_at"`
	UpdatedAt             time.Time                 `bson:"updated_at" json:"updated_at"`
}

// AppleIAPTransactionInput contains all fields needed to create a transaction record
type AppleIAPTransactionInput struct {
	TransactionID         string
	OriginalTransactionID string
	TicketID              bson.ObjectID
	ProductID             string
	Environment           string
	BundleID              string
	AppAccountToken       string
	PurchaseDate          int64 // Unix milliseconds
	ExpiresDate           int64 // Unix milliseconds (0 if no expiry)
	SignedDate            int64 // Unix milliseconds
	TransactionType       string
	Quantity              int
	Storefront            string
	StorefrontID          string
	Price                 int64
	Currency              string
}

// CheckAppleTransactionExists checks if an Apple transaction has already been processed
func CheckAppleTransactionExists(ctx context.Context, transactionID string) (bool, *AppleIAPTransaction, error) {
	if client == nil {
		return false, nil, ErrClientNotInitialized
	}

	db := getDatabase()
	coll := db.Collection(appleIAPTransactionCollection)

	var tx AppleIAPTransaction
	err := coll.FindOne(ctx, bson.M{"_id": transactionID}).Decode(&tx)
	if err == nil {
		return true, &tx, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil, nil
	}
	return false, nil, err
}

// CreateAppleTransaction creates a new Apple transaction record with pending status
// This should be called BEFORE attempting the recharge to ensure we have a record
func CreateAppleTransaction(ctx context.Context, input *AppleIAPTransactionInput) (*AppleIAPTransaction, error) {
	if client == nil {
		return nil, ErrClientNotInitialized
	}

	db := getDatabase()
	coll := db.Collection(appleIAPTransactionCollection)

	now := time.Now().UTC()
	tx := &AppleIAPTransaction{
		TransactionID:         input.TransactionID,
		OriginalTransactionID: input.OriginalTransactionID,
		TicketID:              input.TicketID,
		ProductID:             input.ProductID,
		Environment:           input.Environment,
		BundleID:              input.BundleID,
		AppAccountToken:       input.AppAccountToken,
		PurchaseDate:          time.UnixMilli(input.PurchaseDate),
		SignedDate:            time.UnixMilli(input.SignedDate),
		TransactionType:       input.TransactionType,
		Quantity:              input.Quantity,
		Storefront:            input.Storefront,
		StorefrontID:          input.StorefrontID,
		Price:                 input.Price,
		Currency:              input.Currency,
		Status:                AppleIAPStatusPending,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if input.ExpiresDate > 0 {
		exp := time.UnixMilli(input.ExpiresDate)
		tx.ExpiresDate = &exp
	}

	_, err := coll.InsertOne(ctx, tx)
	if err != nil {
		// Check if it's a duplicate key error
		if mongo.IsDuplicateKeyError(err) {
			return nil, fmt.Errorf("transaction already exists: %s", input.TransactionID)
		}
		return nil, err
	}
	return tx, nil
}

// UpdateAppleTransactionStatus updates the status of an Apple transaction
func UpdateAppleTransactionStatus(ctx context.Context, transactionID string, status AppleIAPTransactionStatus, errorMsg string) error {
	if client == nil {
		return ErrClientNotInitialized
	}

	db := getDatabase()
	coll := db.Collection(appleIAPTransactionCollection)

	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"updated_at": time.Now().UTC(),
		},
	}

	if errorMsg != "" {
		update["$set"].(bson.M)["error_message"] = errorMsg
	}

	_, err := coll.UpdateByID(ctx, transactionID, update)
	return err
}

// FindAppleTransactionsByOriginalID finds all transactions with the same original transaction ID
// This can be used to find all purchases by the same user for ticket recovery
func FindAppleTransactionsByOriginalID(ctx context.Context, originalTransactionID string) ([]*AppleIAPTransaction, error) {
	if client == nil {
		return nil, ErrClientNotInitialized
	}

	db := getDatabase()
	coll := db.Collection(appleIAPTransactionCollection)

	cursor, err := coll.Find(ctx, bson.M{"original_transaction_id": originalTransactionID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var transactions []*AppleIAPTransaction
	if err := cursor.All(ctx, &transactions); err != nil {
		return nil, err
	}
	return transactions, nil
}

// FindPendingAppleTransactions finds all pending transactions for retry processing
func FindPendingAppleTransactions(ctx context.Context) ([]*AppleIAPTransaction, error) {
	if client == nil {
		return nil, ErrClientNotInitialized
	}

	db := getDatabase()
	coll := db.Collection(appleIAPTransactionCollection)

	cursor, err := coll.Find(ctx, bson.M{"status": AppleIAPStatusPending})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var transactions []*AppleIAPTransaction
	if err := cursor.All(ctx, &transactions); err != nil {
		return nil, err
	}
	return transactions, nil
}

// MarkTransactionRefunded marks an Apple IAP transaction as refunded
func MarkTransactionRefunded(ctx context.Context, transactionID string) error {
	if client == nil {
		return ErrClientNotInitialized
	}

	db := getDatabase()
	coll := db.Collection(appleIAPTransactionCollection)

	update := bson.M{
		"$set": bson.M{
			"status":     AppleIAPStatusRefunded,
			"updated_at": time.Now().UTC(),
		},
	}

	_, err := coll.UpdateByID(ctx, transactionID, update)
	return err
}

// --- Ticket 扩展: 设备推送 & 体重 ---

func UpdateTicketDeviceToken(ctx context.Context, idHex, deviceToken string) error {
	oid, err := bson.ObjectIDFromHex(idHex)
	if err != nil {
		return ErrInvalidTicketID
	}

	coll, err := ticketColl()
	if err != nil {
		return err
	}

	result, err := coll.UpdateByID(ctx, oid, bson.M{
		"$set": bson.M{"device_token": deviceToken, "updated_at": time.Now().UTC()},
	})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrTicketNotFound
	}
	return nil
}

func UpdateTicketWeight(ctx context.Context, idHex string, weight float64) (*Ticket, error) {
	// UpdateTicketWeight now uses AddWeightRecord which handles both weight_history and current_weight
	// 此处不传 allowMultiplePerDay，保持“一天一次”的默认限制
	_, err := AddWeightRecord(ctx, idHex, weight, false)
	if err != nil {
		return nil, err
	}
	return GetTicket(ctx, idHex)
}

// GetAllTicketsWithDeviceToken 获取所有注册了推送 token 的 ticket
func GetAllTicketsWithDeviceToken(ctx context.Context) ([]*Ticket, error) {
	coll, err := ticketColl()
	if err != nil {
		return nil, err
	}

	cursor, err := coll.Find(ctx, bson.M{
		"device_token": bson.M{"$exists": true, "$ne": ""},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var tickets []*Ticket
	if err := cursor.All(ctx, &tickets); err != nil {
		return nil, err
	}
	return tickets, nil
}

func UpdateTicketReminderTime(ctx context.Context, idHex string, hour, minute int) error {
	oid, err := bson.ObjectIDFromHex(idHex)
	if err != nil {
		return ErrInvalidTicketID
	}

	coll, err := ticketColl()
	if err != nil {
		return err
	}

	result, err := coll.UpdateByID(ctx, oid, bson.M{
		"$set": bson.M{
			"reminder_hour":   hour,
			"reminder_minute": minute,
			"updated_at":      time.Now().UTC(),
		},
	})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrTicketNotFound
	}
	return nil
}

// UpdateTicketProfile updates user profile information
func UpdateTicketProfile(ctx context.Context, idHex string, basicInfo, dietaryAndExercisePreferences, healthIssues *string, targetWeight *float64, workType, executionConstraints, pastFailureExperience, fitnessEquipment *string) error {
	oid, err := bson.ObjectIDFromHex(idHex)
	if err != nil {
		return ErrInvalidTicketID
	}

	coll, err := ticketColl()
	if err != nil {
		return err
	}

	updateFields := bson.M{
		"updated_at": time.Now().UTC(),
	}

	if basicInfo != nil {
		updateFields["basic_info"] = *basicInfo
	}
	if dietaryAndExercisePreferences != nil {
		updateFields["dietary_and_exercise_preferences"] = *dietaryAndExercisePreferences
	}
	if healthIssues != nil {
		updateFields["health_issues"] = *healthIssues
	}
	if targetWeight != nil {
		updateFields["target_weight"] = *targetWeight
	}
	if workType != nil {
		updateFields["work_type"] = *workType
	}
	if executionConstraints != nil {
		updateFields["execution_constraints"] = *executionConstraints
	}
	if pastFailureExperience != nil {
		updateFields["past_failure_experience"] = *pastFailureExperience
	}
	if fitnessEquipment != nil {
		updateFields["fitness_equipment"] = *fitnessEquipment
	}

	result, err := coll.UpdateByID(ctx, oid, bson.M{
		"$set": updateFields,
	})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrTicketNotFound
	}
	return nil
}

// --- Weight History (stored in Ticket) ---

// AddWeightRecord adds a weight record to the ticket's weight_history array.
// 当 allowMultiplePerDay 为 false 时，一天最多只能更新一次体重；为 true 时（开发测试用）不限制。
func AddWeightRecord(ctx context.Context, ticketIDHex string, weight float64, allowMultiplePerDay bool) (*WeightRecord, error) {
	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return nil, ErrInvalidTicketID
	}

	coll, err := ticketColl()
	if err != nil {
		return nil, err
	}

	// 先获取ticket，检查今天是否已经更新过
	var ticket Ticket
	err = coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&ticket)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrTicketNotFound
		}
		return nil, err
	}

	today := getTodayDateString()
	now := time.Now().UTC()
	loc, _ := time.LoadLocation("Asia/Shanghai")

	// 检查今天是否已经更新过体重（通过 WeightUpdatedAt 判断）；开发测试可配置允许一天多次更新
	if !allowMultiplePerDay && ticket.WeightUpdatedAt != nil {
		updatedDate := ticket.WeightUpdatedAt.In(loc).Format("2006-01-02")
		if updatedDate == today {
			return nil, ErrWeightUpdateLimitReached
		}
	}

	record := WeightRecord{
		Weight:    weight,
		Date:      today,
		CreatedAt: now,
	}

	// 更新体重和更新时间
	updateFields := bson.M{
		"current_weight":    weight,
		"weight_updated_at": now,
		"updated_at":        now,
	}

	// 允许一天多次更新且今天已有记录时：替换 weight_history 中当天的记录，否则插入新记录
	isReplaceToday := allowMultiplePerDay && ticket.WeightUpdatedAt != nil &&
		ticket.WeightUpdatedAt.In(loc).Format("2006-01-02") == today

	var result *mongo.UpdateResult
	if isReplaceToday {
		result, err = coll.UpdateOne(ctx,
			bson.M{"_id": oid, "weight_history.date": today},
			bson.M{
				"$set": bson.M{
					"current_weight":         weight,
					"weight_updated_at":      now,
					"updated_at":             now,
					"weight_history.$[elem]": record,
				},
			},
			options.UpdateOne().SetArrayFilters([]any{bson.M{"elem.date": today}}),
		)
	} else {
		result, err = coll.UpdateOne(ctx,
			bson.M{"_id": oid},
			bson.M{
				"$push": bson.M{
					"weight_history": bson.M{
						"$each":     []WeightRecord{record},
						"$position": 0,
					},
				},
				"$set": updateFields,
			},
		)
	}
	if err != nil {
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, ErrTicketNotFound
	}

	return &record, nil
}

// GetWeightHistory retrieves weight history from ticket's weight_history array
func GetWeightHistory(ctx context.Context, ticketIDHex string, limit int) ([]*WeightRecord, error) {
	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return nil, ErrInvalidTicketID
	}

	coll, err := ticketColl()
	if err != nil {
		return nil, err
	}

	var ticket Ticket
	err = coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&ticket)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrTicketNotFound
		}
		return nil, err
	}

	// 限制返回数量
	history := ticket.WeightHistory
	if len(history) > limit {
		history = history[:limit]
	}

	// 转换为指针数组
	result := make([]*WeightRecord, len(history))
	for i := range history {
		result[i] = &history[i]
	}

	return result, nil
}

// HasWeightRecordToday checks if there's a weight record for today in the ticket
// 通过 WeightUpdatedAt 字段判断今天是否已更新体重
func HasWeightRecordToday(ctx context.Context, ticketIDHex string) (bool, error) {
	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return false, ErrInvalidTicketID
	}

	coll, err := ticketColl()
	if err != nil {
		return false, err
	}

	var ticket Ticket
	err = coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&ticket)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, ErrTicketNotFound
		}
		return false, err
	}

	// 通过 WeightUpdatedAt 判断今天是否已更新
	if ticket.WeightUpdatedAt != nil {
		loc, _ := time.LoadLocation("Asia/Shanghai")
		updatedDate := ticket.WeightUpdatedAt.In(loc).Format("2006-01-02")
		today := getTodayDateString()
		return updatedDate == today, nil
	}

	return false, nil
}
