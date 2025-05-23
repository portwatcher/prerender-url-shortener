package api

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// SetupRouter initializes and configures the Gin router.
func SetupRouter() *gin.Engine {
	r := gin.Default() // Logger and Recovery middleware included

	// CORS middleware configuration
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	// You can customize other CORS options here if needed, for example:
	// config.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	// config.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization"}
	r.Use(cors.New(config))

	// Health check endpoint
	r.GET("/health", HealthCheckHandler)

	// Status endpoint with detailed information
	r.GET("/status", StatusHandler)

	// API v1 group (optional, but good practice)
	// apiV1 := r.Group("/api/v1")
	// {
	// 	apiV1.POST("/generate", GenerateShortCodeHandler)
	// }

	// Directly define routes for simplicity for now
	r.POST("/generate", GenerateShortCodeHandler)
	r.GET("/:shortCode", RedirectHandler)

	return r
}
