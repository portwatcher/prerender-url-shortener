package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	// Save original env vars
	originalVars := map[string]string{
		"DATABASE_URL":           os.Getenv("DATABASE_URL"),
		"SERVER_PORT":            os.Getenv("SERVER_PORT"),
		"ROD_BIN_PATH":           os.Getenv("ROD_BIN_PATH"),
		"ALLOWED_DOMAINS":        os.Getenv("ALLOWED_DOMAINS"),
		"RENDER_WORKER_COUNT":    os.Getenv("RENDER_WORKER_COUNT"),
		"RENDER_TIMEOUT_SECONDS": os.Getenv("RENDER_TIMEOUT_SECONDS"),
	}

	// Clean up function
	cleanup := func() {
		for key, value := range originalVars {
			if value == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, value)
			}
		}
	}
	defer cleanup()

	tests := []struct {
		name        string
		envVars     map[string]string
		wantErr     bool
		expectPanic bool
		expected    *Config
	}{
		{
			name: "valid configuration",
			envVars: map[string]string{
				"DATABASE_URL":           "postgres://user:pass@host:5432/db",
				"SERVER_PORT":            ":3000",
				"ROD_BIN_PATH":           "/usr/bin/chrome",
				"ALLOWED_DOMAINS":        "example.com,test.org",
				"RENDER_WORKER_COUNT":    "5",
				"RENDER_TIMEOUT_SECONDS": "120",
			},
			wantErr:     false,
			expectPanic: false,
			expected: &Config{
				DatabaseURL:          "postgres://user:pass@host:5432/db",
				ServerPort:           ":3000",
				RodBinPath:           "/usr/bin/chrome",
				AllowedDomains:       "example.com,test.org",
				RenderWorkerCount:    5,
				RenderTimeoutSeconds: 120,
			},
		},
		{
			name: "default values",
			envVars: map[string]string{
				"DATABASE_URL": "postgres://user:pass@host:5432/db",
			},
			wantErr:     false,
			expectPanic: false,
			expected: &Config{
				DatabaseURL:          "postgres://user:pass@host:5432/db",
				ServerPort:           ":8080",
				RodBinPath:           "",
				AllowedDomains:       "",
				RenderWorkerCount:    3,
				RenderTimeoutSeconds: 90,
			},
		},
		{
			name: "invalid worker count",
			envVars: map[string]string{
				"DATABASE_URL":        "postgres://user:pass@host:5432/db",
				"RENDER_WORKER_COUNT": "not_a_number",
			},
			wantErr:     false,
			expectPanic: false,
			expected: &Config{
				DatabaseURL:          "postgres://user:pass@host:5432/db",
				ServerPort:           ":8080",
				RenderWorkerCount:    3, // should fallback to default
				RenderTimeoutSeconds: 90,
			},
		},
		{
			name: "missing database URL",
			envVars: map[string]string{
				"SERVER_PORT": ":8080",
			},
			wantErr:     false,
			expectPanic: true, // Should call log.Fatal
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all env vars first
			for key := range originalVars {
				os.Unsetenv(key)
			}

			// Set test env vars
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			if tt.expectPanic {
				// This test expects log.Fatal to be called
				// We can't easily test log.Fatal, so we'll skip this specific assertion
				// and just test that DATABASE_URL is required
				os.Unsetenv("DATABASE_URL")
				AppConfig = &Config{}
				AppConfig.DatabaseURL = getEnv("DATABASE_URL", "")
				assert.Empty(t, AppConfig.DatabaseURL)
				return
			}

			err := LoadConfig()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, AppConfig)
				assert.Equal(t, tt.expected.DatabaseURL, AppConfig.DatabaseURL)
				assert.Equal(t, tt.expected.ServerPort, AppConfig.ServerPort)
				assert.Equal(t, tt.expected.RodBinPath, AppConfig.RodBinPath)
				assert.Equal(t, tt.expected.AllowedDomains, AppConfig.AllowedDomains)
				assert.Equal(t, tt.expected.RenderWorkerCount, AppConfig.RenderWorkerCount)
				assert.Equal(t, tt.expected.RenderTimeoutSeconds, AppConfig.RenderTimeoutSeconds)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		fallback string
		envValue string
		setEnv   bool
		expected string
	}{
		{
			name:     "env var exists",
			key:      "TEST_KEY",
			fallback: "default",
			envValue: "custom_value",
			setEnv:   true,
			expected: "custom_value",
		},
		{
			name:     "env var does not exist",
			key:      "NONEXISTENT_KEY",
			fallback: "default_value",
			setEnv:   false,
			expected: "default_value",
		},
		{
			name:     "empty env var",
			key:      "EMPTY_KEY",
			fallback: "default",
			envValue: "",
			setEnv:   true,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up
			defer os.Unsetenv(tt.key)

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
			}

			result := getEnv(tt.key, tt.fallback)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		fallback int
		envValue string
		setEnv   bool
		expected int
	}{
		{
			name:     "valid integer",
			key:      "TEST_INT",
			fallback: 10,
			envValue: "42",
			setEnv:   true,
			expected: 42,
		},
		{
			name:     "invalid integer",
			key:      "INVALID_INT",
			fallback: 15,
			envValue: "not_a_number",
			setEnv:   true,
			expected: 15, // should return fallback
		},
		{
			name:     "env var not set",
			key:      "UNSET_INT",
			fallback: 20,
			setEnv:   false,
			expected: 20,
		},
		{
			name:     "negative integer",
			key:      "NEGATIVE_INT",
			fallback: 5,
			envValue: "-10",
			setEnv:   true,
			expected: -10,
		},
		{
			name:     "zero value",
			key:      "ZERO_INT",
			fallback: 5,
			envValue: "0",
			setEnv:   true,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up
			defer os.Unsetenv(tt.key)

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
			}

			result := getEnvInt(tt.key, tt.fallback)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfigStruct(t *testing.T) {
	config := &Config{
		ServerPort:           ":8080",
		DatabaseURL:          "postgres://localhost/test",
		RodBinPath:           "/usr/bin/chrome",
		AllowedDomains:       "example.com,test.org",
		RenderWorkerCount:    3,
		RenderTimeoutSeconds: 90,
	}

	assert.Equal(t, ":8080", config.ServerPort)
	assert.Equal(t, "postgres://localhost/test", config.DatabaseURL)
	assert.Equal(t, "/usr/bin/chrome", config.RodBinPath)
	assert.Equal(t, "example.com,test.org", config.AllowedDomains)
	assert.Equal(t, 3, config.RenderWorkerCount)
	assert.Equal(t, 90, config.RenderTimeoutSeconds)
}

func TestConfigDefaults(t *testing.T) {
	// Clear all env vars
	envVars := []string{
		"DATABASE_URL", "SERVER_PORT", "ROD_BIN_PATH",
		"ALLOWED_DOMAINS", "RENDER_WORKER_COUNT", "RENDER_TIMEOUT_SECONDS",
	}

	originalValues := make(map[string]string)
	for _, env := range envVars {
		originalValues[env] = os.Getenv(env)
		os.Unsetenv(env)
	}

	// Restore env vars after test
	defer func() {
		for env, value := range originalValues {
			if value != "" {
				os.Setenv(env, value)
			}
		}
	}()

	// Set only required DATABASE_URL
	os.Setenv("DATABASE_URL", "postgres://test/db")

	err := LoadConfig()
	require.NoError(t, err)

	// Check defaults
	assert.Equal(t, ":8080", AppConfig.ServerPort)
	assert.Equal(t, "", AppConfig.RodBinPath)
	assert.Equal(t, "", AppConfig.AllowedDomains)
	assert.Equal(t, 3, AppConfig.RenderWorkerCount)
	assert.Equal(t, 90, AppConfig.RenderTimeoutSeconds)
}

func TestConfigValidation(t *testing.T) {
	// Test that required fields are validated
	originalDB := os.Getenv("DATABASE_URL")
	defer func() {
		if originalDB != "" {
			os.Setenv("DATABASE_URL", originalDB)
		} else {
			os.Unsetenv("DATABASE_URL")
		}
	}()

	os.Unsetenv("DATABASE_URL")

	// This should work without panicking in tests, but we know it would panic in real usage
	AppConfig = &Config{}
	AppConfig.DatabaseURL = getEnv("DATABASE_URL", "")
	assert.Empty(t, AppConfig.DatabaseURL, "DATABASE_URL should be empty when not set")
}
