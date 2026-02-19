package db

import (
	"log"
	"time"

	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var client *mongo.Client

func Connect(uri string) {
	var err error
	client, err = mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatalf("failed to connect to MongoDB: %v", err)
	}
	log.Println("connected to MongoDB")
}

func Close() {
	if err := client.Disconnect(context.Background()); err != nil {
		log.Fatalf("failed to disconnect from MongoDB: %v", err)
	}
	log.Println("disconnected from MongoDB")
}

func getDatabase() *mongo.Database {
	return client.Database("shapee")
}

func getTodayDateString() string {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	return time.Now().In(loc).Format("2006-01-02")
}

func GetTodayDateString() string {
	return getTodayDateString()
}
