package main

import (
	"keepy-go/config"
	"keepy-go/db"
	"keepy-go/middleware"
	"keepy-go/routes"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	config, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	// validate config
	if err := config.Validate(); err != nil {
		log.Fatalf("failed to validate config: %v", err)
	}
	// connect to MongoDB
	db.Connect(config.Database.URL)
	defer db.Close()

	// Create a Gin router with default middleware (logger and recovery)
	r := gin.Default()
	// Public routes
	routes.TicketRoutes(r, config)

	// Protected routes
	protected := r.Group("/")
	protected.Use(middleware.TicketAuth)
	{
		routes.ChatRoutes(protected, config)
		routes.NoteRoutes(protected, config)
		routes.SummaryRoutes(protected, config)
		routes.CheckinRoutes(protected)
	}

	// Let's use a simpler approach: Apply middleware to specific routes individually or refactor routes functions
	// Current routes functions: func ChatRoutes(r *gin.Engine, cfg *config.Config)
	// They register r.POST(...) internally.
	// To support middleware, we should change them to accept *gin.RouterGroup OR just wrap the handler.

	// Refactoring routes to accept gin.IRouter interface is better, but requires changing all files.
	// Alternative: Define routes in main.go using handlers? No, logic is in routes pkg.

	// Let's modify routes/chat.go etc to accept gin.IRouter

	// Define a simple GET endpoint
	r.GET("/ping", func(c *gin.Context) {
		// Return JSON response
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	// Start server on port 8080 (default)
	// Server will listen on 0.0.0.0:8080 (localhost:8080 on Windows)
	if err := r.Run(":" + config.Port); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
