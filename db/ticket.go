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
	ErrTicketNotFound       = errors.New("ticket not found")
	ErrInsufficientTicket   = errors.New("insufficient ticket balance")
	ErrInvalidAmount        = errors.New("invalid amount")
	ErrInvalidTicketID      = errors.New("invalid ticket id")
	ErrClientNotInitialized = errors.New("client not initialized")
)

type Ticket struct {
	ID        bson.ObjectID `bson:"_id"`
	DeviceID  string        `bson:"device_id,omitempty" json:"device_id,omitempty"`
	Balance   int64         `bson:"balance"`
	CreatedAt time.Time     `bson:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"`
}

func ticketColl() (*mongo.Collection, error) {
	if client == nil {
		return nil, ErrClientNotInitialized
	}
	db := getKeepyDatabase()
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

// GenerateTicket creates a new ticket with initial bonus balance
func GenerateTicket(ctx context.Context, deviceID string) (string, error) {
	tickets, err := ticketColl()
	if err != nil {
		return "", err
	}

	const initialBonus int64 = 66 // 新账号赠送66次调用额度

	now := time.Now().UTC()
	id := bson.NewObjectID()
	ticket := &Ticket{
		ID:        id,
		DeviceID:  deviceID,
		Balance:   initialBonus,
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

// RechargeTicket adds quota to a ticket
func RechargeTicket(ctx context.Context, idHex string, amount int64) error {
	if idHex == "" {
		return errors.New("ticket id is required")
	}
	if amount <= 0 {
		return ErrInvalidAmount
	}
	oid, err := bson.ObjectIDFromHex(idHex)
	if err != nil {
		return errors.New("invalid ticket id")
	}

	tickets, err := ticketColl()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	filter := bson.M{"_id": oid}
	update := bson.M{
		"$inc": bson.M{"balance": amount},
		"$set": bson.M{"updated_at": now},
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

// ConsumeTicket consumes 1 quota from a ticket
func ConsumeTicket(ctx context.Context, idHex string) error {
	if idHex == "" {
		return errors.New("ticket id is required")
	}
	oid, err := bson.ObjectIDFromHex(idHex)
	if err != nil {
		return errors.New("invalid ticket id")
	}

	tickets, err := ticketColl()
	if err != nil {
		return err
	}

	amount := int64(1)
	now := time.Now().UTC()

	// Atomic check and update
	filter := bson.M{
		"_id":     oid,
		"balance": bson.M{"$gte": amount},
	}
	update := bson.M{
		"$inc": bson.M{"balance": -amount},
		"$set": bson.M{"updated_at": now},
	}

	var ticket Ticket
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	if err := tickets.FindOneAndUpdate(ctx, filter, update, opts).Decode(&ticket); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Check if ticket exists but has insufficient balance
			var existingTicket Ticket
			if findErr := tickets.FindOne(ctx, bson.M{"_id": oid}).Decode(&existingTicket); findErr == nil {
				return ErrInsufficientTicket
			}
			return ErrTicketNotFound
		}
		return err
	}
	return nil
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
	Amount                int64                     `bson:"amount" json:"amount"`
	Environment           string                    `bson:"environment" json:"environment"` // "Sandbox" or "Production"
	BundleID              string                    `bson:"bundle_id" json:"bundle_id"`
	AppAccountToken       string                    `bson:"app_account_token,omitempty" json:"app_account_token,omitempty"`
	PurchaseDate          time.Time                 `bson:"purchase_date" json:"purchase_date"`
	SignedDate            time.Time                 `bson:"signed_date" json:"signed_date"`
	TransactionType       string                    `bson:"transaction_type" json:"transaction_type"` // Consumable, Non-Consumable, etc.
	Quantity              int                       `bson:"quantity" json:"quantity"`
	Storefront            string                    `bson:"storefront,omitempty" json:"storefront,omitempty"`
	StorefrontID          string                    `bson:"storefront_id,omitempty" json:"storefront_id,omitempty"`
	Price                 int64                     `bson:"price,omitempty" json:"price,omitempty"`
	Currency              string                    `bson:"currency,omitempty" json:"currency,omitempty"`
	Status                AppleIAPTransactionStatus `bson:"status" json:"status"`
	ErrorMessage          string                    `bson:"error_message,omitempty" json:"error_message,omitempty"`
	RechargeTransactionID bson.ObjectID             `bson:"recharge_transaction_id,omitempty" json:"recharge_transaction_id,omitempty"`
	CreatedAt             time.Time                 `bson:"created_at" json:"created_at"`
	UpdatedAt             time.Time                 `bson:"updated_at" json:"updated_at"`
}

// AppleIAPTransactionInput contains all fields needed to create a transaction record
type AppleIAPTransactionInput struct {
	TransactionID         string
	OriginalTransactionID string
	TicketID              bson.ObjectID
	ProductID             string
	Amount                int64
	Environment           string
	BundleID              string
	AppAccountToken       string
	PurchaseDate          int64 // Unix milliseconds
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

	db := getKeepyDatabase()
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

	db := getKeepyDatabase()
	coll := db.Collection(appleIAPTransactionCollection)

	now := time.Now().UTC()
	tx := &AppleIAPTransaction{
		TransactionID:         input.TransactionID,
		OriginalTransactionID: input.OriginalTransactionID,
		TicketID:              input.TicketID,
		ProductID:             input.ProductID,
		Amount:                input.Amount,
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
func UpdateAppleTransactionStatus(ctx context.Context, transactionID string, status AppleIAPTransactionStatus, errorMsg string, rechargeTransactionID bson.ObjectID) error {
	if client == nil {
		return ErrClientNotInitialized
	}

	db := getKeepyDatabase()
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

	if !rechargeTransactionID.IsZero() {
		update["$set"].(bson.M)["recharge_transaction_id"] = rechargeTransactionID
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

	db := getKeepyDatabase()
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

	db := getKeepyDatabase()
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

// DeductTicketBalance deducts balance from a ticket (for refunds)
// This function allows the balance to go negative
func DeductTicketBalance(ctx context.Context, ticketID bson.ObjectID, amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	tickets, err := ticketColl()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	filter := bson.M{"_id": ticketID}
	update := bson.M{
		"$inc": bson.M{"balance": -amount},
		"$set": bson.M{"updated_at": now},
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

// MarkTransactionRefunded marks an Apple IAP transaction as refunded
func MarkTransactionRefunded(ctx context.Context, transactionID string) error {
	if client == nil {
		return ErrClientNotInitialized
	}

	db := getKeepyDatabase()
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
