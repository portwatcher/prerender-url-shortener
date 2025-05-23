package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"prerender-url-shortener/internal/config"
	"prerender-url-shortener/internal/db"
	"prerender-url-shortener/internal/renderer"

	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestAPI(t *testing.T) *gin.Engine {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Setup test database
	var err error
	db.DB, err = gorm.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	err = db.DB.AutoMigrate(&db.Link{}).Error
	require.NoError(t, err)

	// Setup test config
	config.AppConfig = &config.Config{
		ServerPort:           ":8080",
		DatabaseURL:          "sqlite3://:memory:",
		AllowedDomains:       "",
		RenderWorkerCount:    1,
		RenderTimeoutSeconds: 30,
	}

	// Initialize render queue for testing
	renderer.InitRenderQueue(1)

	// Setup router
	router := gin.New()
	router.POST("/generate", GenerateShortCodeHandler)
	router.GET("/:shortCode", RedirectHandler)
	router.GET("/health", HealthCheckHandler)
	router.GET("/status", StatusHandler)

	return router
}

func teardownTestAPI(t *testing.T) {
	if db.DB != nil {
		db.DB.Close()
	}
	if renderer.GlobalRenderQueue != nil {
		renderer.GlobalRenderQueue.Shutdown()
	}
}

func TestGenerateShortCodeHandler(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		expectedStatus int
		expectedFields []string
		setupFunc      func()
	}{
		{
			name: "valid URL",
			requestBody: GenerateRequest{
				URL: "https://example.com/test",
			},
			expectedStatus: http.StatusCreated,
			expectedFields: []string{"short_code", "original_url"},
			setupFunc:      func() {},
		},
		{
			name: "existing URL",
			requestBody: GenerateRequest{
				URL: "https://existing.com",
			},
			expectedStatus: http.StatusOK,
			expectedFields: []string{"short_code", "original_url"},
			setupFunc: func() {
				// Pre-create a link
				link := &db.Link{
					ShortCode:    "EXIST1",
					OriginalURL:  "https://existing.com",
					RenderStatus: db.RenderStatusCompleted,
				}
				db.CreateLink(link)
			},
		},
		{
			name:           "invalid JSON",
			requestBody:    "invalid json",
			expectedStatus: http.StatusBadRequest,
			setupFunc:      func() {},
		},
		{
			name: "missing URL",
			requestBody: map[string]string{
				"noturl": "test",
			},
			expectedStatus: http.StatusBadRequest,
			setupFunc:      func() {},
		},
		{
			name: "invalid URL format",
			requestBody: GenerateRequest{
				URL: "not-a-valid-url",
			},
			expectedStatus: http.StatusBadRequest,
			setupFunc:      func() {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := setupTestAPI(t)
			defer teardownTestAPI(t)

			tt.setupFunc()

			// Prepare request
			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req, err := http.NewRequest("POST", "/generate", bytes.NewBuffer(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			// Perform request
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Check status
			assert.Equal(t, tt.expectedStatus, w.Code)

			// Check response body for successful requests
			if tt.expectedStatus == http.StatusCreated || tt.expectedStatus == http.StatusOK {
				var response GenerateResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)

				for _, field := range tt.expectedFields {
					switch field {
					case "short_code":
						assert.NotEmpty(t, response.ShortCode)
					case "original_url":
						assert.NotEmpty(t, response.OriginalURL)
					}
				}
			}
		})
	}
}

func TestGenerateShortCodeHandlerWithDomainRestriction(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		expectedStatus int
	}{
		{
			name:           "allowed domain",
			url:            "https://allowed.com/page",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "another allowed domain",
			url:            "https://example.org/test",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "forbidden domain",
			url:            "https://forbidden.com/page",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := setupTestAPI(t)
			defer teardownTestAPI(t)

			// Set allowed domains
			config.AppConfig.AllowedDomains = "allowed.com,example.org"

			requestBody := GenerateRequest{URL: tt.url}
			body, err := json.Marshal(requestBody)
			require.NoError(t, err)

			req, err := http.NewRequest("POST", "/generate", bytes.NewBuffer(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestRedirectHandler(t *testing.T) {
	tests := []struct {
		name           string
		shortCode      string
		userAgent      string
		setupFunc      func()
		expectedStatus int
		expectedHeader string
	}{
		{
			name:      "redirect user to original URL",
			shortCode: "USER123",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
			setupFunc: func() {
				link := &db.Link{
					ShortCode:    "USER123",
					OriginalURL:  "https://redirect-test.com",
					RenderStatus: db.RenderStatusCompleted,
				}
				db.CreateLink(link)
			},
			expectedStatus: http.StatusFound,
			expectedHeader: "https://redirect-test.com",
		},
		{
			name:      "serve HTML to bot",
			shortCode: "BOT123",
			userAgent: "Googlebot/2.1 (+http://www.google.com/bot.html)",
			setupFunc: func() {
				link := &db.Link{
					ShortCode:           "BOT123",
					OriginalURL:         "https://bot-test.com",
					RenderedHTMLContent: "<html><body>Rendered Content</body></html>",
					RenderStatus:        db.RenderStatusCompleted,
				}
				db.CreateLink(link)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "short code not found",
			shortCode:      "NOTFOUND",
			userAgent:      "Mozilla/5.0",
			setupFunc:      func() {},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:      "bot with failed rendering",
			shortCode: "FAILED123",
			userAgent: "Googlebot/2.1",
			setupFunc: func() {
				link := &db.Link{
					ShortCode:    "FAILED123",
					OriginalURL:  "https://failed-test.com",
					RenderStatus: db.RenderStatusFailed,
				}
				db.CreateLink(link)
			},
			expectedStatus: http.StatusFound,
			expectedHeader: "https://failed-test.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := setupTestAPI(t)
			defer teardownTestAPI(t)

			tt.setupFunc()

			req, err := http.NewRequest("GET", "/"+tt.shortCode, nil)
			require.NoError(t, err)
			req.Header.Set("User-Agent", tt.userAgent)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedHeader != "" {
				assert.Equal(t, tt.expectedHeader, w.Header().Get("Location"))
			}

			if tt.expectedStatus == http.StatusOK {
				// Check that HTML content is served
				assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
			}
		})
	}
}

func TestRedirectHandlerBotDetection(t *testing.T) {
	router := setupTestAPI(t)
	defer teardownTestAPI(t)

	// Setup a link with rendered content
	link := &db.Link{
		ShortCode:           "DETECT123",
		OriginalURL:         "https://detection-test.com",
		RenderedHTMLContent: "<html><body>Bot Content</body></html>",
		RenderStatus:        db.RenderStatusCompleted,
	}
	db.CreateLink(link)

	botUserAgents := []string{
		"Googlebot/2.1 (+http://www.google.com/bot.html)",
		"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
		"Slurp/3.0 (slurp@inktomi.com; http://www.inktomi.com/slurp.html)",
		"DuckDuckBot/1.1; (+http://duckduckgo.com/duckduckbot.html)",
		"BaiduSpider/2.0",
		"YandexBot/3.0",
		"facebookexternalhit/1.1",
		"Twitterbot/1.0",
		"LinkedInBot/1.0",
		"SomeCustomBot/1.0",
		"Web Crawler 1.0",
		"Search Spider",
	}

	for _, userAgent := range botUserAgents {
		t.Run("bot_detection_"+userAgent, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/DETECT123", nil)
			require.NoError(t, err)
			req.Header.Set("User-Agent", userAgent)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
			assert.Contains(t, w.Body.String(), "Bot Content")
		})
	}

	// Test regular user agents
	regularUserAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36",
	}

	for _, userAgent := range regularUserAgents {
		t.Run("user_detection_"+userAgent, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/DETECT123", nil)
			require.NoError(t, err)
			req.Header.Set("User-Agent", userAgent)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusFound, w.Code)
			assert.Equal(t, "https://detection-test.com", w.Header().Get("Location"))
		})
	}
}

func TestHealthCheckHandler(t *testing.T) {
	router := setupTestAPI(t)
	defer teardownTestAPI(t)

	req, err := http.NewRequest("GET", "/health", nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "UP", response["status"])
}

func TestStatusHandler(t *testing.T) {
	router := setupTestAPI(t)
	defer teardownTestAPI(t)

	req, err := http.NewRequest("GET", "/status", nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	assert.Equal(t, "UP", response["status"])
	assert.Contains(t, response, "render_queue")

	renderQueue, ok := response["render_queue"].(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, renderQueue, "worker_count")
	assert.Contains(t, renderQueue, "queue_length")
	assert.Contains(t, renderQueue, "in_progress_count")
}

func TestGenerateRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		request GenerateRequest
		isValid bool
	}{
		{
			name:    "valid HTTPS URL",
			request: GenerateRequest{URL: "https://example.com"},
			isValid: true,
		},
		{
			name:    "valid HTTP URL",
			request: GenerateRequest{URL: "http://example.com"},
			isValid: true,
		},
		{
			name:    "empty URL",
			request: GenerateRequest{URL: ""},
			isValid: false,
		},
		{
			name:    "invalid URL format",
			request: GenerateRequest{URL: "not-a-url"},
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := setupTestAPI(t)
			defer teardownTestAPI(t)

			body, err := json.Marshal(tt.request)
			require.NoError(t, err)

			req, err := http.NewRequest("POST", "/generate", bytes.NewBuffer(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if tt.isValid {
				assert.Equal(t, http.StatusCreated, w.Code)
			} else {
				assert.Equal(t, http.StatusBadRequest, w.Code)
			}
		})
	}
}

func TestGenerateResponseFormat(t *testing.T) {
	router := setupTestAPI(t)
	defer teardownTestAPI(t)

	requestBody := GenerateRequest{URL: "https://format-test.com"}
	body, err := json.Marshal(requestBody)
	require.NoError(t, err)

	req, err := http.NewRequest("POST", "/generate", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var response GenerateResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Validate response format
	assert.NotEmpty(t, response.ShortCode)
	assert.Equal(t, "https://format-test.com", response.OriginalURL)
	assert.Len(t, response.ShortCode, 6) // Default short code length
}

func BenchmarkGenerateShortCodeHandler(b *testing.B) {
	router := setupTestAPI(&testing.T{})
	defer teardownTestAPI(&testing.T{})

	requestBody := GenerateRequest{URL: "https://benchmark.com"}
	body, _ := json.Marshal(requestBody)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/generate", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkRedirectHandler(b *testing.B) {
	router := setupTestAPI(&testing.T{})
	defer teardownTestAPI(&testing.T{})

	// Setup test link
	link := &db.Link{
		ShortCode:    "BENCH123",
		OriginalURL:  "https://benchmark-redirect.com",
		RenderStatus: db.RenderStatusCompleted,
	}
	db.CreateLink(link)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("GET", "/BENCH123", nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (test)")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}
