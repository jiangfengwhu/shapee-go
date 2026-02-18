package db

import (
	"log"

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

func getKeepyDatabase() *mongo.Database {
	return client.Database("keepy")
}
