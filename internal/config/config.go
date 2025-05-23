package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds the application configuration.
// We'll use struct tags for environment variable loading.
type Config struct {
	ServerPort           string `env:"SERVER_PORT,default=:8080"`
	DatabaseURL          string `env:"DATABASE_URL,required"`
	RodBinPath           string `env:"ROD_BIN_PATH"`                      // Optional, if not in default PATH
	AllowedDomains       string `env:"ALLOWED_DOMAINS"`                   // Comma-separated list of allowed domains
	RenderWorkerCount    int    `env:"RENDER_WORKER_COUNT,default=3"`     // Number of render workers
	RenderTimeoutSeconds int    `env:"RENDER_TIMEOUT_SECONDS,default=90"` // Timeout for Rod rendering in seconds
}

var AppConfig *Config

// LoadConfig loads configuration from environment variables.
// It looks for a .env file in the current directory for development convenience.
func LoadConfig() error {
	// Attempt to load .env file, but don't fail if it's not there (for production)
	_ = godotenv.Load()

	AppConfig = &Config{}

	// Basic manual loading for now, can be replaced with a library like 'envconfig' later
	AppConfig.ServerPort = getEnv("SERVER_PORT", ":8080")
	AppConfig.DatabaseURL = getEnv("DATABASE_URL", "") // Required, so empty default
	AppConfig.RodBinPath = getEnv("ROD_BIN_PATH", "")
	AppConfig.AllowedDomains = getEnv("ALLOWED_DOMAINS", "") // Empty means allow all
	AppConfig.RenderWorkerCount = getEnvInt("RENDER_WORKER_COUNT", 3)
	AppConfig.RenderTimeoutSeconds = getEnvInt("RENDER_TIMEOUT_SECONDS", 90)

	if AppConfig.DatabaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	return nil
}

func getEnv(key string, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
		log.Printf("Warning: Invalid integer value for %s: %s, using default %d", key, value, fallback)
	}
	return fallback
}
