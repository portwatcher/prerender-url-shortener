package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config holds the application configuration.
// We'll use struct tags for environment variable loading.
type Config struct {
	ServerPort     string `env:"SERVER_PORT,default=:8080"`
	DatabaseURL    string `env:"DATABASE_URL,required"`
	RodBinPath     string `env:"ROD_BIN_PATH"`    // Optional, if not in default PATH
	AllowedDomains string `env:"ALLOWED_DOMAINS"` // Comma-separated list of allowed domains
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
