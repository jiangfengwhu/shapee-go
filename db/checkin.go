package db

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const checkinCollection = "checkins"

var (
	ErrAlreadyCheckedIn = errors.New("already checked in today")
)

// CheckinRecord represents a daily check-in record
type CheckinRecord struct {
	ID        bson.ObjectID `bson:"_id" json:"id"`
	TicketID  bson.ObjectID `bson:"ticket_id" json:"ticket_id"`
	Date      string        `bson:"date" json:"date"` // "2006-01-02" in Asia/Shanghai timezone
	CreatedAt time.Time     `bson:"created_at" json:"created_at"`
}

// getTodayDateString returns today's date string in Asia/Shanghai timezone
func getTodayDateString() string {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	return time.Now().In(loc).Format("2006-01-02")
}

// Checkin performs a daily check-in for the given ticket.
// Returns the check-in record.
// Uses a unique index on (ticket_id, date) to enforce once-per-day constraint.
func Checkin(ctx context.Context, ticketIDHex string) (*CheckinRecord, error) {
	if client == nil {
		return nil, ErrClientNotInitialized
	}

	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return nil, ErrInvalidTicketID
	}

	db := getKeepyDatabase()
	coll := db.Collection(checkinCollection)

	// Ensure unique index on (ticket_id, date)
	indexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "ticket_id", Value: 1},
			{Key: "date", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	}
	coll.Indexes().CreateOne(ctx, indexModel)

	today := getTodayDateString()
	now := time.Now().UTC()

	record := &CheckinRecord{
		ID:        bson.NewObjectID(),
		TicketID:  oid,
		Date:      today,
		CreatedAt: now,
	}

	_, err = coll.InsertOne(ctx, record)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, ErrAlreadyCheckedIn
		}
		return nil, err
	}

	// Add 10 quota
	if err := RechargeTicket(ctx, ticketIDHex, 10); err != nil {
		return nil, err
	}

	return record, nil
}
