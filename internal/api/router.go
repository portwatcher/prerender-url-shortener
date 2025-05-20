package api

import (
	"github.com/gin-gonic/gin"
)

// SetupRouter initializes and configures the Gin router.
func SetupRouter() *gin.Engine {
	r := gin.Default() // Logger and Recovery middleware included

	// Health check endpoint
	r.GET("/health", HealthCheckHandler)

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
