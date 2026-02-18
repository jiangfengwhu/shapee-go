package db

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const weightCollection = "weight_records"

type WeightRecord struct {
	ID        bson.ObjectID `bson:"_id" json:"id"`
	TicketID  bson.ObjectID `bson:"ticket_id" json:"ticket_id"`
	Weight    float64       `bson:"weight" json:"weight"`
	Date      string        `bson:"date" json:"date"`
	CreatedAt time.Time     `bson:"created_at" json:"created_at"`
}

func weightColl() (*mongo.Collection, error) {
	if client == nil {
		return nil, ErrClientNotInitialized
	}
	return getDatabase().Collection(weightCollection), nil
}

func EnsureWeightIndexes(ctx context.Context) error {
	coll, err := weightColl()
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

func AddWeightRecord(ctx context.Context, ticketIDHex string, weight float64) (*WeightRecord, error) {
	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return nil, ErrInvalidTicketID
	}

	coll, err := weightColl()
	if err != nil {
		return nil, err
	}

	today := getTodayDateString()
	now := time.Now().UTC()

	record := &WeightRecord{
		ID:        bson.NewObjectID(),
		TicketID:  oid,
		Weight:    weight,
		Date:      today,
		CreatedAt: now,
	}

	// upsert: 同一天更新体重则覆盖
	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)
	err = coll.FindOneAndUpdate(ctx,
		bson.M{"ticket_id": oid, "date": today},
		bson.M{
			"$set": bson.M{"weight": weight, "created_at": now},
			"$setOnInsert": bson.M{"_id": record.ID},
		},
		opts,
	).Decode(record)
	if err != nil {
		return nil, err
	}

	return record, nil
}

func GetWeightHistory(ctx context.Context, ticketIDHex string, limit int) ([]*WeightRecord, error) {
	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return nil, ErrInvalidTicketID
	}

	coll, err := weightColl()
	if err != nil {
		return nil, err
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "date", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := coll.Find(ctx, bson.M{"ticket_id": oid}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var records []*WeightRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func HasWeightRecordToday(ctx context.Context, ticketIDHex string) (bool, error) {
	oid, err := bson.ObjectIDFromHex(ticketIDHex)
	if err != nil {
		return false, ErrInvalidTicketID
	}

	coll, err := weightColl()
	if err != nil {
		return false, err
	}

	today := getTodayDateString()
	count, err := coll.CountDocuments(ctx, bson.M{
		"ticket_id": oid,
		"date":      today,
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func getTodayDateString() string {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	return time.Now().In(loc).Format("2006-01-02")
}

func GetTodayDateString() string {
	return getTodayDateString()
}
