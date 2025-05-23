package main

import (
	"log"
	"os"
	"os/signal"
	"prerender-url-shortener/internal/api"
	"prerender-url-shortener/internal/config"
	"prerender-url-shortener/internal/db"
	"prerender-url-shortener/internal/renderer"
	"syscall"

	_ "github.com/jinzhu/gorm/dialects/postgres" // PostgreSQL driver for GORM
)

func main() {
	// Load application configuration
	err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	log.Println("Configuration loaded successfully.")

	// Initialize database connection
	log.Printf("Connecting to database: %s...", redactDBURL(config.AppConfig.DatabaseURL))
	err = db.InitDB(config.AppConfig.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.DB.Close()
	log.Println("Database connection successful and schema migrated.")

	// Initialize render queue with configurable worker count
	workerCount := config.AppConfig.RenderWorkerCount
	renderer.InitRenderQueue(workerCount)

	// Setup graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Shutting down gracefully...")
		renderer.GlobalRenderQueue.Shutdown()
		os.Exit(0)
	}()

	// Setup router
	router := api.SetupRouter()
	serverAddr := config.AppConfig.ServerPort

	log.Printf("Starting server on %s...", serverAddr)
	if err := router.Run(serverAddr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// redactDBURL is a helper function to avoid logging sensitive parts of the DB URL.
// It's a basic redaction, more robust parsing might be needed for complex URLs.
func redactDBURL(dbURL string) string {
	// Example: postgres://user:password@host:port/dbname?sslmode=disable
	// Becomes: postgres://user:********@host:port/dbname?sslmode=disable
	parts := []byte(dbURL)
	passStart := -1
	passEnd := -1
	atFound := false

	// Find user: part
	for i, char := range parts {
		if char == ':' && passStart == -1 { // First colon after user part
			passStart = i + 1
		} else if char == '@' && passStart != -1 { // @ after password part
			passEnd = i
			atFound = true
			break
		}
	}

	if atFound && passStart != -1 && passEnd > passStart {
		return string(parts[:passStart]) + "********" + string(parts[passEnd:])
	}
	return dbURL // Return original if parsing fails or no password found
}
